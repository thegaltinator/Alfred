package email_triage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"alfred-cloud/security"

	"github.com/redis/go-redis/v9"
	"google.golang.org/api/gmail/v1"
)

const (
	gmailListPageSize int64 = 50
)

// EmailMessage represents a raw email message from Gmail (pre-classification)
type EmailMessage struct {
	ID        string    `json:"id"`
	ThreadID  string    `json:"thread_id"`
	Subject   string    `json:"subject"`
	From      string    `json:"from"`
	To        []string  `json:"to"`
	Date      time.Time `json:"date"`
	Snippet   string    `json:"snippet"`
	BodyText  string    `json:"body_text,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	UserID    string    `json:"user_id"`
}

// EmailPoller polls Gmail for new messages every 30 seconds
type EmailPoller struct {
	googleClient   *security.GoogleServiceClient
	redisClient    *redis.Client
	userIDs        []string
	pollInterval   time.Duration
	lastMessageIDs map[string]string // userID -> last message ID
	startupTime    time.Time         // NEW: Track when poller started
	stopChan       chan struct{}
	running        bool
}

// NewEmailPoller creates a new email poller
func NewEmailPoller(googleClient *security.GoogleServiceClient, redisClient *redis.Client, userIDs []string) *EmailPoller {
	return &EmailPoller{
		googleClient:   googleClient,
		redisClient:    redisClient,
		userIDs:        userIDs,
		pollInterval:   30 * time.Second,
		lastMessageIDs: make(map[string]string),
		startupTime:    time.Now(), // NEW: Record when poller started
		stopChan:       make(chan struct{}),
		running:        false,
	}
}

// Start begins the email polling process
func (p *EmailPoller) Start(ctx context.Context) error {
	if p.running {
		return fmt.Errorf("email poller is already running")
	}

	p.running = true
	log.Printf("Starting email poller for %d users, checking every %v", len(p.userIDs), p.pollInterval)

	// Initialize last message IDs for each user
	for _, userID := range p.userIDs {
		if id, err := p.loadLastMessageID(ctx, userID); err != nil {
			log.Printf("Warning: Failed to load last message ID for user %s: %v", userID, err)
		} else if id != "" {
			continue
		}

		if err := p.initializeLastMessageID(ctx, userID); err != nil {
			log.Printf("Warning: Failed to initialize last message ID for user %s: %v", userID, err)
			continue
		}

		if id := p.lastMessageIDs[userID]; id != "" {
			p.persistLastMessageID(ctx, userID, id)
		}
	}

	// Start the polling loop
	go p.pollLoop(ctx)

	return nil
}

// Stop stops the email polling process
func (p *EmailPoller) Stop() {
	if !p.running {
		return
	}

	log.Println("Stopping email poller...")
	p.running = false
	close(p.stopChan)
}

// GetUserIDs returns the list of user IDs being polled
func (p *EmailPoller) GetUserIDs() []string {
	return p.userIDs
}

// pollLoop runs the main polling loop
func (p *EmailPoller) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("Email poller stopped due to context cancellation")
			return
		case <-p.stopChan:
			log.Println("Email poller stopped via stop signal")
			return
		case <-ticker.C:
			p.pollAllUsers(ctx)
		}
	}
}

// pollAllUsers checks for new emails for all users
func (p *EmailPoller) pollAllUsers(ctx context.Context) {
	for _, userID := range p.userIDs {
		if err := p.pollUser(ctx, userID); err != nil {
			log.Printf("Error polling user %s: %v", userID, err)
		}
	}
}

// pollUser checks for new emails for a specific user
func (p *EmailPoller) pollUser(ctx context.Context, userID string) error {
	log.Printf("Polling user %s for new emails...", userID)

	// Get Gmail service
	service, err := p.googleClient.GetGmailService(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get Gmail service for user %s: %w", userID, err)
	}

	// Get new messages since last check
	lastMessageID := p.lastMessageIDs[userID]
	messages, err := p.getNewMessages(ctx, service, userID, lastMessageID)
	if err != nil {
		return fmt.Errorf("failed to get new messages for user %s: %w", userID, err)
	}

	if len(messages) == 0 {
		log.Printf("No new messages found for user %s", userID)
		return nil // No new messages
	}

	log.Printf("Found %d new messages for user %s", len(messages), userID)

	// Process each new message
	for _, message := range messages {
		processedMessage, err := p.processMessage(ctx, service, userID, message)
		if err != nil {
			log.Printf("Error processing message %s for user %s: %v", message.Id, userID, err)
			continue
		}

		// Emit to input stream
		if err := p.emitToInputStream(ctx, userID, processedMessage); err != nil {
			log.Printf("Error emitting message %s to stream: %v", message.Id, err)
		}

		// Update last message ID
		p.lastMessageIDs[userID] = message.Id
		p.persistLastMessageID(ctx, userID, message.Id)
	}

	return nil
}

// initializeLastMessageID initializes the last message ID for a user using timestamp filtering
func (p *EmailPoller) initializeLastMessageID(ctx context.Context, userID string) error {
	service, err := p.googleClient.GetGmailService(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get Gmail service: %w", err)
	}

	// Only look for emails received after poller startup (with 5-minute buffer for clock sync)
	sinceTime := p.startupTime.Add(-5 * time.Minute)
	query := fmt.Sprintf("is:unread category:primary after:%d", sinceTime.Unix())

	// Get the most recent message received after startup time
	response, err := service.Users.Messages.List("me").
		MaxResults(1).
		Q(query).
		Do()
	if err != nil {
		return fmt.Errorf("failed to list messages: %w", err)
	}

	if len(response.Messages) > 0 {
		p.lastMessageIDs[userID] = response.Messages[0].Id
		log.Printf("Initialized last message ID for user %s: %s (emails since %s)", userID, p.lastMessageIDs[userID], sinceTime.Format("2006-01-02 15:04:05"))
		p.persistLastMessageID(ctx, userID, p.lastMessageIDs[userID])
	} else {
		log.Printf("No unread messages found since startup time %s for user %s", sinceTime.Format("2006-01-02 15:04:05"), userID)
	}

	return nil
}

// getNewMessages retrieves new messages since the last check
func (p *EmailPoller) getNewMessages(ctx context.Context, service *gmail.Service, userID, lastMessageID string) ([]*gmail.Message, error) {
	pageToken := ""
	lastFound := lastMessageID == ""
	var pendingIDs []string

	for {
		// Add timestamp filtering for extra safety - only look for emails since startup
		query := fmt.Sprintf("is:unread category:primary after:%d", p.startupTime.Add(-5*time.Minute).Unix())
		listCall := service.Users.Messages.List("me").
			Q(query).
			MaxResults(gmailListPageSize)

		if pageToken != "" {
			listCall = listCall.PageToken(pageToken)
		}

		response, err := listCall.Do()
		if err != nil {
			return nil, fmt.Errorf("failed to list messages: %w", err)
		}

		batchIDs, hitLast := collectNewMessageIDs(response.Messages, lastMessageID)
		pendingIDs = append(pendingIDs, batchIDs...)
		if hitLast {
			lastFound = true
		}

		if hitLast || response.NextPageToken == "" || len(response.Messages) == 0 {
			break
		}

		pageToken = response.NextPageToken
	}

	if len(pendingIDs) == 0 {
		return nil, nil
	}

	if lastMessageID != "" && !lastFound {
		log.Printf("Last message ID %s not found; processing %d latest unread messages", lastMessageID, len(pendingIDs))
	}

	var messages []*gmail.Message
	for i := len(pendingIDs) - 1; i >= 0; i-- {
		message, err := service.Users.Messages.Get("me", pendingIDs[i]).
			Format("full").
			Do()
		if err != nil {
			log.Printf("Warning: Failed to get message details for %s: %v", pendingIDs[i], err)
			continue
		}
		messages = append(messages, message)
	}

	return messages, nil
}

// processMessage processes a single Gmail message
func (p *EmailPoller) processMessage(ctx context.Context, service *gmail.Service, userID string, message *gmail.Message) (*EmailMessage, error) {
	// Extract headers
	var subject, from string
	var to []string

	for _, header := range message.Payload.Headers {
		switch header.Name {
		case "Subject":
			subject = header.Value
		case "From":
			from = header.Value
		case "To":
			to = append(to, header.Value)
		}
	}

	// Get message body
	bodyText := p.extractPlainText(message)

	// Parse date
	var messageDate time.Time
	if message.InternalDate != 0 {
		messageDate = time.Unix(message.InternalDate/1000, 0)
	} else {
		messageDate = time.Now()
	}

	processedMessage := &EmailMessage{
		ID:        message.Id,
		ThreadID:  message.ThreadId,
		Subject:   subject,
		From:      from,
		To:        to,
		Date:      messageDate,
		Snippet:   message.Snippet,
		BodyText:  bodyText,
		Timestamp: time.Now(),
		UserID:    userID,
	}

	return processedMessage, nil
}

// extractPlainText extracts plain text from a Gmail message
func (p *EmailPoller) extractPlainText(message *gmail.Message) string {
	if message.Payload == nil {
		return message.Snippet
	}

	return p.extractFromPart(message.Payload)
}

// extractFromPart recursively extracts text from message parts
func (p *EmailPoller) extractFromPart(part *gmail.MessagePart) string {
	// If this part has text content, return it
	if part.MimeType == "text/plain" && len(part.Body.Data) > 0 {
		// Decode base64 URL encoded content
		data, err := base64URLDecode(part.Body.Data)
		if err != nil {
			log.Printf("Warning: Failed to decode message body: %v", err)
			return ""
		}
		return string(data)
	}

	// Recursively check nested parts
	for _, childPart := range part.Parts {
		text := p.extractFromPart(childPart)
		if text != "" {
			return text
		}
	}

	return ""
}

// base64URLDecode decodes base64 URL encoded strings
func base64URLDecode(data string) ([]byte, error) {
	// Add padding if necessary
	for len(data)%4 != 0 {
		data += "="
	}
	return base64.URLEncoding.DecodeString(data)
}

// collectNewMessageIDs extracts unread message IDs newer than the last processed ID.
func collectNewMessageIDs(messageRefs []*gmail.Message, lastMessageID string) ([]string, bool) {
	newIDs := make([]string, 0, len(messageRefs))

	if lastMessageID == "" {
		for _, ref := range messageRefs {
			if ref == nil || ref.Id == "" {
				continue
			}
			newIDs = append(newIDs, ref.Id)
		}
		return newIDs, true
	}

	for _, ref := range messageRefs {
		if ref == nil || ref.Id == "" {
			continue
		}
		if ref.Id == lastMessageID {
			return newIDs, true
		}
		newIDs = append(newIDs, ref.Id)
	}

	return newIDs, false
}

// Note: Classification logic has been moved to the EmailClassifier in classifier.go
// This poller now only fetches raw emails from Primary category for processing

// emitToInputStream emits a raw message to the email input stream for processing by the classifier
func (p *EmailPoller) emitToInputStream(ctx context.Context, userID string, message *EmailMessage) error {
	streamKey := fmt.Sprintf("user:%s:in:email", userID)

	toJSON, _ := json.Marshal(message.To)
	rawJSON, _ := json.Marshal(message)

	values := map[string]interface{}{
		"type":         "email_delta",
		"user_id":      userID,
		"message_id":   message.ID,
		"thread_id":    message.ThreadID,
		"subject":      message.Subject,
		"from":         message.From,
		"to":           strings.Join(message.To, ", "),
		"to_json":      string(toJSON),
		"snippet":      message.Snippet,
		"timestamp":    message.Timestamp.UTC().Format(time.RFC3339Nano),
		"received_at":  message.Date.UTC().Format(time.RFC3339Nano),
		"raw_json":     string(rawJSON),
	}

	if message.BodyText != "" {
		values["body_preview"] = truncateString(message.BodyText, 512)
	}

	// Add to Redis stream for classification by email triage subagent
	if err := p.redisClient.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: values,
	}).Err(); err != nil {
		return fmt.Errorf("failed to append message to stream: %w", err)
	}

	log.Printf("Emitted raw email message %s to stream %s for classification", message.ID, streamKey)
	return nil
}

func (p *EmailPoller) lastMessageRedisKey(userID string) string {
	return fmt.Sprintf("email_poller:last_message:%s", userID)
}

func (p *EmailPoller) loadLastMessageID(ctx context.Context, userID string) (string, error) {
	if p.redisClient == nil {
		return "", nil
	}

	id, err := p.redisClient.Get(ctx, p.lastMessageRedisKey(userID)).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	if id != "" {
		p.lastMessageIDs[userID] = id
	}

	return id, nil
}

func (p *EmailPoller) persistLastMessageID(ctx context.Context, userID, messageID string) {
	if p.redisClient == nil || messageID == "" {
		return
	}

	if err := p.redisClient.Set(ctx, p.lastMessageRedisKey(userID), messageID, 0).Err(); err != nil {
		log.Printf("Warning: Failed to persist last message ID for user %s: %v", userID, err)
	}
}

func truncateString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
