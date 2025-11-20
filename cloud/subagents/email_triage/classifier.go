package email_triage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// EmailClassifier uses GPT-5 Nano to classify emails and generate draft responses
type EmailClassifier struct {
	client       *http.Client
	apiURL       string
	apiKey       string
	model        string
	systemPrompt string
	maxTokens    int
	temperature  float32
	useTemp      bool
}

// ClassificationResult represents the output of email classification
type ClassificationResult struct {
	Classification   string  `json:"classification"`    // FYI, Question, Action Required, Information
	RequiresResponse bool    `json:"requires_response"` // Whether email needs a response
	Summary          string  `json:"summary"`           // One-line summary of email content
	DraftReply       string  `json:"draft_reply"`       // 1-2 sentence professional draft reply
	Priority         string  `json:"priority"`          // High, Medium, Low
	Confidence       float64 `json:"confidence"`        // Classification confidence 0-1
	Reasoning        string  `json:"reasoning"`         // Brief explanation of classification
}

// EmailContent represents the email content for classification
type EmailContent struct {
	Subject string
	From    string
	Body    string
	Snippet string
}

const (
	defaultClassifierTimeout    = 60 * time.Second
	defaultClassifierModel      = "gpt-5-nano-2025-08-07"
	defaultSystemPromptPath     = "subagents/email_triage/system_prompts/email_triage.system.md"
	defaultMaxCompletionTokens  = 500
	gpt5NanoMaxCompletionTokens = 10000
)

// NewEmailClassifier creates a new email classifier using GPT-5 Nano
func NewEmailClassifier() (*EmailClassifier, error) {
	apiURL := strings.TrimSpace(os.Getenv("EMAIL_TRIAGE_API_URL"))
	if apiURL == "" {
		apiURL = "https://api.openai.com/v1/chat/completions"
	}

	apiKey := strings.TrimSpace(os.Getenv("EMAIL_TRIAGE_API_KEY"))
	if apiKey == "" {
		return nil, errors.New("EMAIL_TRIAGE_API_KEY is required for email classification")
	}

	model := strings.TrimSpace(os.Getenv("EMAIL_TRIAGE_MODEL_NAME"))
	if model == "" {
		model = defaultClassifierModel
	}

	systemPrompt := resolveSystemPrompt()

	// Default temp: omit for GPT-5 Nano (only default supported)
	useTemp := true
	temp := float32(0.7)
	if strings.HasPrefix(model, "gpt-5-nano") {
		useTemp = false
		temp = 0
	}
	if raw := strings.TrimSpace(os.Getenv("EMAIL_TRIAGE_TEMPERATURE")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 32); err == nil && v >= 0 {
			temp = float32(v)
			useTemp = true
		}
	}

	maxTokens := defaultMaxCompletionTokens
	if strings.HasPrefix(model, "gpt-5-nano") {
		maxTokens = gpt5NanoMaxCompletionTokens
	}
	if raw := strings.TrimSpace(os.Getenv("EMAIL_TRIAGE_MAX_COMPLETION_TOKENS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxTokens = n
		}
	}
	if strings.HasPrefix(model, "gpt-5-nano") && maxTokens > gpt5NanoMaxCompletionTokens {
		maxTokens = gpt5NanoMaxCompletionTokens
	}

	return &EmailClassifier{
		client: &http.Client{
			Timeout: defaultClassifierTimeout,
		},
		apiURL:       apiURL,
		apiKey:       apiKey,
		model:        model,
		systemPrompt: systemPrompt,
		maxTokens:    maxTokens,
		temperature:  temp,
		useTemp:      useTemp,
	}, nil
}

// ClassifyEmail classifies an email and generates a draft response if needed
func (c *EmailClassifier) ClassifyEmail(ctx context.Context, email EmailContent) (*ClassificationResult, error) {
	if c == nil {
		return nil, errors.New("email classifier not initialized")
	}

	log.Printf("EmailClassifier: classifying email from %s, subject: %s", email.From, email.Subject)

	// Build the request for GPT-5 Nano
	body, err := c.buildClassificationRequest(email)
	if err != nil {
		return nil, fmt.Errorf("build classification request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	start := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call classification model: %w", err)
	}
	defer resp.Body.Close()
	duration := time.Since(start)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	log.Printf("EmailClassifier: received status %d in %v", resp.StatusCode, duration)

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("classification model error %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed chatCompletionResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode classification response: %w", err)
	}

	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return nil, errors.New("classification model returned empty response")
	}

	result, err := c.parseClassificationResponse(parsed.Choices[0].Message.Content)
	if err != nil {
		return nil, fmt.Errorf("parse classification result: %w", err)
	}

	log.Printf("EmailClassifier: classified as %s, requires_response=%t", result.Classification, result.RequiresResponse)
	return result, nil
}

// buildClassificationRequest creates the JSON request for email classification
func (c *EmailClassifier) buildClassificationRequest(email EmailContent) ([]byte, error) {
	// Truncate very long emails to stay within token limits
	maxBodyLength := 2000
	bodyText := email.Body
	if len(bodyText) > maxBodyLength {
		bodyText = bodyText[:maxBodyLength] + "... [truncated]"
	}

	userContent := fmt.Sprintf(
		"From: %s\nSubject: %s\nBody: %s\n\nClassify this email and generate a response if needed.",
		email.From,
		email.Subject,
		bodyText,
	)

	req := chatCompletionRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: c.systemPrompt},
			{Role: "user", Content: userContent},
		},
		ResponseFormat: &responseFormat{
			Type: "json_object",
		},
		MaxCompletionTokens: c.maxTokens,
	}
	if c.useTemp {
		req.Temperature = c.temperature
	}

	return json.Marshal(req)
}

// parseClassificationResponse parses the JSON response from the classification model
func (c *EmailClassifier) parseClassificationResponse(content string) (*ClassificationResult, error) {
	content = strings.TrimSpace(content)

	// Strip markdown code blocks if present
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}

	var result ClassificationResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// Try to extract information from malformed response
		log.Printf("Warning: Failed to parse classification JSON: %v. Content: %s", err, content)
		return c.fallbackClassification(content), nil
	}

	// Validate and normalize the classification
	result.Classification = c.normalizeClassification(result.Classification)

	// Ensure draft reply is only present when response is required
	if !result.RequiresResponse {
		result.DraftReply = ""
	} else if result.DraftReply == "" {
		result.DraftReply = "Thank you for your message. I'll review this and get back to you shortly."
	}

	// Set default priority if not provided
	if result.Priority == "" {
		result.Priority = c.determinePriority(result.Classification)
	}

	return &result, nil
}

// normalizeClassification ensures classification values are valid
func (c *EmailClassifier) normalizeClassification(classification string) string {
	switch strings.ToLower(strings.TrimSpace(classification)) {
	case "question", "q":
		return "Question"
	case "action required", "action", "urgent":
		return "Action Required"
	case "fyi", "informational":
		return "FYI"
	case "information", "info":
		return "Information"
	default:
		return "Information" // Default fallback
	}
}

// determinePriority sets priority based on classification
func (c *EmailClassifier) determinePriority(classification string) string {
	switch classification {
	case "Action Required":
		return "High"
	case "Question":
		return "Medium"
	default:
		return "Low"
	}
}

// fallbackClassification provides basic classification when JSON parsing fails
func (c *EmailClassifier) fallbackClassification(content string) *ClassificationResult {
	lowerContent := strings.ToLower(content)

	result := &ClassificationResult{
		Classification:   "Information",
		RequiresResponse: false,
		Summary:          "Email processing failed",
		DraftReply:       "",
		Priority:         "Low",
		Confidence:       0.1,
		Reasoning:        "JSON parsing failed, using fallback",
	}

	// Simple keyword-based fallback
	if strings.Contains(lowerContent, "question") || strings.Contains(lowerContent, "?") {
		result.Classification = "Question"
		result.RequiresResponse = true
		result.Priority = "Medium"
		result.DraftReply = "Thank you for your question. I'll review and respond accordingly."
	} else if strings.Contains(lowerContent, "urgent") || strings.Contains(lowerContent, "action required") {
		result.Classification = "Action Required"
		result.RequiresResponse = true
		result.Priority = "High"
		result.DraftReply = "I've received your urgent request and will prioritize it accordingly."
	} else if strings.Contains(lowerContent, "fyi") || strings.Contains(lowerContent, "for your information") {
		result.Classification = "FYI"
		result.RequiresResponse = false
		result.Priority = "Low"
	}

	return result
}

// resolveSystemPrompt loads the system prompt from environment or file
func resolveSystemPrompt() string {
	if prompt := strings.TrimSpace(os.Getenv("EMAIL_TRIAGE_SYSTEM_PROMPT")); prompt != "" {
		return prompt
	}
	if path := strings.TrimSpace(os.Getenv("EMAIL_TRIAGE_SYSTEM_PROMPT_PATH")); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
				return trimmed
			}
		}
	}
	if data, err := os.ReadFile(defaultSystemPromptPath); err == nil {
		if trimmed := strings.TrimSpace(string(data)); trimmed != "" {
			return trimmed
		}
	}

	// Fallback system prompt
	return `You are an email triage assistant. Classify emails into one of these categories:
- FYI: Informational only, no action required
- Question: Direct inquiry requiring response
- Action Required: Explicit task or urgent request
- Information: General correspondence

For each email, determine:
1. Classification (one of the four categories)
2. requires_response (boolean)
3. summary (brief 1-2 sentence summary)
4. draft_reply (professional 1-2 sentence response, only when requires_response=true)
5. priority (High/Medium/Low)
6. confidence (0-1)
7. reasoning (brief explanation)

Return JSON with these fields.`
}

// HTTP request/response types for OpenAI API compatibility
type chatCompletionRequest struct {
	Model               string          `json:"model"`
	Messages            []chatMessage   `json:"messages"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Temperature         float32         `json:"temperature,omitempty"`
	ResponseFormat      *responseFormat `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

type responseFormat struct {
	Type string `json:"type"`
}
