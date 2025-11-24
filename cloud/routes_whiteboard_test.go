package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"alfred-cloud/wb"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestWhiteboardSSEStreamsEvents(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bus := wb.NewBus(client)

	r := mux.NewRouter()
	registerWhiteboardRoutes(r, bus)

	server := httptest.NewServer(r)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/wb/stream?user_id=test-user", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	eventsCh := make(chan wb.Event, 1)
	go func() {
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				close(eventsCh)
				return
			}

			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var evt wb.Event
			if err := json.Unmarshal([]byte(payload), &evt); err == nil {
				eventsCh <- evt
				return
			}
		}
	}()

	appendBody, err := json.Marshal(map[string]any{
		"user_id":   "test-user",
		"thread_id": "thread-1",
		"values": map[string]any{
			"kind": "test",
			"body": "hello",
		},
	})
	require.NoError(t, err)

	appendResp, err := http.Post(server.URL+"/admin/wb/append", "application/json", bytes.NewReader(appendBody))
	require.NoError(t, err)
	defer appendResp.Body.Close()
	require.Equal(t, http.StatusOK, appendResp.StatusCode)

	select {
	case evt := <-eventsCh:
		require.Equal(t, wb.StreamKey("test-user"), evt.Stream)
		require.Equal(t, "test", evt.Values["kind"])
		require.Equal(t, "hello", evt.Values["body"])
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for whiteboard event")
	}
}

func TestWhiteboardSSEFiltersThread(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bus := wb.NewBus(client)

	r := mux.NewRouter()
	registerWhiteboardRoutes(r, bus)

	server := httptest.NewServer(r)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/wb/stream?user_id=test-user&thread_id=thread-a", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	eventsCh := make(chan wb.Event, 1)
	go func() {
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				close(eventsCh)
				return
			}
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var evt wb.Event
			if err := json.Unmarshal([]byte(payload), &evt); err == nil {
				eventsCh <- evt
				return
			}
		}
	}()

	// Append matching thread
	bodyThreadA, _ := json.Marshal(map[string]any{
		"user_id":   "test-user",
		"thread_id": "thread-a",
		"values": map[string]any{
			"type":    "talker.user_message",
			"content": "from thread a",
		},
	})
	// Append different thread
	bodyThreadB, _ := json.Marshal(map[string]any{
		"user_id":   "test-user",
		"thread_id": "thread-b",
		"values": map[string]any{
			"type":    "talker.user_message",
			"content": "from thread b",
		},
	})

	_, err = http.Post(server.URL+"/admin/wb/append", "application/json", bytes.NewReader(bodyThreadA))
	require.NoError(t, err)
	_, err = http.Post(server.URL+"/admin/wb/append", "application/json", bytes.NewReader(bodyThreadB))
	require.NoError(t, err)

	select {
	case evt := <-eventsCh:
		require.Equal(t, "thread-a", evt.ThreadID)
		require.Equal(t, "from thread a", evt.Values["content"])
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for filtered whiteboard event")
	}
}

func TestThreadIsolation(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bus := wb.NewBus(client)

	r := mux.NewRouter()
	registerWhiteboardRoutes(r, bus)

	server := httptest.NewServer(r)
	defer server.Close()

	// Generate two different thread IDs
	threadID1 := uuid.New()
	threadID2 := uuid.New()
	require.NotEqual(t, threadID1.String(), threadID2.String())

	// Test case 1: Append events with different thread IDs
	thread1Body, _ := json.Marshal(map[string]any{
		"user_id":   "test-user",
		"thread_id": threadID1.String(),
		"values": map[string]any{
			"type":    "talker.user_message",
			"content": "Hello from thread 1",
		},
	})

	thread2Body, _ := json.Marshal(map[string]any{
		"user_id":   "test-user",
		"thread_id": threadID2.String(),
		"values": map[string]any{
			"type":    "talker.user_message",
			"content": "Hello from thread 2",
		},
	})

	// Append both events
	resp1, err := http.Post(server.URL+"/admin/wb/append", "application/json", bytes.NewReader(thread1Body))
	require.NoError(t, err)
	defer resp1.Body.Close()
	require.Equal(t, http.StatusOK, resp1.StatusCode)

	resp2, err := http.Post(server.URL+"/admin/wb/append", "application/json", bytes.NewReader(thread2Body))
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	// Read the stream and verify thread IDs are preserved
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/wb/stream?user_id=test-user", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Read events from stream
	events := make([]wb.Event, 0)
	reader := bufio.NewReader(resp.Body)

	// Read for a limited time to collect events
	timeout := time.After(2 * time.Second)
	eventCount := 0

	for eventCount < 2 {
		select {
		case <-timeout:
			break
		default:
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}

			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var evt wb.Event
			if err := json.Unmarshal([]byte(payload), &evt); err == nil {
				events = append(events, evt)
				eventCount++
			}
		}
	}

	// Verify we got both events with correct thread IDs
	require.Len(t, events, 2, "Should receive exactly 2 events")

	var thread1Event, thread2Event *wb.Event
	for i := range events {
		if events[i].ThreadID == threadID1.String() {
			thread1Event = &events[i]
		} else if events[i].ThreadID == threadID2.String() {
			thread2Event = &events[i]
		}
	}

	require.NotNil(t, thread1Event, "Should find event for thread 1")
	require.NotNil(t, thread2Event, "Should find event for thread 2")
	require.Equal(t, "Hello from thread 1", thread1Event.Values["content"])
	require.Equal(t, "Hello from thread 2", thread2Event.Values["content"])
}

func TestThreadIsolationParallel(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	bus := wb.NewBus(client)

	r := mux.NewRouter()
	registerWhiteboardRoutes(r, bus)

	server := httptest.NewServer(r)
	defer server.Close()

	// Test parallel conversations don't interfere
	threadCount := 5
	threads := make([]uuid.UUID, threadCount)
	for i := range threads {
		threads[i] = uuid.New()
	}

	// Append events to different threads concurrently
	done := make(chan bool, threadCount)
	for i, threadID := range threads {
		go func(idx int, tid uuid.UUID) {
			defer func() { done <- true }()

			body, _ := json.Marshal(map[string]any{
				"user_id":   "test-user",
				"thread_id": tid.String(),
				"values": map[string]any{
					"type":    "talker.user_message",
					"content": fmt.Sprintf("Message from thread %d", idx),
				},
			})

			resp, err := http.Post(server.URL+"/admin/wb/append", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Errorf("Failed to append event for thread %d: %v", idx, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d for thread %d", resp.StatusCode, idx)
			}
		}(i, threadID)
	}

	// Wait for all goroutines to complete
	for i := 0; i < threadCount; i++ {
		select {
		case <-done:
			// Got completion signal
		case <-time.After(5 * time.Second):
			t.Fatal("Timed out waiting for concurrent appends to complete")
		}
	}

	// Verify all events are in the stream with unique thread IDs
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/wb/stream?user_id=test-user", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Collect events
	events := make([]wb.Event, 0)
	reader := bufio.NewReader(resp.Body)
	timeout := time.After(3 * time.Second)

	for len(events) < threadCount {
		select {
		case <-timeout:
			break
		default:
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}

			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var evt wb.Event
			if err := json.Unmarshal([]byte(payload), &evt); err == nil && evt.ThreadID != "" {
				events = append(events, evt)
			}
		}
	}

	// Verify thread isolation
	require.Len(t, events, threadCount, "Should receive exactly %d events", threadCount)

	threadIDMap := make(map[string]bool)
	for _, evt := range events {
		require.NotEmpty(t, evt.ThreadID, "Event should have a thread ID")
		require.False(t, threadIDMap[evt.ThreadID], "Thread ID should be unique: %s", evt.ThreadID)
		threadIDMap[evt.ThreadID] = true
	}
}
