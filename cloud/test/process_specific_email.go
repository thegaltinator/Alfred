package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"alfred-cloud/subagents/email_triage"
	"github.com/redis/go-redis/v9"
)

func main() {
	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	ctx := context.Background()

	// Test Redis connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis")

	// Create GPT-5 Nano classifier
	classifier, err := email_triage.NewEmailClassifier()
	if err != nil {
		log.Fatalf("Failed to create classifier: %v", err)
	}
	log.Println("Created GPT-5 Nano classifier")

	// Look for our specific test email
	streamKey := "user:test-user:in:email"
	messages, err := rdb.XRange(ctx, streamKey, "-", "+").Result()
	if err != nil {
		log.Printf("Error reading input stream: %v", err)
		return
	}

	log.Printf("Found %d emails in input stream", len(messages))

	// Find our test email (the most recent one with "Test Email" in subject)
	var targetMsg *redis.XMessage
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if subject, ok := msg.Values["subject"].(string); ok && strings.Contains(subject, "Test Email: Project Status Check") {
			targetMsg = &msg
			break
		}
	}

	if targetMsg == nil {
		log.Println("Test email not found in stream")
		return
	}

	log.Printf("Found test email ID: %s", targetMsg.ID)

	// Extract email data directly from message fields instead of raw_json parsing
	emailMsg := email_triage.EmailMessage{
		ID:        getString(targetMsg.Values, "message_id"),
		ThreadID:  getString(targetMsg.Values, "thread_id"),
		UserID:    getString(targetMsg.Values, "user_id"),
		From:      getString(targetMsg.Values, "from"),
		To:        getString(targetMsg.Values, "to"),
		Subject:   getString(targetMsg.Values, "subject"),
		Snippet:   getString(targetMsg.Values, "snippet"),
		BodyText:  getString(targetMsg.Values, "body_text"),
		Timestamp: time.Now().Unix(),
	}

	
	fmt.Printf("\n=== EMAIL TO CLASSIFY ===\n")
	fmt.Printf("From: %s\n", emailMsg.From)
	fmt.Printf("Subject: %s\n", emailMsg.Subject)
	fmt.Printf("Snippet: %s\n", emailMsg.Snippet)
	fmt.Printf("Body: %s\n", emailMsg.BodyText)

	// Create email content for classifier
	emailContent := email_triage.EmailContent{
		Subject: emailMsg.Subject,
		From:    emailMsg.From,
		Body:    emailMsg.BodyText,
		Snippet: emailMsg.Snippet,
	}

	fmt.Printf("✅ Processing with GPT-5 Nano...\n")

	// Classify with GPT-5 Nano
	result, err := classifier.ClassifyEmail(ctx, emailContent)
	if err != nil {
		log.Printf("Error classifying email: %v", err)
		return
	}

	fmt.Printf("\n=== GPT-5 NANO RESULT ===\n")
	fmt.Printf("Classification: %s\n", result.Classification)
	fmt.Printf("Requires Response: %t\n", result.RequiresResponse)
	fmt.Printf("Priority: %s\n", result.Priority)
	fmt.Printf("Confidence: %.2f\n", result.Confidence)
	fmt.Printf("Summary: %s\n", result.Summary)
	if result.DraftReply != "" {
		fmt.Printf("Draft Reply: %s\n", result.DraftReply)
	}
	fmt.Printf("Reasoning: %s\n", result.Reasoning)

	// Save to processed stream
	outputStreamKey := "user:test-user:processed:email"
	values := map[string]interface{}{
		"type":                "email_classified",
		"user_id":             emailMsg.UserID,
		"message_id":          emailMsg.ID,
		"classification":      result.Classification,
		"requires_response":   fmt.Sprintf("%v", result.RequiresResponse),
		"summary":             result.Summary,
		"draft_reply":         result.DraftReply,
		"priority":            result.Priority,
		"confidence":          fmt.Sprintf("%.2f", result.Confidence),
		"reasoning":           result.Reasoning,
		"processed_at":        time.Now().UTC().Format(time.RFC3339Nano),
	}

	if err := rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: outputStreamKey,
		Values: values,
	}).Err(); err != nil {
		log.Printf("Error saving processed email: %v", err)
	} else {
		fmt.Printf("\n✅ Saved to processed stream: %s\n", outputStreamKey)
	}

	log.Println("Classification test completed successfully!")
}

func getString(values map[string]interface{}, key string) string {
	if val, ok := values[key].(string); ok {
		return val
	}
	return ""
}