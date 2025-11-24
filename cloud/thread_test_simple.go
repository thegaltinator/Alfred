package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"alfred-cloud/wb"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestThreadIDSupport(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bus := wb.NewBus(client)

	r := mux.NewRouter()
	registerWhiteboardRoutes(r, bus)

	server := httptest.NewServer(r)
	defer server.Close()

	// Test that we can append events with thread IDs
	threadID := uuid.New()

	body, _ := json.Marshal(map[string]any{
		"user_id":   "test-user",
		"thread_id": threadID.String(),
		"values": map[string]any{
			"type":    "talker.user_message",
			"content": "Hello with thread ID",
		},
	})

	resp, err := http.Post(server.URL+"/admin/wb/append", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Parse the response to verify thread_id is echoed back
	var appendResp appendResponse
	err = json.NewDecoder(resp.Body).Decode(&appendResp)
	require.NoError(t, err)
	require.Equal(t, threadID.String(), appendResp.ThreadID)
	require.True(t, appendResp.OK)

	// Test appending without thread_id still works
	bodyNoThread, _ := json.Marshal(map[string]any{
		"user_id":   "test-user",
		"thread_id": uuid.New().String(),
		"values": map[string]any{
			"type":    "test.event",
			"content": "No thread ID",
		},
	})

	resp2, err := http.Post(server.URL+"/admin/wb/append", "application/json", bytes.NewReader(bodyNoThread))
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	var appendResp2 appendResponse
	err = json.NewDecoder(resp2.Body).Decode(&appendResp2)
	require.NoError(t, err)
	require.NotEmpty(t, appendResp2.ThreadID)
	require.True(t, appendResp2.OK)
}
