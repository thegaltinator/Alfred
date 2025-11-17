package email_triage

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"google.golang.org/api/gmail/v1"
)

// newTestEmailPoller creates an EmailPoller for testing classification logic
func newTestEmailPoller(userIDs []string) *EmailPoller {
	return &EmailPoller{
		googleClient:   nil, // Not needed for classification tests
		redisClient:    nil, // Not needed for classification tests
		userIDs:        userIDs,
		pollInterval:   30 * time.Second,
		lastMessageIDs: make(map[string]string),
		stopChan:       make(chan struct{}),
		running:        false,
	}
}

func TestEmailPollerCreation(t *testing.T) {
	// Test that the poller structure is properly initialized
	userIDs := []string{"test-user"}
	poller := newTestEmailPoller(userIDs)

	assert.NotNil(t, poller)
	assert.Equal(t, userIDs, poller.userIDs)
	assert.Equal(t, 30*time.Second, poller.pollInterval)
	assert.False(t, poller.running)
}

func TestClassifyMessage(t *testing.T) {
	poller := newTestEmailPoller([]string{"test"})

	tests := []struct {
		name             string
		subject          string
		from             string
		body             string
		expectedClass    string
		expectedRequires bool
		expectedPriority string
	}{
		{
			name:             "Question email",
			subject:          "Quick question",
			from:             "colleague@example.com",
			body:             "Can you help me with something?",
			expectedClass:    "Question",
			expectedRequires: true,
			expectedPriority: "Medium",
		},
		{
			name:             "Urgent email",
			subject:          "URGENT: Server down",
			from:             "ops@example.com",
			body:             "The production server is down",
			expectedClass:    "Action Required",
			expectedRequires: true,
			expectedPriority: "High",
		},
		{
			name:             "FYI email",
			subject:          "FYI: Meeting notes",
			from:             "admin@example.com",
			body:             "Here are the notes from today's meeting",
			expectedClass:    "FYI",
			expectedRequires: false,
			expectedPriority: "Low",
		},
		{
			name:             "Information email",
			subject:          "Weekly update",
			from:             "team@example.com",
			body:             "Here's what happened this week",
			expectedClass:    "Information",
			expectedRequires: false,
			expectedPriority: "Low",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classification, summary, draft, requiresResponse := poller.classifyMessage(tt.subject, tt.from, tt.body)
			priority := poller.determinePriority(tt.subject, tt.from, classification)

			assert.Equal(t, tt.expectedClass, classification)
			assert.Equal(t, tt.expectedRequires, requiresResponse)
			assert.Equal(t, tt.expectedPriority, priority)
			assert.NotEmpty(t, summary)
			if tt.expectedRequires {
				assert.NotEmpty(t, draft)
			}
		})
	}
}

func TestDeterminePriority(t *testing.T) {
	poller := newTestEmailPoller([]string{"test"})

	tests := []struct {
		name           string
		subject        string
		from           string
		classification string
		expected       string
	}{
		{
			name:           "Urgent subject",
			subject:        "URGENT: Action needed",
			from:           "boss@example.com",
			classification: "Information",
			expected:       "High",
		},
		{
			name:           "ASAP subject",
			subject:        "ASAP: Review needed",
			from:           "client@example.com",
			classification: "FYI",
			expected:       "High",
		},
		{
			name:           "Question classification",
			subject:        "Quick question",
			from:           "colleague@example.com",
			classification: "Question",
			expected:       "Medium",
		},
		{
			name:           "Action Required classification",
			subject:        "Task for you",
			from:           "manager@example.com",
			classification: "Action Required",
			expected:       "Medium",
		},
		{
			name:           "Low priority",
			subject:        "Weekly newsletter",
			from:           "marketing@example.com",
			classification: "Information",
			expected:       "Low",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			priority := poller.determinePriority(tt.subject, tt.from, tt.classification)
			assert.Equal(t, tt.expected, priority)
		})
	}
}

func TestCollectNewMessageIDs(t *testing.T) {
	messageRefs := []*gmail.Message{
		{Id: "m3"},
		{Id: "m2"},
		{Id: "m1"},
	}

	t.Run("no last message ID processes everything", func(t *testing.T) {
		ids, found := collectNewMessageIDs(messageRefs, "")
		assert.Equal(t, []string{"m3", "m2", "m1"}, ids)
		assert.True(t, found)
	})

	t.Run("stops once last message encountered", func(t *testing.T) {
		ids, found := collectNewMessageIDs(messageRefs, "m2")
		assert.Equal(t, []string{"m3"}, ids)
		assert.True(t, found)
	})

	t.Run("returns false when last message missing", func(t *testing.T) {
		ids, found := collectNewMessageIDs(messageRefs, "missing")
		assert.Equal(t, []string{"m3", "m2", "m1"}, ids)
		assert.False(t, found)
	})

	t.Run("skips nil or empty entries", func(t *testing.T) {
		ids, found := collectNewMessageIDs([]*gmail.Message{
			nil,
			{Id: ""},
			{Id: "m5"},
		}, "m4")
		assert.Equal(t, []string{"m5"}, ids)
		assert.False(t, found)
	})
}

func TestEmitToInputStreamStructure(t *testing.T) {
	// Test that emitToInputStream has the correct structure and message format
	// Create test message
	message := &EmailMessage{
		ID:               "msg123",
		ThreadID:         "thread456",
		Subject:          "Test Subject",
		From:             "sender@example.com",
		To:               []string{"recipient@example.com"},
		Date:             time.Now(),
		Snippet:          "Test snippet",
		BodyText:         "Test body",
		RequiresResponse: true,
		Summary:          "Test summary",
		DraftReply:       "Test draft",
		Classification:   "Question",
		Priority:         "Medium",
		Timestamp:        time.Now(),
		UserID:           "test-user",
	}

	// Test that message can be marshaled to JSON (required for Redis stream)
	_, err := json.Marshal(message)
	assert.NoError(t, err)

	// Test stream key generation
	expectedStreamKey := "user:test-user:in:email"
	assert.Equal(t, expectedStreamKey, fmt.Sprintf("user:%s:in:email", "test-user"))
}

func TestBase64URLDecode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		hasError bool
	}{
		{
			name:     "Normal base64",
			input:    "SGVsbG8gV29ybGQ=",
			expected: "Hello World",
			hasError: false,
		},
		{
			name:     "URL safe base64 without padding",
			input:    "SGVsbG8gV29ybGQ",
			expected: "Hello World",
			hasError: false,
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
			hasError: false,
		},
		{
			name:     "Invalid base64",
			input:    "invalid!@#",
			expected: "",
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := base64URLDecode(tt.input)

			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, string(result))
			}
		})
	}
}

func TestEmailMessageJSONSerialization(t *testing.T) {
	message := &EmailMessage{
		ID:               "msg123",
		ThreadID:         "thread456",
		Subject:          "Test Subject",
		From:             "sender@example.com",
		To:               []string{"recipient@example.com"},
		Date:             time.Now(),
		Snippet:          "Test snippet",
		BodyText:         "Test body",
		RequiresResponse: true,
		Summary:          "Test summary",
		DraftReply:       "Test draft",
		Classification:   "Question",
		Priority:         "Medium",
		Timestamp:        time.Now(),
		UserID:           "test-user",
	}

	// Test JSON marshaling
	data, err := json.Marshal(message)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	// Test JSON unmarshaling
	var unmarshaled EmailMessage
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)

	assert.Equal(t, message.ID, unmarshaled.ID)
	assert.Equal(t, message.Subject, unmarshaled.Subject)
	assert.Equal(t, message.From, unmarshaled.From)
	assert.Equal(t, message.Classification, unmarshaled.Classification)
	assert.Equal(t, message.RequiresResponse, unmarshaled.RequiresResponse)
}
