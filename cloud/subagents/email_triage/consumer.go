package email_triage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// EmailClassifier interface for dependency injection
type EmailClassifierInterface interface {
	ClassifyEmail(ctx context.Context, email EmailContent) (*ClassificationResult, error)
}

// EmailConsumer consumes emails from the input stream and processes them
type EmailConsumer struct {
	redisClient    *redis.Client
	classifier     EmailClassifierInterface
	userIDs        []string
	consumerGroup  string
	consumerName   string
	stopChan       chan struct{}
	running        bool
}

// ProcessedEmail represents an email that has been classified and processed
type ProcessedEmail struct {
	MessageID         string               `json:"message_id"`
	ThreadID          string               `json:"thread_id"`
	UserID            string               `json:"user_id"`
	From              string               `json:"from"`
	To                string               `json:"to"`
	Subject           string               `json:"subject"`
	Snippet           string               `json:"snippet"`
	BodyPreview       string               `json:"body_preview"`
	Classification    *ClassificationResult `json:"classification"`
	ReceivedAt        time.Time            `json:"received_at"`
	ProcessedAt       time.Time            `json:"processed_at"`
	OriginalEmail     *EmailMessage        `json:"original_email,omitempty"`
}

const (
	defaultConsumerGroup = "email-triage"
	defaultConsumerName  = "email-classifier"
	streamReadCount      = 10
	streamBlockTimeout   = 5 * time.Second
	idleProcessingDelay  = 1 * time.Second
)

// NewEmailConsumer creates a new email consumer
func NewEmailConsumer(redisClient *redis.Client, classifier EmailClassifierInterface, userIDs []string) *EmailConsumer {
	return &EmailConsumer{
		redisClient:   redisClient,
		classifier:    classifier,
		userIDs:       userIDs,
		consumerGroup: defaultConsumerGroup,
		consumerName:  defaultConsumerName,
		stopChan:      make(chan struct{}),
		running:       false,
	}
}

// Start begins consuming emails from the input stream
func (c *EmailConsumer) Start(ctx context.Context) error {
	if c.running {
		return fmt.Errorf("email consumer is already running")
	}

	if err := c.ensureConsumerGroups(ctx); err != nil {
		return fmt.Errorf("failed to ensure consumer groups: %w", err)
	}

	c.running = true
	log.Printf("Starting email consumer for %d users, group: %s, name: %s", len(c.userIDs), c.consumerGroup, c.consumerName)

	// Start the consumption loop
	go c.consumeLoop(ctx)

	return nil
}

// Stop stops the email consumer
func (c *EmailConsumer) Stop() {
	if !c.running {
		return
	}

	log.Println("Stopping email consumer...")
	c.running = false
	close(c.stopChan)
}

// GetUserIDs returns the list of user IDs being consumed
func (c *EmailConsumer) GetUserIDs() []string {
	return c.userIDs
}

// consumeLoop runs the main consumption loop
func (c *EmailConsumer) consumeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Println("Email consumer stopped due to context cancellation")
			return
		case <-c.stopChan:
			log.Println("Email consumer stopped via stop signal")
			return
		default:
			idle := c.processAllUsers(ctx)
			if idle {
				// Sleep briefly if no messages were processed
				time.Sleep(idleProcessingDelay)
			}
		}
	}
}

// processAllUsers processes pending messages for all users
func (c *EmailConsumer) processAllUsers(ctx context.Context) bool {
	idle := true
	for _, userID := range c.userIDs {
		if processed := c.processUserMessages(ctx, userID); processed {
			idle = false
		}
	}
	return idle
}

// processUserMessages processes pending messages for a specific user
func (c *EmailConsumer) processUserMessages(ctx context.Context, userID string) bool {
	streamKey := fmt.Sprintf("user:%s:in:email", userID)

	// Read messages from the stream
	messages, err := c.redisClient.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.consumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{streamKey, ">"},
		Count:    streamReadCount,
		Block:    streamBlockTimeout,
	}).Result()

	if err != nil {
		if err != redis.Nil {
			log.Printf("Error reading from stream %s: %v", streamKey, err)
		}
		return false
	}

	if len(messages) == 0 || len(messages[0].Messages) == 0 {
		return false
	}

	processedCount := 0
	for _, streamMsg := range messages[0].Messages {
		if err := c.processMessage(ctx, userID, streamMsg); err != nil {
			log.Printf("Error processing message %s for user %s: %v", streamMsg.ID, userID, err)
			// Continue processing other messages
		} else {
			processedCount++
		}
	}

	if processedCount > 0 {
		log.Printf("Processed %d messages for user %s", processedCount, userID)
	}

	return processedCount > 0
}

// processMessage processes a single message from the stream
func (c *EmailConsumer) processMessage(ctx context.Context, userID string, streamMsg redis.XMessage) error {
	// Parse the message from the stream
	emailMsg, err := c.parseStreamMessage(streamMsg)
	if err != nil {
		log.Printf("Failed to parse stream message %s: %v", streamMsg.ID, err)
		// Acknowledge the malformed message to avoid reprocessing
		return c.acknowledgeMessage(ctx, userID, streamMsg.ID)
	}

	if emailMsg == nil {
		return c.acknowledgeMessage(ctx, userID, streamMsg.ID)
	}

	// Only process emails that clearly need responses
	if !c.shouldProcessEmail(emailMsg) {
		log.Printf("Skipping email %s - does not clearly need response", emailMsg.ID)
		return c.acknowledgeMessage(ctx, userID, streamMsg.ID)
	}

	// Classify the email
	classification, err := c.classifier.ClassifyEmail(ctx, EmailContent{
		Subject: emailMsg.Subject,
		From:    emailMsg.From,
		Body:    emailMsg.BodyText,
		Snippet: emailMsg.Snippet,
	})
	if err != nil {
		return fmt.Errorf("failed to classify email %s: %w", emailMsg.ID, err)
	}

	// Create processed email
	processedEmail := &ProcessedEmail{
		MessageID:   emailMsg.ID,
		ThreadID:    emailMsg.ThreadID,
		UserID:      userID,
		From:        emailMsg.From,
		To:          fmt.Sprintf("%v", emailMsg.To), // Convert []string to string for display
		Subject:     emailMsg.Subject,
		Snippet:     emailMsg.Snippet,
		Classification: classification,
		ReceivedAt:  emailMsg.Date,
		ProcessedAt: time.Now().UTC(),
		OriginalEmail: emailMsg,
	}

	if emailMsg.BodyText != "" {
		processedEmail.BodyPreview = truncateString(emailMsg.BodyText, 512)
	}

	// Emit the processed email to internal stream (Phase D - not whiteboard yet)
	if err := c.emitProcessedEmail(ctx, userID, processedEmail); err != nil {
		return fmt.Errorf("failed to emit processed email %s: %w", emailMsg.ID, err)
	}

	// Acknowledge the message
	return c.acknowledgeMessage(ctx, userID, streamMsg.ID)
}

// parseStreamMessage parses a message from the Redis stream
func (c *EmailConsumer) parseStreamMessage(streamMsg redis.XMessage) (*EmailMessage, error) {
	rawJSON, ok := streamMsg.Values["raw_json"]
	if !ok {
		return nil, fmt.Errorf("missing raw_json field in stream message")
	}

	jsonStr, ok := rawJSON.(string)
	if !ok {
		return nil, fmt.Errorf("raw_json field is not a string")
	}

	var emailMsg EmailMessage
	if err := json.Unmarshal([]byte(jsonStr), &emailMsg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal email message: %w", err)
	}

	return &emailMsg, nil
}

// shouldProcessEmail determines if an email needs processing (BE GENEROUS)
func (c *EmailConsumer) shouldProcessEmail(emailMsg *EmailMessage) bool {
	if emailMsg == nil {
		return false
	}

	// BE GENEROUS: Process most emails, let GPT-5 Nano make the final decision
	subjectLower := strings.ToLower(emailMsg.Subject)
	snippetLower := strings.ToLower(emailMsg.Snippet)
	bodyLower := strings.ToLower(emailMsg.BodyText)

	// Only skip clearly automated bulk messages that would never need responses
	bulkAutomatedKeywords := []string{
		"unsubscribe", "receipt", "invoice", "order confirmation",
		"shipping", "newsletter", "weekly digest", "noreply", "no-reply", "donotreply",
	}

	for _, keyword := range bulkAutomatedKeywords {
		if strings.Contains(subjectLower, keyword) ||
		   strings.Contains(snippetLower, keyword) ||
		   strings.Contains(fromDomain(emailMsg.From), keyword) {
			return false
		}
	}

	// Look for indicators that a response might be needed (BE GENEROUS)
	responseKeywords := []string{
		"?", "question", "confirm", "can you", "could you", "would you",
		"please", "help", "advice", "opinion", "feedback", "thoughts",
		"meeting", "call", "discuss", "review", "approval", "input",
		"decision", "suggestion", "when", "what", "why", "how", "where",
		"urgent", "deadline", "asap", "today", "tomorrow", "this week",
		"notification", "reminder", "update", "alert", "important",
		"thanks", "thank you", "appreciate", "looking forward",
	}

	for _, keyword := range responseKeywords {
		if strings.Contains(subjectLower, keyword) ||
		   strings.Contains(snippetLower, keyword) ||
		   strings.Contains(bodyLower, keyword) {
			return true
		}
	}

	// If from a person (not noreply) and has any meaningful content, process it
	fromLower := strings.ToLower(emailMsg.From)
	if !strings.Contains(fromLower, "noreply") &&
	   !strings.Contains(fromLower, "no-reply") &&
	   !strings.Contains(fromLower, "donotreply") &&
	   (len(emailMsg.Subject) > 5 || len(emailMsg.BodyText) > 20) {
		return true
	}

	// Default to processing when in doubt (BE GENEROUS)
	return len(emailMsg.Subject) > 10 || len(emailMsg.BodyText) > 50
}

// fromDomain extracts the domain from an email address
func fromDomain(from string) string {
	parts := strings.Split(from, "@")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// emitProcessedEmail emits a processed email to the internal stream
func (c *EmailConsumer) emitProcessedEmail(ctx context.Context, userID string, processedEmail *ProcessedEmail) error {
	outputStreamKey := fmt.Sprintf("user:%s:processed:email", userID)

	values := map[string]interface{}{
		"type":                "email_classified",
		"user_id":             userID,
		"message_id":          processedEmail.MessageID,
		"thread_id":           processedEmail.ThreadID,
		"from":                processedEmail.From,
		"to":                  processedEmail.To,
		"subject":             processedEmail.Subject,
		"snippet":             processedEmail.Snippet,
		"body_preview":        processedEmail.BodyPreview,
		"classification":      processedEmail.Classification.Classification,
		"requires_response":   func() string { if processedEmail.Classification.RequiresResponse { return "true" } else { return "false" } }(),
		"summary":             processedEmail.Classification.Summary,
		"draft_reply":         processedEmail.Classification.DraftReply,
		"priority":            processedEmail.Classification.Priority,
		"confidence":          processedEmail.Classification.Confidence,
		"reasoning":           processedEmail.Classification.Reasoning,
		"received_at":         processedEmail.ReceivedAt.UTC().Format(time.RFC3339Nano),
		"processed_at":        processedEmail.ProcessedAt.UTC().Format(time.RFC3339Nano),
	}

	// Add classification as JSON
	if classificationJSON, err := json.Marshal(processedEmail.Classification); err == nil {
		values["classification_json"] = string(classificationJSON)
	}

	// Add to Redis stream
	if err := c.redisClient.XAdd(ctx, &redis.XAddArgs{
		Stream: outputStreamKey,
		Values: values,
	}).Err(); err != nil {
		return fmt.Errorf("failed to append processed email to stream: %w", err)
	}

	log.Printf("Emitted processed email %s to stream %s", processedEmail.MessageID, outputStreamKey)
	return nil
}

// acknowledgeMessage acknowledges a message in the consumer group
func (c *EmailConsumer) acknowledgeMessage(ctx context.Context, userID, messageID string) error {
	streamKey := fmt.Sprintf("user:%s:in:email", userID)

	if err := c.redisClient.XAck(ctx, streamKey, c.consumerGroup, messageID).Err(); err != nil {
		return fmt.Errorf("failed to acknowledge message %s: %w", messageID, err)
	}

	return nil
}

// ensureConsumerGroups ensures consumer groups exist for all user streams
func (c *EmailConsumer) ensureConsumerGroups(ctx context.Context) error {
	for _, userID := range c.userIDs {
		streamKey := fmt.Sprintf("user:%s:in:email", userID)

		// Create the consumer group with MKSTREAM option to create stream if it doesn't exist
		err := c.redisClient.XGroupCreateMkStream(ctx, streamKey, c.consumerGroup, "0").Err()
		if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
			log.Printf("Warning: Failed to create consumer group for %s: %v", streamKey, err)
		}
	}
	return nil
}

// truncateString is defined in poller_30s.go