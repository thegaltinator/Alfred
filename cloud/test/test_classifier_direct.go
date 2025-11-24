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
	// Set API key for GPT-5 Nano classifier from environment
	// The key should already be set in the .env file

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

	// Check for emails in input stream
	streamKey := "user:dev-user:in:email"
	messages, err := rdb.XRange(ctx, streamKey, "-", "+").Result()
	if err != nil {
		log.Printf("Error reading input stream: %v", err)
		return
	}

	log.Printf("Found %d emails in input stream", len(messages))

	if len(messages) == 0 {
		log.Println("No emails found in stream")
		return
	}

	// Process the most recent email
	lastMsg := messages[len(messages)-1]
	log.Printf("Processing email ID: %s", lastMsg.ID)

	// Extract email data from raw_json
	var rawJSON string
	if val, ok := lastMsg.Values["raw_json"]; ok {
		if str, ok := val.(string); ok {
			rawJSON = str
		}
	}

	if rawJSON == "" {
		log.Println("No raw_json found in email")
		return
	}

	// Parse the email message
	var emailMsg email_triage.EmailMessage
	if err := json.Unmarshal([]byte(rawJSON), &emailMsg); err != nil {
		log.Printf("Error parsing email: %v", err)
		return
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

	// Check if this is a question email that should be processed
	if !shouldProcessEmail(&emailMsg) {
		fmt.Printf("⚠️  Email doesn't require response (likely newsletter/automated)\n")
		return
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
	outputStreamKey := "user:dev-user:processed:email"
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

	log.Println("Classification test completed")
}

func shouldProcessEmail(emailMsg *email_triage.EmailMessage) bool {
	if emailMsg == nil {
		return false
	}

	// Skip newsletters, notifications, and automated messages
	subjectLower := strings.ToLower(emailMsg.Subject)
	snippetLower := strings.ToLower(emailMsg.Snippet)
	fromLower := strings.ToLower(emailMsg.From)

	automatedKeywords := []string{
		"unsubscribe", "notification", "alert", "reminder",
		"receipt", "invoice", "order confirmation", "shipping",
		"newsletter", "weekly digest", "update", "noreply",
		"no-reply", "donotreply", "google alerts", "funclick",
	}

	for _, keyword := range automatedKeywords {
		if strings.Contains(subjectLower, keyword) ||
		   strings.Contains(snippetLower, keyword) ||
		   strings.Contains(fromLower, keyword) {
			return false
		}
	}

	// Look for indicators that a response is needed
	responseKeywords := []string{
		"?", "question", "confirm", "can you", "could you",
		"please", "help", "advice", "opinion", "feedback",
		"meeting", "call", "discuss", "review", "approval",
		"decision", "input", "thoughts", "suggestion",
	}

	bodyLower := strings.ToLower(emailMsg.BodyText)
	for _, keyword := range responseKeywords {
		if strings.Contains(subjectLower, keyword) ||
		   strings.Contains(snippetLower, keyword) ||
		   strings.Contains(bodyLower, keyword) {
			return true
		}
	}

	return false
}