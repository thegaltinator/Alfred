package productivity

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

// NanoGenerator calls the GPT-5 Nano endpoint to derive expected apps/tabs.
type NanoGenerator struct {
	client       *http.Client
	apiURL       string
	apiKey       string
	model        string
	systemPrompt string
	maxTokens    int
}

const (
	defaultNanoTimeout      = 60 * time.Second
	defaultNanoModel        = "gpt-5-nano-2025-08-07"
	defaultSystemPromptPath = "subagents/productivity/system_prompts/productivity.system.md"
)

func NewNanoGeneratorFromEnv() (*NanoGenerator, error) {
	apiURL := strings.TrimSpace(os.Getenv("PRODUCTIVITY_MODEL_API_URL"))
	if apiURL == "" {
		apiURL = "https://api.openai.com/v1/chat/completions"
	}

	apiKey := strings.TrimSpace(os.Getenv("PRODUCTIVITY_MODEL_API_KEY"))
	if apiKey == "" {
		return nil, errors.New("PRODUCTIVITY_MODEL_API_KEY is required for productivity heuristic")
	}

	model := strings.TrimSpace(os.Getenv("PRODUCTIVITY_MODEL_NAME"))
	if model == "" {
		model = defaultNanoModel
	}

	systemPrompt := resolveSystemPrompt()

	maxTokens := 0
	if raw := strings.TrimSpace(os.Getenv("PRODUCTIVITY_MODEL_MAX_COMPLETION_TOKENS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxTokens = n
		}
	}

	return &NanoGenerator{
		client: &http.Client{
			Timeout: defaultNanoTimeout,
		},
		apiURL:       apiURL,
		apiKey:       apiKey,
		model:        model,
		systemPrompt: systemPrompt,
		maxTokens:    maxTokens,
	}, nil
}

func (g *NanoGenerator) ExpectedApps(ctx context.Context, payload EventPayload) ([]string, error) {
	if g == nil {
		return nil, errors.New("nano generator not initialized")
	}

	// Log request details
	log.Printf("NanoGenerator: requesting expected apps for event %q (%s)", payload.Title, payload.TimeBlock())

	body, err := buildNanoRequest(g.model, g.systemPrompt, g.maxTokens, payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build nano request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+g.apiKey)

	start := time.Now()
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call nano model: %w", err)
	}
	defer resp.Body.Close()
	duration := time.Since(start)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read nano response body: %w", err)
	}

	// Log raw response for debugging
	log.Printf("NanoGenerator: received status %d in %v. Response: %s", resp.StatusCode, duration, string(respBody))

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("nano model returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed chatCompletionResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode nano response: %w", err)
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return nil, errors.New("nano model returned empty response")
	}

	apps := parseExpectedApps(parsed.Choices[0].Message.Content)
	log.Printf("NanoGenerator: parsed apps: %v", apps)
	return apps, nil
}

// ClassifyForeground asks the model if the foreground app/window matches the event.
func (g *NanoGenerator) ClassifyForeground(ctx context.Context, payload EventPayload, foreground string) (bool, error) {
	if g == nil {
		return false, errors.New("nano generator not initialized")
	}

	// Prompt for classification
	userContent := fmt.Sprintf(
		"Task: %s\nDescription: %s\n\nCurrent Foreground: %s\n\nIs this foreground app/window essential for the task? Return JSON: {\"match\": true/false}",
		payload.Title,
		payload.Description,
		foreground,
	)

	req := chatCompletionRequest{
		Model: g.model,
		Messages: []chatMessage{
			{Role: "system", Content: "You are a productivity assistant. Decide if the user's current foreground app/window matches their scheduled task."},
			{Role: "user", Content: userContent},
		},
		ResponseFormat: &responseFormat{
			Type: "json_object",
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return false, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.apiURL, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+g.apiKey)

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return false, fmt.Errorf("call nano model: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 300 {
		return false, fmt.Errorf("nano model error %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed chatCompletionResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return false, errors.New("empty response from model")
	}

	content := parsed.Choices[0].Message.Content
	var result struct {
		Match bool `json:"match"`
	}
	// Handle potential markdown wrapping
	if strings.Contains(content, "```") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
	}

	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// Fallback: look for "true" in text if JSON fails
		lower := strings.ToLower(content)
		if strings.Contains(lower, "true") {
			return true, nil
		}
		return false, nil // Default to false on parse error
	}

	return result.Match, nil
}

type ExpectedAppsResponse struct {
	Apps          []string `json:"apps"`
	Domains       []string `json:"domains"`
	TitleKeywords []string `json:"title_keywords"`
}

func buildNanoRequest(model, systemPrompt string, maxTokens int, payload EventPayload) ([]byte, error) {
	userContent := fmt.Sprintf(
		"Event title: %s\nDescription: %s\nStart: %s\nEnd: %s\nTime block: %s\nReturn JSON with expected apps, domains, and title keywords.",
		payload.Title,
		payload.Description,
		payload.StartTime.Format(time.RFC3339),
		payload.EndTime.Format(time.RFC3339),
		payload.TimeBlock(),
	)

	req := chatCompletionRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userContent},
		},
		ResponseFormat: &responseFormat{
			Type: "json_object",
		},
	}
	if maxTokens > 0 {
		req.MaxCompletionTokens = maxTokens
	}

	return json.Marshal(req)
}

func parseExpectedApps(content string) []string {
	content = strings.TrimSpace(content)
	// Strip markdown code blocks if present
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}

	var resp ExpectedAppsResponse
	if err := json.Unmarshal([]byte(content), &resp); err == nil {
		// Flatten all fields into a single list of strings for now, as the heuristic service expects []string
		var result []string
		result = append(result, resp.Apps...)
		for _, d := range resp.Domains {
			result = append(result, "domain:"+d)
		}
		for _, k := range resp.TitleKeywords {
			result = append(result, "title:"+k)
		}
		return sanitizeApps(result)
	}

	// Fallback to old array parsing if JSON object fails
	var apps []string
	if json.Unmarshal([]byte(content), &apps) == nil {
		return sanitizeApps(apps)
	}
	
	return []string{}
}

func sanitizeApps(apps []string) []string {
	seen := make(map[string]struct{}, len(apps))
	out := make([]string, 0, len(apps))
	for _, a := range apps {
		trimmed := strings.TrimSpace(a)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func appsFromInterface(arr []interface{}) []string {
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

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
	Type       string      `json:"type"`
	JSONSchema *jsonSchema `json:"json_schema,omitempty"`
}

type jsonSchema struct {
	Name   string      `json:"name"`
	Schema interface{} `json:"schema"`
	Strict bool        `json:"strict,omitempty"`
}


func (g *NanoGenerator) expectedAppsViaPython(ctx context.Context, payload EventPayload) ([]string, error) {
	return nil, errors.New("python helper is deprecated")
}

func resolveSystemPrompt() string {
	if prompt := strings.TrimSpace(os.Getenv("PRODUCTIVITY_MODEL_SYSTEM_PROMPT")); prompt != "" {
		return prompt
	}
	if path := strings.TrimSpace(os.Getenv("PRODUCTIVITY_MODEL_SYSTEM_PROMPT_PATH")); path != "" {
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
	return "You are labeling expected windows for a task. Given the expected task description, return JSON with keys: apps (array of lowercase substrings that should appear in app names), domains (array of eTLD+1 or substrings), title_keywords (array of lowercase substrings). Keep lists small (<=10 each)."
}
