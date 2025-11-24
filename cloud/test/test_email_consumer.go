package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"alfred-cloud/subagents/email_triage"
	"github.com/redis/go-redis/v9"
)

func main() {
	// Set API key for GPT-5 Nano classifier
	os.Setenv("EMAIL_TRIAGE_API_KEY", "test-key") // Replace with real key

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

	// Create consumer
	consumer := email_triage.NewEmailConsumer(rdb, classifier, []string{"dev-user"})

	// Set up consumer groups (private method - need to access differently)
	err := consumer.Start(ctx)
	if err != nil {
		log.Printf("Warning starting consumer: %v", err)
	}
	defer consumer.Stop()

	// Check for existing emails in input stream
	streamKey := "user:dev-user:in:email"
	messages, err := rdb.XRange(ctx, streamKey, "-", "+").Result()
	if err != nil {
		log.Printf("Error reading input stream: %v", err)
		return
	}

	log.Printf("Found %d emails in input stream", len(messages))

	// Process all pending messages
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		log.Printf("Processing email %d/%d: %s", len(messages)-i, len(messages), msg.ID)

		// Process this email using the consumer
		processed := consumer.ProcessUserMessages(ctx, "dev-user")
		if processed {
			log.Printf("✅ Processed email %s", msg.ID)
		} else {
			log.Printf("⚠️  No new messages to process for email %s", msg.ID)
		}

		// Small delay between processing
		time.Sleep(1 * time.Second)
	}

	// Check for processed emails
	outputStreamKey := "user:dev-user:processed:email"
	processedMessages, err := rdb.XRange(ctx, outputStreamKey, "-", "+").Result()
	if err != nil {
		log.Printf("Error reading output stream: %v", err)
		return
	}

	log.Printf("Found %d processed emails in output stream", len(processedMessages))

	// Show details of processed emails
	for _, msg := range processedMessages {
		fmt.Printf("\n=== PROCESSED EMAIL ===\n")
		fmt.Printf("ID: %s\n", msg.ID)
		fmt.Printf("Type: %s\n", msg.Values["type"])
		if classification, ok := msg.Values["classification"]; ok {
			fmt.Printf("Classification: %s\n", classification)
		}
		if summary, ok := msg.Values["summary"]; ok {
			fmt.Printf("Summary: %s\n", summary)
		}
		if draftReply, ok := msg.Values["draft_reply"]; ok && draftReply != "" {
			fmt.Printf("Draft Reply: %s\n", draftReply)
		}
		if confidence, ok := msg.Values["confidence"]; ok {
			fmt.Printf("Confidence: %s\n", confidence)
		}
	}

	log.Println("Email triage test completed")
}