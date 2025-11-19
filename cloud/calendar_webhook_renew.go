package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"alfred-cloud/security"
	"alfred-cloud/subagents/calendar_planner"
	"github.com/redis/go-redis/v9"
	calendar "google.golang.org/api/calendar/v3"
)

type WebhookRenewer struct {
	redisClient *redis.Client
	tokenStore  *security.TokenStore
	interval    time.Duration
	threshold   time.Duration
	enabled     bool
}

func NewWebhookRenewer(redisClient *redis.Client, tokenStore *security.TokenStore, interval, threshold time.Duration, enabled bool) *WebhookRenewer {
	return &WebhookRenewer{
		redisClient: redisClient,
		tokenStore:  tokenStore,
		interval:    interval,
		threshold:   threshold,
		enabled:     enabled,
	}
}

func (r *WebhookRenewer) Start(ctx context.Context) {
	if !r.enabled {
		log.Println("Calendar webhook renewal disabled")
		return
	}
	if r.redisClient == nil || r.tokenStore == nil {
		log.Println("Calendar webhook renewal disabled: missing redis or token store")
		return
	}
	if r.interval <= 0 {
		r.interval = time.Hour
	}
	if r.threshold <= 0 {
		r.threshold = 12 * time.Hour
	}
	go r.loop(ctx)
}

func (r *WebhookRenewer) loop(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		if err := r.scanAndRenew(ctx); err != nil {
			log.Printf("Calendar webhook renewal scan error: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *WebhookRenewer) scanAndRenew(ctx context.Context) error {
	iter := r.redisClient.Scan(ctx, 0, "calendar_webhook:*", 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		data, err := r.redisClient.HGetAll(ctx, key).Result()
		if err != nil {
			log.Printf("Renewal: failed to read %s: %v", key, err)
			continue
		}
		expStr := data["expiration"]
		webhookURL := data["webhook_url"]
		userID := data["user_id"]
		calendarID := data["calendar_id"]
		channelID := data["channel_id"]
		resourceID := data["resource_id"]

		expMs, err := strconv.ParseInt(strings.TrimSpace(expStr), 10, 64)
		if err != nil || expMs == 0 {
			log.Printf("Renewal: invalid expiration for %s: %q", key, expStr)
			continue
		}
		expTime := time.UnixMilli(expMs)
		if time.Until(expTime) > r.threshold {
			continue
		}

		log.Printf("Renewal: renewing calendar webhook user=%s calendar=%s channel=%s resource=%s expiring=%s", userID, calendarID, channelID, resourceID, expTime.Format(time.RFC3339))

		googleClient := security.NewGoogleServiceClient(r.tokenStore)
		calendarService, err := googleClient.GetCalendarService(ctx, userID)
		if err != nil {
			log.Printf("Renewal: failed to get calendar service for user %s: %v", userID, err)
			continue
		}

		registrar := calendar_planner.NewWebhookRegistrar(r.redisClient, calendarService)
		channel, err := registrar.RegisterWebhook(ctx, userID, calendarID, webhookURL)
		if err != nil {
			log.Printf("Renewal: register failed for user %s: %v", userID, err)
			continue
		}

		if err := r.persistNewChannel(ctx, userID, calendarID, webhookURL, channel); err != nil {
			log.Printf("Renewal: persist new channel failed: %v", err)
			continue
		}

		// Cleanup old key
		if err := r.redisClient.Del(ctx, key).Err(); err != nil {
			log.Printf("Renewal: failed to delete old webhook key %s: %v", key, err)
		}
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("renewal scan iterator: %w", err)
	}
	return nil
}

func (r *WebhookRenewer) persistNewChannel(ctx context.Context, userID, calendarID, webhookURL string, channel *calendar.Channel) error {
	if channel == nil {
		return fmt.Errorf("nil channel")
	}
	registrationKey := fmt.Sprintf("calendar_webhook:%s:%s", userID, channel.Id)
	registrationData := map[string]interface{}{
		"channel_id":  channel.Id,
		"resource_id": channel.ResourceId,
		"calendar_id": calendarID,
		"webhook_url": webhookURL,
		"expiration":  channel.Expiration,
		"created_at":  time.Now(),
		"user_id":     userID,
	}

	if err := r.redisClient.HMSet(ctx, registrationKey, registrationData).Err(); err != nil {
		return fmt.Errorf("store renewed webhook: %w", err)
	}

	expirationTime := time.Unix(channel.Expiration/1000, 0)
	if err := r.redisClient.Expire(ctx, registrationKey, time.Until(expirationTime)).Err(); err != nil {
		log.Printf("Renewal: failed to set expiration on %s: %v", registrationKey, err)
	}
	return nil
}
