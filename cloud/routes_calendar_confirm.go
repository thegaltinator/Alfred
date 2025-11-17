package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"alfred-cloud/security"
	"alfred-cloud/subagents/calendar_planner"
	"github.com/gorilla/mux"
	cal "google.golang.org/api/calendar/v3"
)

type proposalService interface {
	GetProposal(ctx context.Context, userID, proposalID string) (*calendar_planner.ShadowProposal, error)
	SaveProposal(ctx context.Context, proposal *calendar_planner.ShadowProposal) error
}

type calendarUpdater interface {
	UpdateEvent(ctx context.Context, calendarID, eventID string, event *cal.Event) (*cal.Event, error)
}

type calendarUpdaterFactory func(ctx context.Context, userID string) (calendarUpdater, error)

type proposalConfirmHandler struct {
	proposals  proposalService
	newUpdater calendarUpdaterFactory
}

type confirmProposalRequest struct {
	UserID     string `json:"user_id"`
	ProposalID string `json:"proposal_id"`
	CalendarID string `json:"calendar_id,omitempty"`
	ApplyTo    string `json:"apply_to,omitempty"`   // primary|conflicting
	PlanIndex  int    `json:"plan_index,omitempty"` // which plan.Events entry to apply
}

type confirmProposalResponse struct {
	OK           bool                                 `json:"ok"`
	ProposalID   string                               `json:"proposal_id"`
	EventID      string                               `json:"event_id"`
	CalendarID   string                               `json:"calendar_id"`
	Status       string                               `json:"status"`
	UpdatedAt    string                               `json:"updated_at"`
	AppliedEvent calendar_planner.GoogleCalendarEvent `json:"applied_event"`
}

func registerProposalConfirmRoutes(router *mux.Router, service *calendar_planner.ShadowCalendarService, calendarClient *security.GoogleServiceClient) {
	if router == nil || service == nil || calendarClient == nil {
		return
	}
	handler := &proposalConfirmHandler{
		proposals:  service,
		newUpdater: makeCalendarUpdaterFactory(calendarClient),
	}
	router.HandleFunc("/calendar/proposals/confirm", handler.handleConfirm).Methods("POST")
}

func makeCalendarUpdaterFactory(client *security.GoogleServiceClient) calendarUpdaterFactory {
	return func(ctx context.Context, userID string) (calendarUpdater, error) {
		if client == nil {
			return nil, errors.New("calendar OAuth not configured")
		}
		svc, err := client.GetCalendarService(ctx, userID)
		if err != nil {
			return nil, err
		}
		return &googleCalendarUpdater{events: svc.Events}, nil
	}
}

type googleCalendarUpdater struct {
	events *cal.EventsService
}

func (g *googleCalendarUpdater) UpdateEvent(ctx context.Context, calendarID, eventID string, event *cal.Event) (*cal.Event, error) {
	return g.events.Patch(calendarID, eventID, event).Context(ctx).Do()
}

func (h *proposalConfirmHandler) handleConfirm(w http.ResponseWriter, r *http.Request) {
	var req confirmProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	req.UserID = strings.TrimSpace(req.UserID)
	req.ProposalID = strings.TrimSpace(req.ProposalID)
	req.CalendarID = strings.TrimSpace(req.CalendarID)
	target := strings.ToLower(strings.TrimSpace(req.ApplyTo))

	if req.UserID == "" || req.ProposalID == "" {
		http.Error(w, "user_id and proposal_id are required", http.StatusBadRequest)
		return
	}

	if req.CalendarID == "" {
		req.CalendarID = "primary"
	}

	if target == "" {
		target = "primary"
	}
	if target != "primary" && target != "conflicting" {
		http.Error(w, "apply_to must be 'primary' or 'conflicting'", http.StatusBadRequest)
		return
	}

	if req.PlanIndex < 0 {
		http.Error(w, "plan_index cannot be negative", http.StatusBadRequest)
		return
	}

	proposal, err := h.proposals.GetProposal(r.Context(), req.UserID, req.ProposalID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to load proposal: %v", err), http.StatusInternalServerError)
		return
	}
	if proposal == nil {
		http.Error(w, "proposal not found", http.StatusNotFound)
		return
	}
	if proposal.Plan == nil || len(proposal.Plan.Events) == 0 {
		http.Error(w, "proposal missing plan events", http.StatusBadRequest)
		return
	}
	if req.PlanIndex >= len(proposal.Plan.Events) {
		http.Error(w, "plan_index out of range", http.StatusBadRequest)
		return
	}

	selected := proposal.Plan.Events[req.PlanIndex]
	if strings.TrimSpace(selected.Start.DateTime) == "" || strings.TrimSpace(selected.End.DateTime) == "" {
		http.Error(w, "proposal event missing start or end time", http.StatusBadRequest)
		return
	}

	var targetEventID string
	switch target {
	case "conflicting":
		if proposal.ConflictingEvent != nil {
			targetEventID = proposal.ConflictingEvent.EventID
		}
	default:
		if proposal.PrimaryEvent != nil {
			targetEventID = proposal.PrimaryEvent.EventID
		}
	}
	if strings.TrimSpace(targetEventID) == "" {
		http.Error(w, "target event missing", http.StatusBadRequest)
		return
	}

	updater, err := h.newUpdater(r.Context(), req.UserID)
	if err != nil {
		http.Error(w, fmt.Sprintf("calendar not ready: %v", err), http.StatusFailedDependency)
		return
	}

	payload := buildCalendarEventPayload(selected)
	if _, err := updater.UpdateEvent(r.Context(), req.CalendarID, targetEventID, payload); err != nil {
		http.Error(w, fmt.Sprintf("failed to update calendar: %v", err), http.StatusBadGateway)
		return
	}

	proposal.Status = "applied"
	proposal.UpdatedAt = time.Now().UTC()
	if err := h.proposals.SaveProposal(r.Context(), proposal); err != nil {
		log.Printf("calendar confirm: failed to persist proposal %s: %v", proposal.ID, err)
	}

	resp := confirmProposalResponse{
		OK:           true,
		ProposalID:   proposal.ID,
		EventID:      targetEventID,
		CalendarID:   req.CalendarID,
		Status:       proposal.Status,
		UpdatedAt:    proposal.UpdatedAt.Format(time.RFC3339Nano),
		AppliedEvent: selected,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func buildCalendarEventPayload(evt calendar_planner.GoogleCalendarEvent) *cal.Event {
	startTZ := strings.TrimSpace(evt.Start.TimeZone)
	endTZ := strings.TrimSpace(evt.End.TimeZone)
	if startTZ == "" {
		startTZ = "UTC"
	}
	if endTZ == "" {
		endTZ = startTZ
	}
	return &cal.Event{
		Summary:     evt.Summary,
		Description: evt.Description,
		Location:    evt.Location,
		Start: &cal.EventDateTime{
			DateTime: strings.TrimSpace(evt.Start.DateTime),
			TimeZone: startTZ,
		},
		End: &cal.EventDateTime{
			DateTime: strings.TrimSpace(evt.End.DateTime),
			TimeZone: endTZ,
		},
	}
}
