package main

import (
	"context"
	"fmt"
	"log"
	"net/smtp"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func main() {
	// Connect to Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	ctx := context.Background()

	// Get Gmail token from Redis
	tokenData, err := rdb.Get(ctx, "oauth_token:test-user:gmail").Result()
	if err != nil {
		log.Fatalf("Failed to get Gmail token: %v", err)
	}

	fmt.Printf("Found Gmail token: %s...\n", tokenData[:min(50, len(tokenData))])

	// Create a more comprehensive test email directly via SMTP
	from := "amanrahmanirocks@gmail.com"
	to := []string{"amanrahmanirocks@gmail.com"}

	// Email content
	subject := "Test Email: Project Status Check & Feedback Request - " + time.Now().Format("2006-01-02 15:04:05")
	body := `Hi Aman,

I'm following up on our discussion about the email triage system implementation. I wanted to test the complete workflow with a more substantial email that should properly trigger the GPT-5 Nano 3-role classification system.

Here are the specific questions I have:

1. Email Poller Status: Have you successfully implemented the timestamp filtering to prevent processing old unread emails?

2. GPT-5 Nano Classification: Is the classifier generating proper responses with all required fields (classification, priority, summary, draft_reply, reasoning)?

3. System Integration: Are the Redis streams working correctly for both input and processed emails?

4. Next Steps: What's your timeline for completing the email triage feature?

This email contains enough content that it should definitely trigger:
- Role 1: Response determination (this clearly requires a response)
- Role 2: Summarization (there are multiple specific questions to summarize)
- Role 3: Draft response preparation (specific questions that need answers)

Please let me know the status when you have a moment. I'd like to ensure the system is working correctly before we proceed further.

Best regards,
Test System

---
This is an automated test email to verify the email triage system functionality.`

	// Compose message
	message := fmt.Sprintf("To: %s\r\n", strings.Join(to, ", "))
	message += fmt.Sprintf("From: %s\r\n", from)
	message += fmt.Sprintf("Subject: %s\r\n", subject)
	message += "MIME-Version: 1.0\r\n"
	message += "Content-Type: text/plain; charset=\"utf-8\"\r\n"
	message += "Content-Transfer-Encoding: 7bit\r\n\r\n"
	message += body

	// Send using Gmail SMTP
	auth := smtp.PlainAuth("", from, "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", "smtp.gmail.com")
	err = smtp.SendMail("smtp.gmail.com:587", auth, from, to, []byte(message))
	if err != nil {
		log.Printf("Failed to send via SMTP: %v", err)

		// Try with Redis stream injection as fallback
		fmt.Println("Sending to Redis stream instead...")
		timestamp := time.Now().Unix()
		values := map[string]interface{}{
			"type":       "email_message",
			"user_id":    "test-user",
			"message_id": fmt.Sprintf("test-%d", timestamp),
			"thread_id":  fmt.Sprintf("test-thread-%d", timestamp),
			"from":       from,
			"to":         strings.Join(to, ","),
			"subject":    subject,
			"snippet":    "Hi Aman, I'm following up on our discussion...",
			"body_text":  body,
			"timestamp":  fmt.Sprintf("%d", timestamp),
			"raw_json": fmt.Sprintf(`{
				"from": "%s",
				"to": "%s",
				"subject": "%s",
				"body_text": "%s",
				"snippet": "Hi Aman, I'm following up on our discussion...",
				"message_id": "test-%d",
				"thread_id": "test-thread-%d",
				"user_id": "test-user"
			}`, from, strings.Join(to, ","), subject, strings.ReplaceAll(body, "\n", "\\n"), timestamp, timestamp),
		}

		err = rdb.XAdd(ctx, &redis.XAddArgs{
			Stream: "user:test-user:in:email",
			Values: values,
		}).Err()

		if err != nil {
			log.Fatalf("Failed to add to Redis stream: %v", err)
		}

		fmt.Println("✅ Email added to Redis stream for processing")
	} else {
		fmt.Println("✅ Email sent successfully via Gmail SMTP")
	}

	fmt.Printf("Subject: %s\n", subject)
	fmt.Printf("Body length: %d characters\n", len(body))
	fmt.Println("This email should trigger proper GPT-5 Nano classification.")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}