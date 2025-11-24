package manager

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

// LLMClient wraps calls to the Manager model (GPT-5 Mini).
type LLMClient struct {
	client       *http.Client
	apiURL       string
	apiKey       string
	model        string
	systemPrompt string
	temperature  float32
	timeout      time.Duration
}

const (
	defaultManagerModel       = "gpt-5-mini-2025-08-07"
	defaultManagerAPIURL      = "https://api.openai.com/v1/chat/completions"
	defaultSystemPromptPath   = "manager/system_prompts/manager.system.md"
	defaultManagerTimeout     = 60 * time.Second
	defaultManagerTemperature = 1.0
	managerResponseSchemaName = "manager_decision"
)

// NewLLMClientFromEnv builds the Manager LLM client using shared API keys.
func NewLLMClientFromEnv() (*LLMClient, error) {
	apiURL := strings.TrimSpace(os.Getenv("MANAGER_API_URL"))
	if apiURL == "" {
		apiURL = defaultManagerAPIURL
	}

	apiKey := resolveAPIKey()
	if apiKey == "" {
		return nil, errors.New("manager API key missing (set MANAGER_API_KEY or reuse PRODUCTIVITY_MODEL_API_KEY/EMAIL_TRIAGE_API_KEY/CEREBRAS_API_KEY)")
	}

	model := strings.TrimSpace(os.Getenv("MANAGER_MODEL_NAME"))
	if model == "" {
		model = defaultManagerModel
	}

	systemPrompt := resolveManagerPrompt()

	timeout := defaultManagerTimeout
	if raw := strings.TrimSpace(os.Getenv("MANAGER_TIMEOUT")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			timeout = d
		}
	}

	temp := float32(defaultManagerTemperature)
	if raw := strings.TrimSpace(os.Getenv("MANAGER_TEMPERATURE")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 32); err == nil && v >= 0 {
			temp = float32(v)
		}
	}

	return &LLMClient{
		client:       &http.Client{Timeout: timeout},
		apiURL:       apiURL,
		apiKey:       apiKey,
		model:        model,
		systemPrompt: systemPrompt,
		temperature:  temp,
		timeout:      timeout,
	}, nil
}

// Decide calls the Manager model to map a subagent event to an action.
func (c *LLMClient) Decide(ctx context.Context, evt Event) (Decision, error) {
	if c == nil {
		return Decision{}, errors.New("manager LLM client not initialized")
	}

	body, err := c.buildRequest(evt)
	if err != nil {
		return Decision{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return Decision{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	start := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		return Decision{}, fmt.Errorf("call manager model: %w", err)
	}
	defer resp.Body.Close()
	duration := time.Since(start)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Decision{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 300 {
		return Decision{}, fmt.Errorf("manager model returned %d: %s", resp.StatusCode, string(respBody))
	}

	decision, err := parseManagerDecision(respBody)
	if err != nil {
		log.Printf("Manager LLM returned unparseable payload in %v: %v", duration, err)
		return Decision{}, err
	}

	return decision, nil
}

func (c *LLMClient) buildRequest(evt Event) ([]byte, error) {
	payloadJSON := "{}"
	if len(evt.Payload) > 0 {
		if b, err := json.Marshal(evt.Payload); err == nil {
			payloadJSON = string(b)
		}
	}

	userPrompt := fmt.Sprintf(
		"Subagent output received.\nsource: %s\nkind: %s\npayload: %s\n\nDecide the next manager action.",
		evt.Source, evt.Kind, payloadJSON,
	)

	req := chatCompletionRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: c.systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}

	req.Temperature = c.temperature

	return json.Marshal(req)
}

func parseManagerDecision(body []byte) (Decision, error) {
	var parsed chatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Decision{}, fmt.Errorf("decode model response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Decision{}, errors.New("manager model returned no choices")
	}

	content := parsed.Choices[0].Message.Content
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")

	var out Decision
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return Decision{}, fmt.Errorf("parse decision JSON: %w", err)
	}

	if out.Action == "" {
		return Decision{}, errors.New("manager decision missing action")
	}

	return out, nil
}

func resolveAPIKey() string {
	for _, key := range []string{
		"MANAGER_API_KEY",
		"PRODUCTIVITY_MODEL_API_KEY",
		"EMAIL_TRIAGE_API_KEY",
		"CEREBRAS_API_KEY",
	} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func resolveManagerPrompt() string {
	if prompt := strings.TrimSpace(os.Getenv("MANAGER_SYSTEM_PROMPT")); prompt != "" {
		return prompt
	}
	if path := strings.TrimSpace(os.Getenv("MANAGER_SYSTEM_PROMPT_PATH")); path != "" {
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
	return "You are the Alfred Manager. Decide the minimal next step for every subagent emission."
}

type chatCompletionRequest struct {
	Model               string        `json:"model"`
	Messages            []chatMessage `json:"messages"`
	MaxCompletionTokens int           `json:"max_completion_tokens,omitempty"`
	Temperature         float32       `json:"temperature,omitempty"`
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
