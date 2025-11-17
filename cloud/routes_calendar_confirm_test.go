package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"alfred-cloud/subagents/calendar_planner"
	"github.com/stretchr/testify/require"
	cal "google.golang.org/api/calendar/v3"
)

type stubProposalService struct {
	proposals map[string]*calendar_planner.ShadowProposal
}

func (s *stubProposalService) GetProposal(ctx context.Context, userID, proposalID string) (*calendar_planner.ShadowProposal, error) {
	if proposal, ok := s.proposals[proposalID]; ok && proposal.UserID == userID {
		copy := *proposal
		return &copy, nil
	}
	return nil, nil
}

func (s *stubProposalService) SaveProposal(ctx context.Context, proposal *calendar_planner.ShadowProposal) error {
	if s.proposals == nil {
		s.proposals = make(map[string]*calendar_planner.ShadowProposal)
	}
	copy := *proposal
	s.proposals[proposal.ID] = &copy
	return nil
}

type stubCalendarUpdater struct {
	calls      int
	calendarID string
	eventID    string
	lastEvent  cal.Event
	err        error
}

func (s *stubCalendarUpdater) UpdateEvent(ctx context.Context, calendarID, eventID string, event *cal.Event) (*cal.Event, error) {
	s.calls++
	s.calendarID = calendarID
	s.eventID = eventID
	if event != nil {
		s.lastEvent = *event
	}
	if s.err != nil {
		return nil, s.err
	}
	return event, nil
}

func TestConfirmProposalUpdatesCalendar(t *testing.T) {
	proposal := &calendar_planner.ShadowProposal{
		ID:     "proposal-1",
		UserID: "user-1",
		PrimaryEvent: &calendar_planner.ShadowEventSummary{
			EventID:    "event-123",
			CalendarID: "primary",
			Summary:    "Deep Work",
			StartISO:   time.Now().Format(time.RFC3339),
			EndISO:     time.Now().Add(time.Hour).Format(time.RFC3339),
		},
		Plan: &calendar_planner.CalendarPlan{
			Events: []calendar_planner.GoogleCalendarEvent{
				{
					Summary: "Deep Work (moved)",
					Start:   calendar_planner.GoogleCalendarTime{DateTime: "2025-01-01T10:00:00Z", TimeZone: "UTC"},
					End:     calendar_planner.GoogleCalendarTime{DateTime: "2025-01-01T11:00:00Z", TimeZone: "UTC"},
				},
			},
		},
		Status: "pending",
	}

	proposalStore := &stubProposalService{
		proposals: map[string]*calendar_planner.ShadowProposal{
			proposal.ID: proposal,
		},
	}

	updater := &stubCalendarUpdater{}
	handler := &proposalConfirmHandler{
		proposals: proposalStore,
		newUpdater: func(ctx context.Context, userID string) (calendarUpdater, error) {
			return updater, nil
		},
	}

	body, err := json.Marshal(confirmProposalRequest{
		UserID:     "user-1",
		ProposalID: "proposal-1",
		CalendarID: "primary",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/calendar/proposals/confirm", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.handleConfirm(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp confirmProposalResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.True(t, resp.OK)
	require.Equal(t, "proposal-1", resp.ProposalID)
	require.Equal(t, "event-123", resp.EventID)
	require.Equal(t, "primary", updater.calendarID)
	require.Equal(t, "applied", resp.Status)

	require.Equal(t, 1, updater.calls)
	require.Equal(t, "2025-01-01T10:00:00Z", updater.lastEvent.Start.DateTime)
	require.Equal(t, "2025-01-01T11:00:00Z", updater.lastEvent.End.DateTime)

	saved, err := proposalStore.GetProposal(context.Background(), "user-1", "proposal-1")
	require.NoError(t, err)
	require.Equal(t, "applied", saved.Status)
}
