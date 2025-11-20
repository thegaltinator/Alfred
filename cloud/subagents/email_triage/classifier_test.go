package email_triage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Mock HTTP server for testing the classifier
func createMockClassifierServer(response string, statusCode int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		w.Write([]byte(response))
	}))
}

func TestNewEmailClassifier(t *testing.T) {
	tests := []struct {
		name        string
		envVars     map[string]string
		expectError bool
	}{
		{
			name: "valid environment variables",
			envVars: map[string]string{
				"EMAIL_TRIAGE_API_KEY": "test-key",
				"EMAIL_TRIAGE_API_URL": "https://api.test.com",
				"EMAIL_TRIAGE_MODEL_NAME": "test-model",
			},
			expectError: false,
		},
		{
			name: "missing API key",
			envVars: map[string]string{
				"EMAIL_TRIAGE_API_URL": "https://api.test.com",
			},
			expectError: true,
		},
		{
			name: "using defaults",
			envVars: map[string]string{
				"EMAIL_TRIAGE_API_KEY": "test-key",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}
			defer func() {
				// Clean up environment variables
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			classifier, err := NewEmailClassifier()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if classifier != nil {
					t.Error("Expected nil classifier on error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if classifier == nil {
					t.Error("Expected non-nil classifier")
				}
			}
		})
	}
}

func TestClassifyEmail(t *testing.T) {
	// Mock response for classification question email
	mockResponse := `{
		"choices": [{
			"message": {
				"content": "{\"classification\": \"Question\", \"requires_response\": true, \"summary\": \"User asking for 3pm meeting confirmation\", \"draft_reply\": \"Yes, I can confirm the meeting for 3pm. Looking forward to it.\", \"priority\": \"Medium\", \"confidence\": 0.9, \"reasoning\": \"Email contains direct question requiring confirmation\"}"
			}
		}]
	}`

	server := createMockClassifierServer(mockResponse, http.StatusOK)
	defer server.Close()

	// Set up environment variables for test
	os.Setenv("EMAIL_TRIAGE_API_KEY", "test-key")
	os.Setenv("EMAIL_TRIAGE_API_URL", server.URL)
	defer func() {
		os.Unsetenv("EMAIL_TRIAGE_API_KEY")
		os.Unsetenv("EMAIL_TRIAGE_API_URL")
	}()

	classifier, err := NewEmailClassifier()
	if err != nil {
		t.Fatalf("Failed to create classifier: %v", err)
	}

	ctx := context.Background()
	email := EmailContent{
		Subject: "Can you confirm 3pm?",
		From:    "colleague@example.com",
		Body:    "Hi, can we confirm our meeting at 3pm today? Let me know if that works for you.",
		Snippet: "Can we confirm our meeting at 3pm today?",
	}

	result, err := classifier.ClassifyEmail(ctx, email)
	if err != nil {
		t.Fatalf("Classification failed: %v", err)
	}

	// Verify classification results
	if result.Classification != "Question" {
		t.Errorf("Expected classification 'Question', got '%s'", result.Classification)
	}
	if !result.RequiresResponse {
		t.Error("Expected requires_response to be true")
	}
	if result.Priority != "Medium" {
		t.Errorf("Expected priority 'Medium', got '%s'", result.Priority)
	}
	if result.Summary == "" {
		t.Error("Expected non-empty summary")
	}
	if result.DraftReply == "" {
		t.Error("Expected non-empty draft reply")
	}
	if result.Confidence <= 0 {
		t.Error("Expected positive confidence score")
	}
}

func TestClassifyEmail_ActionRequired(t *testing.T) {
	mockResponse := `{
		"choices": [{
			"message": {
				"content": "{\"classification\": \"Action Required\", \"requires_response\": true, \"summary\": \"Urgent request for project approval\", \"draft_reply\": \"I'll review the project immediately and provide approval within the hour.\", \"priority\": \"High\", \"confidence\": 0.95, \"reasoning\": \"Email contains urgent action items\"}"
			}
		}]
	}`

	server := createMockClassifierServer(mockResponse, http.StatusOK)
	defer server.Close()

	os.Setenv("EMAIL_TRIAGE_API_KEY", "test-key")
	os.Setenv("EMAIL_TRIAGE_API_URL", server.URL)
	defer func() {
		os.Unsetenv("EMAIL_TRIAGE_API_KEY")
		os.Unsetenv("EMAIL_TRIAGE_API_URL")
	}()

	classifier, err := NewEmailClassifier()
	if err != nil {
		t.Fatalf("Failed to create classifier: %v", err)
	}

	ctx := context.Background()
	email := EmailContent{
		Subject: "URGENT: Project approval needed",
		From:    "manager@example.com",
		Body:    "Please approve the attached project proposal immediately. This is time-sensitive.",
		Snippet: "Please approve the attached project proposal immediately.",
	}

	result, err := classifier.ClassifyEmail(ctx, email)
	if err != nil {
		t.Fatalf("Classification failed: %v", err)
	}

	if result.Classification != "Action Required" {
		t.Errorf("Expected classification 'Action Required', got '%s'", result.Classification)
	}
	if result.Priority != "High" {
		t.Errorf("Expected priority 'High', got '%s'", result.Priority)
	}
}

func TestClassifyEmail_FYI(t *testing.T) {
	mockResponse := `{
		"choices": [{
			"message": {
				"content": "{\"classification\": \"FYI\", \"requires_response\": false, \"summary\": \"Weekly team update with project status\", \"draft_reply\": \"\", \"priority\": \"Low\", \"confidence\": 0.85, \"reasoning\": \"Informational email with no action required\"}"
			}
		}]
	}`

	server := createMockClassifierServer(mockResponse, http.StatusOK)
	defer server.Close()

	os.Setenv("EMAIL_TRIAGE_API_KEY", "test-key")
	os.Setenv("EMAIL_TRIAGE_API_URL", server.URL)
	defer func() {
		os.Unsetenv("EMAIL_TRIAGE_API_KEY")
		os.Unsetenv("EMAIL_TRIAGE_API_URL")
	}()

	classifier, err := NewEmailClassifier()
	if err != nil {
		t.Fatalf("Failed to create classifier: %v", err)
	}

	ctx := context.Background()
	email := EmailContent{
		Subject: "FYI: Weekly Team Update",
		From:    "team-lead@example.com",
		Body:    "Here's the weekly update on all project progress. No action needed.",
		Snippet: "Here's the weekly update on all project progress.",
	}

	result, err := classifier.ClassifyEmail(ctx, email)
	if err != nil {
		t.Fatalf("Classification failed: %v", err)
	}

	if result.Classification != "FYI" {
		t.Errorf("Expected classification 'FYI', got '%s'", result.Classification)
	}
	if result.RequiresResponse {
		t.Error("Expected requires_response to be false for FYI")
	}
	if result.DraftReply != "" {
		t.Error("Expected empty draft reply for FYI")
	}
	if result.Priority != "Low" {
		t.Errorf("Expected priority 'Low', got '%s'", result.Priority)
	}
}

func TestClassifyEmail_APIError(t *testing.T) {
	server := createMockClassifierServer("Internal Server Error", http.StatusInternalServerError)
	defer server.Close()

	os.Setenv("EMAIL_TRIAGE_API_KEY", "test-key")
	os.Setenv("EMAIL_TRIAGE_API_URL", server.URL)
	defer func() {
		os.Unsetenv("EMAIL_TRIAGE_API_KEY")
		os.Unsetenv("EMAIL_TRIAGE_API_URL")
	}()

	classifier, err := NewEmailClassifier()
	if err != nil {
		t.Fatalf("Failed to create classifier: %v", err)
	}

	ctx := context.Background()
	email := EmailContent{
		Subject: "Test",
		From:    "test@example.com",
		Body:    "Test content",
		Snippet: "Test snippet",
	}

	_, err = classifier.ClassifyEmail(ctx, email)
	if err == nil {
		t.Error("Expected error from API but got none")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("Expected API error in message, got: %v", err)
	}
}

func TestClassifyEmail_MalformedJSON(t *testing.T) {
	// Response with malformed JSON content
	mockResponse := `{
		"choices": [{
			"message": {
				"content": "This is not valid JSON content"
			}
		}]
	}`

	server := createMockClassifierServer(mockResponse, http.StatusOK)
	defer server.Close()

	os.Setenv("EMAIL_TRIAGE_API_KEY", "test-key")
	os.Setenv("EMAIL_TRIAGE_API_URL", server.URL)
	defer func() {
		os.Unsetenv("EMAIL_TRIAGE_API_KEY")
		os.Unsetenv("EMAIL_TRIAGE_API_URL")
	}()

	classifier, err := NewEmailClassifier()
	if err != nil {
		t.Fatalf("Failed to create classifier: %v", err)
	}

	ctx := context.Background()
	email := EmailContent{
		Subject: "Test question?",
		From:    "test@example.com",
		Body:    "Is this a question?",
		Snippet: "Is this a question?",
	}

	result, err := classifier.ClassifyEmail(ctx, email)
	if err != nil {
		t.Fatalf("Unexpected error during classification: %v", err)
	}

	// Should fall back to basic classification
	if result == nil {
		t.Error("Expected fallback classification result but got nil")
	}
}

func TestParseClassificationResponse(t *testing.T) {
	classifier := &EmailClassifier{}

	tests := []struct {
		name           string
		content        string
		expectedResult *ClassificationResult
		expectError    bool
	}{
		{
			name:    "valid JSON response",
			content: `{"classification": "Question", "requires_response": true, "summary": "Test", "draft_reply": "Reply", "priority": "Medium", "confidence": 0.8, "reasoning": "Test"}`,
			expectedResult: &ClassificationResult{
				Classification:   "Question",
				RequiresResponse: true,
				Summary:          "Test",
				DraftReply:       "Reply",
				Priority:         "Medium",
				Confidence:       0.8,
				Reasoning:        "Test",
			},
			expectError: false,
		},
		{
			name:    "JSON with markdown wrapper",
			content: "```json\n{\"classification\": \"FYI\", \"requires_response\": false, \"summary\": \"Info\", \"draft_reply\": \"\", \"priority\": \"Low\", \"confidence\": 0.9, \"reasoning\": \"FYI\"}\n```",
			expectedResult: &ClassificationResult{
				Classification:   "FYI",
				RequiresResponse: false,
				Summary:          "Info",
				DraftReply:       "",
				Priority:         "Low",
				Confidence:       0.9,
				Reasoning:        "FYI",
			},
			expectError: false,
		},
		{
			name:           "malformed JSON",
			content:        `{"classification": "Question", "invalid":}`,
			expectError:    false, // Should not error, should use fallback
			expectedResult: nil, // We'll get a fallback result
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := classifier.parseClassificationResponse(tt.content)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}

			if tt.expectedResult != nil {
				if result == nil {
					t.Error("Expected non-nil result")
					return
				}
				if result.Classification != tt.expectedResult.Classification {
					t.Errorf("Expected classification %s, got %s", tt.expectedResult.Classification, result.Classification)
				}
				if result.RequiresResponse != tt.expectedResult.RequiresResponse {
					t.Errorf("Expected requires_response %t, got %t", tt.expectedResult.RequiresResponse, result.RequiresResponse)
				}
			}
		})
	}
}

func TestNormalizeClassification(t *testing.T) {
	classifier := &EmailClassifier{}

	tests := []struct {
		input    string
		expected string
	}{
		{"question", "Question"},
		{"Question", "Question"},
		{"action required", "Action Required"},
		{"urgent", "Action Required"},
		{"FYI", "FYI"},
		{"informational", "FYI"},
		{"information", "Information"},
		{"info", "Information"},
		{"unknown", "Information"}, // Default fallback
		{"", "Information"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := classifier.normalizeClassification(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestDeterminePriority(t *testing.T) {
	classifier := &EmailClassifier{}

	tests := []struct {
		classification string
		expected       string
	}{
		{"Action Required", "High"},
		{"Question", "Medium"},
		{"FYI", "Low"},
		{"Information", "Low"},
	}

	for _, tt := range tests {
		t.Run(tt.classification, func(t *testing.T) {
			result := classifier.determinePriority(tt.classification)
			if result != tt.expected {
				t.Errorf("Expected priority %s for classification %s, got %s", tt.expected, tt.classification, result)
			}
		})
	}
}

func TestFallbackClassification(t *testing.T) {
	classifier := &EmailClassifier{}

	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "question keyword",
			content:  "This content contains a question mark ?",
			expected: "Question",
		},
		{
			name:     "urgent keyword",
			content:  "This is urgent action required",
			expected: "Action Required",
		},
		{
			name:     "FYI keyword",
			content:  "FYI this is for your information",
			expected: "FYI",
		},
		{
			name:     "default case",
			content:  "Just some random content",
			expected: "Information",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifier.fallbackClassification(tt.content)
			if result.Classification != tt.expected {
				t.Errorf("Expected classification %s, got %s", tt.expected, result.Classification)
			}
		})
	}
}

func TestBuildClassificationRequest(t *testing.T) {
	classifier := &EmailClassifier{
		model:        "test-model",
		systemPrompt: "Test system prompt",
		maxTokens:    100,
	}

	email := EmailContent{
		Subject: "Test Subject",
		From:    "sender@example.com",
		Body:    "This is the email body content",
		Snippet: "Email snippet",
	}

	body, err := classifier.buildClassificationRequest(email)
	if err != nil {
		t.Fatalf("buildClassificationRequest failed: %v", err)
	}

	var req chatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("Failed to unmarshal request: %v", err)
	}

	if req.Model != "test-model" {
		t.Errorf("Expected model 'test-model', got '%s'", req.Model)
	}

	if len(req.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(req.Messages))
	}

	if req.Messages[0].Role != "system" {
		t.Errorf("Expected first message role 'system', got '%s'", req.Messages[0].Role)
	}

	if req.Messages[0].Content != "Test system prompt" {
		t.Errorf("Expected system prompt 'Test system prompt', got '%s'", req.Messages[0].Content)
	}

	if req.Messages[1].Role != "user" {
		t.Errorf("Expected second message role 'user', got '%s'", req.Messages[1].Role)
	}

	userContent := req.Messages[1].Content
	if !strings.Contains(userContent, "sender@example.com") {
		t.Error("Expected user content to contain sender email")
	}
	if !strings.Contains(userContent, "Test Subject") {
		t.Error("Expected user content to contain subject")
	}
	if !strings.Contains(userContent, "This is the email body content") {
		t.Error("Expected user content to contain body")
	}

	if req.MaxCompletionTokens != 100 {
		t.Errorf("Expected max completion tokens 100, got %d", req.MaxCompletionTokens)
	}
}

func TestTruncateEmailBody(t *testing.T) {
	classifier := &EmailClassifier{}

	// Create a long email body
	longBody := strings.Repeat("This is a long sentence. ", 100) // Much longer than 2000 chars

	email := EmailContent{
		Subject: "Test",
		From:    "test@example.com",
		Body:    longBody,
		Snippet: "Test",
	}

	body, err := classifier.buildClassificationRequest(email)
	if err != nil {
		t.Fatalf("buildClassificationRequest failed: %v", err)
	}

	var req chatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("Failed to unmarshal request: %v", err)
	}

	userContent := req.Messages[1].Content
	if !strings.Contains(userContent, "[truncated]") {
		t.Error("Expected long body to be truncated with marker")
	}
}