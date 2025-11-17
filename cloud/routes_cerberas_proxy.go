package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type cerberasProxyRequest struct {
	Message string `json:"message"`
	Model   string `json:"model"`
}

type cerberasChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Text string `json:"text"`
	} `json:"choices"`
	Output []struct {
		Content string `json:"content"`
	} `json:"output"`
}

func cerberasProxyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req cerberasProxyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid request: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		http.Error(w, `{"error":"message is required"}`, http.StatusBadRequest)
		return
	}

	if req.Model == "" {
		req.Model = os.Getenv("CEREBRAS_MODEL")
	}

	apiKey := os.Getenv("CEREBRAS_API_KEY")
	if apiKey == "" {
		http.Error(w, `{"error":"server missing CEREBRAS_API_KEY"}`, http.StatusInternalServerError)
		return
	}

	baseURL := os.Getenv("CEREBRAS_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.cerebras.ai/v1"
	}

	responseText, err := forwardToCerberas(apiKey, baseURL, req.Model, req.Message)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadGateway)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"response": responseText,
	})
}

func forwardToCerberas(apiKey, baseURL, model, message string) (string, error) {
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": message,
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/chat/completions", baseURL), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cerberas error %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp cerberasChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err == nil {
		if len(chatResp.Choices) > 0 {
			if text := chatResp.Choices[0].Message.Content; text != "" {
				return text, nil
			}
			if text := chatResp.Choices[0].Text; text != "" {
				return text, nil
			}
		}
		if len(chatResp.Output) > 0 && chatResp.Output[0].Content != "" {
			return chatResp.Output[0].Content, nil
		}
	}

	// fallback: return raw body string
	if len(respBody) > 0 {
		return string(respBody), nil
	}

	return "", errors.New("cerberas response empty")
}
