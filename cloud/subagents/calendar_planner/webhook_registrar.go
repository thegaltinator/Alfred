package calendar_planner

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"google.golang.org/api/calendar/v3"
)

// WebhookRegistrar manages Google Calendar push notification webhooks
type WebhookRegistrar struct {
	redisClient    *redis.Client
	calendarService *calendar.Service
}

// NewWebhookRegistrar creates a new webhook registrar
func NewWebhookRegistrar(redisClient *redis.Client, calendarService *calendar.Service) *WebhookRegistrar {
	return &WebhookRegistrar{
		redisClient:    redisClient,
		calendarService: calendarService,
	}
}

// RegisterWebhook registers a new webhook for calendar notifications
func (wr *WebhookRegistrar) RegisterWebhook(ctx context.Context, userID, calendarID, webhookURL string) (*calendar.Channel, error) {
	// Generate a unique channel ID
	channelID := uuid.New().String()

	// Create the channel for push notifications
	channel := &calendar.Channel{
		Id:         channelID,
		Type:       "web_hook",
		Address:    webhookURL,
		Expiration: time.Now().Add(24 * time.Hour).UnixNano() / 1000000, // Google expects milliseconds
	}

	// Register the webhook with Google Calendar API
	response, err := wr.calendarService.Events.Watch(calendarID, channel).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to register webhook with Google Calendar API: %w", err)
	}

	log.Printf("Successfully registered webhook for user %s, calendar %s: ChannelID=%s, ResourceID=%s",
		userID, calendarID, response.Id, response.ResourceId)

	// Store webhook registration metadata in Redis for tracking
	webhookKey := fmt.Sprintf("webhook_meta:%s:%s", userID, response.Id)
	metadata := map[string]interface{}{
		"user_id":      userID,
		"calendar_id":  calendarID,
		"channel_id":   response.Id,
		"resource_id":  response.ResourceId,
		"webhook_url":  webhookURL,
		"resource_uri": response.ResourceUri,
		"expiration":   time.Unix(response.Expiration/1000, 0), // Convert back from milliseconds
		"created_at":   time.Now(),
	}

	if err := wr.redisClient.HMSet(ctx, webhookKey, metadata).Err(); err != nil {
		log.Printf("Warning: Failed to store webhook metadata in Redis: %v", err)
		// Don't fail the operation if Redis storage fails
	}

	// Set expiration on the metadata key to match the channel expiration
	expirationDuration := time.Until(time.Unix(response.Expiration/1000, 0))
	if err := wr.redisClient.Expire(ctx, webhookKey, expirationDuration).Err(); err != nil {
		log.Printf("Warning: Failed to set expiration on webhook metadata: %v", err)
	}

	// Store a reverse lookup to find users by channel ID
	reverseKey := fmt.Sprintf("webhook_reverse:%s", response.Id)
	if err := wr.redisClient.Set(ctx, reverseKey, userID, expirationDuration).Err(); err != nil {
		log.Printf("Warning: Failed to store reverse lookup for webhook: %v", err)
	}

	return response, nil
}

// UnregisterWebhook removes a webhook registration
func (wr *WebhookRegistrar) UnregisterWebhook(ctx context.Context, channelID, resourceID string) error {
	// Stop the channel with Google Calendar API
	// Create a channel object with the required ID and resource ID
	channel := &calendar.Channel{
		Id:         channelID,
		ResourceId: resourceID,
	}

	err := wr.calendarService.Channels.Stop(channel).Context(ctx).Do()
	if err != nil {
		// Don't fail if webhook is already stopped or doesn't exist
		log.Printf("Warning: Failed to stop webhook channel %s: %v", channelID, err)
	}

	// Clean up Redis entries
	webhookKey := fmt.Sprintf("webhook_meta:*:%s", channelID)
	keys, err := wr.redisClient.Keys(ctx, webhookKey).Result()
	if err != nil {
		log.Printf("Warning: Failed to find webhook metadata keys: %v", err)
	}

	for _, key := range keys {
		if err := wr.redisClient.Del(ctx, key).Err(); err != nil {
			log.Printf("Warning: Failed to delete webhook metadata %s: %v", key, err)
		}
	}

	// Remove reverse lookup
	reverseKey := fmt.Sprintf("webhook_reverse:%s", channelID)
	if err := wr.redisClient.Del(ctx, reverseKey).Err(); err != nil {
		log.Printf("Warning: Failed to delete reverse lookup for webhook: %v", err)
	}

	log.Printf("Successfully unregistered webhook: ChannelID=%s, ResourceID=%s", channelID, resourceID)
	return nil
}

// RenewWebhook extends the expiration of an existing webhook
func (wr *WebhookRegistrar) RenewWebhook(ctx context.Context, userID, channelID string) (*calendar.Channel, error) {
	// Find the webhook metadata for this user and channel
	webhookKey := fmt.Sprintf("webhook_meta:%s:%s", userID, channelID)
	metadata, err := wr.redisClient.HGetAll(ctx, webhookKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to find webhook metadata: %w", err)
	}

	if len(metadata) == 0 {
		return nil, fmt.Errorf("webhook not found for user %s, channel %s", userID, channelID)
	}

	calendarID := metadata["calendar_id"]
	resourceID := metadata["resource_id"]
	webhookURL := metadata["webhook_url"]

	// First, unregister the old webhook
	if err := wr.UnregisterWebhook(ctx, channelID, resourceID); err != nil {
		log.Printf("Warning: Failed to unregister old webhook during renewal: %v", err)
	}

	// Register a new webhook with extended expiration
	return wr.RegisterWebhook(ctx, userID, calendarID, webhookURL)
}

// GetWebhookInfo returns information about a registered webhook
func (wr *WebhookRegistrar) GetWebhookInfo(ctx context.Context, userID, channelID string) (map[string]interface{}, error) {
	webhookKey := fmt.Sprintf("webhook_meta:%s:%s", userID, channelID)
	metadata, err := wr.redisClient.HGetAll(ctx, webhookKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get webhook metadata: %w", err)
	}

	if len(metadata) == 0 {
		return nil, fmt.Errorf("webhook not found for user %s, channel %s", userID, channelID)
	}

	// Convert string map to interface map
	result := make(map[string]interface{})
	for key, value := range metadata {
		result[key] = value
	}

	return result, nil
}

// ListUserWebhooks returns all webhooks registered for a user
func (wr *WebhookRegistrar) ListUserWebhooks(ctx context.Context, userID string) ([]map[string]interface{}, error) {
	pattern := fmt.Sprintf("webhook_meta:%s:*", userID)
	keys, err := wr.redisClient.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list webhook keys: %w", err)
	}

	webhooks := make([]map[string]interface{}, 0, len(keys))
	for _, key := range keys {
		metadata, err := wr.redisClient.HGetAll(ctx, key).Result()
		if err != nil {
			log.Printf("Warning: Failed to get metadata for key %s: %v", key, err)
			continue
		}

		if len(metadata) > 0 {
			// Convert string map to interface map
			result := make(map[string]interface{})
			for k, v := range metadata {
				result[k] = v
			}
			webhooks = append(webhooks, result)
		}
	}

	return webhooks, nil
}

// CleanupExpiredWebhooks removes webhook metadata for expired channels
func (wr *WebhookRegistrar) CleanupExpiredWebhooks(ctx context.Context) error {
	pattern := "webhook_meta:*"
	keys, err := wr.redisClient.Keys(ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("failed to list webhook keys for cleanup: %w", err)
	}

	now := time.Now()
	cleaned := 0

	for _, key := range keys {
		// Check the expiration time
		expirationStr, err := wr.redisClient.HGet(ctx, key, "expiration").Result()
		if err != nil {
			// If we can't get expiration, check TTL
			ttl := wr.redisClient.TTL(ctx, key).Val()
			if ttl < 0 { // No expiration or key doesn't exist
				continue
			}
		}

		if expirationStr != "" {
			expiration, err := time.Parse(time.RFC3339, expirationStr)
			if err == nil && expiration.Before(now) {
				// Webhook has expired, remove it
				if err := wr.redisClient.Del(ctx, key).Err(); err == nil {
					cleaned++
				}

				// Also remove reverse lookup if channel_id exists
				channelID, _ := wr.redisClient.HGet(ctx, key, "channel_id").Result()
				if channelID != "" {
					reverseKey := fmt.Sprintf("webhook_reverse:%s", channelID)
					wr.redisClient.Del(ctx, reverseKey)
				}
			}
		}
	}

	if cleaned > 0 {
		log.Printf("Cleaned up %d expired webhook entries", cleaned)
	}

	return nil
}

// FindUserByChannel finds the user associated with a given channel ID
func (wr *WebhookRegistrar) FindUserByChannel(ctx context.Context, channelID string) (string, error) {
	// First try the reverse lookup
	reverseKey := fmt.Sprintf("webhook_reverse:%s", channelID)
	userID, err := wr.redisClient.Get(ctx, reverseKey).Result()
	if err == nil && userID != "" {
		return userID, nil
	}

	// If reverse lookup fails, search through all webhook metadata
	pattern := "webhook_meta:*"
	keys, err := wr.redisClient.Keys(ctx, pattern).Result()
	if err != nil {
		return "", fmt.Errorf("failed to search webhook keys: %w", err)
	}

	for _, key := range keys {
		storedChannelID, err := wr.redisClient.HGet(ctx, key, "channel_id").Result()
		if err == nil && storedChannelID == channelID {
			userID, _ := wr.redisClient.HGet(ctx, key, "user_id").Result()
			if userID != "" {
				// Update the reverse lookup for future requests
				wr.redisClient.Set(ctx, reverseKey, userID, 24*time.Hour)
				return userID, nil
			}
		}
	}

	return "", fmt.Errorf("no user found for channel %s", channelID)
}