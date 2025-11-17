package calendar_planner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultPlannerScript = "../python_helper/planner_tool.py"
	defaultPythonBinary  = "python3"
)

// CalendarPlan is the structured response for the planner tool.
type CalendarPlan struct {
	Notes  []string              `json:"notes"`
	Blocks []PlanBlock           `json:"plan_blocks"`
	Events []GoogleCalendarEvent `json:"events"`
}

// PlanBlock represents a single calendar block.
type PlanBlock struct {
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	StartTime   string   `json:"start_time"`
	EndTime     string   `json:"end_time"`
	Location    string   `json:"location,omitempty"`
	Priority    string   `json:"priority,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	AllDay      bool     `json:"all_day,omitempty"`
}

// GoogleCalendarEvent represents an event structure compatible with Google Calendar APIs.
type GoogleCalendarEvent struct {
	Summary     string             `json:"summary"`
	Description string             `json:"description,omitempty"`
	Location    string             `json:"location,omitempty"`
	Start       GoogleCalendarTime `json:"start"`
	End         GoogleCalendarTime `json:"end"`
	Tags        []string           `json:"tags,omitempty"`
	Priority    string             `json:"priority,omitempty"`
}

// GoogleCalendarTime represents a timestamp payload for Google Calendar.
type GoogleCalendarTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

// CalendarManagerService shells out to the Python planner script.
type CalendarManagerService struct {
	scriptPath string
	pythonBin  string
}

// NewCalendarManagerService creates a new planner runner that delegates to Python.
func NewCalendarManagerService(scriptPath string) *CalendarManagerService {
	if scriptPath == "" {
		scriptPath = defaultPlannerScript
	}
	if !filepath.IsAbs(scriptPath) {
		if abs, err := filepath.Abs(scriptPath); err == nil {
			scriptPath = abs
		}
	}

	pythonBin := os.Getenv("PYTHON_BIN")
	if pythonBin == "" {
		pythonBin = defaultPythonBinary
	}

	return &CalendarManagerService{
		scriptPath: scriptPath,
		pythonBin:  pythonBin,
	}
}

// GenerateCalendarPlan calls the Python helper and composes the ICS output.
func (ps *CalendarManagerService) GenerateCalendarPlan(
	ctx context.Context,
	planDate,
	timeBlock,
	activityType string,
) (*CalendarPlan, error) {
	timeBlock = strings.TrimSpace(timeBlock)
	if timeBlock == "" {
		return nil, fmt.Errorf("timeBlock is required")
	}

	payload, err := json.Marshal(map[string]string{
		"plan_date":     planDate,
		"time_block":    timeBlock,
		"activity_type": activityType,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode planner request: %w", err)
	}

	cmd := exec.CommandContext(ctx, ps.pythonBin, ps.scriptPath)
	cmd.Stdin = bytes.NewReader(payload)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("planner script failed: %s", detail)
	}

	var response struct {
		Notes      []string    `json:"notes"`
		PlanBlocks []PlanBlock `json:"plan_blocks"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return nil, fmt.Errorf("planner response decode failed: %w", err)
	}

	if len(response.PlanBlocks) == 0 {
		return nil, fmt.Errorf("planner response missing plan_blocks")
	}

	planDate = strings.TrimSpace(planDate)
	if planDate == "" {
		planDate = time.Now().Format("2006-01-02")
	}

	blocks := sanitizeBlocks(response.PlanBlocks)
	if len(blocks) == 0 {
		return nil, fmt.Errorf("planner blocks invalid after sanitization")
	}

	plan := &CalendarPlan{
		Notes:  dedupeStrings(response.Notes),
		Blocks: blocks,
	}
	events, err := buildGoogleEvents(planDate, blocks)
	if err != nil {
		return nil, err
	}
	plan.Events = events

	return plan, nil
}

func buildGoogleEvents(planDate string, blocks []PlanBlock) ([]GoogleCalendarEvent, error) {
	events := make([]GoogleCalendarEvent, 0, len(blocks))
	for _, block := range blocks {
		start, err := parseBlockTime(planDate, block.StartTime)
		if err != nil {
			return nil, fmt.Errorf("invalid start time for block %q: %w", block.Title, err)
		}
		end, err := parseBlockTime(planDate, block.EndTime)
		if err != nil {
			return nil, fmt.Errorf("invalid end time for block %q: %w", block.Title, err)
		}
		if !end.After(start) {
			end = start.Add(30 * time.Minute)
		}

		events = append(events, GoogleCalendarEvent{
			Summary:     block.Title,
			Description: block.Description,
			Location:    block.Location,
			Tags:        block.Tags,
			Priority:    block.Priority,
			Start: GoogleCalendarTime{
				DateTime: start.Format(time.RFC3339),
				TimeZone: formatTimeZone(start),
			},
			End: GoogleCalendarTime{
				DateTime: end.Format(time.RFC3339),
				TimeZone: formatTimeZone(end),
			},
		})
	}
	return events, nil
}

func sanitizeBlocks(blocks []PlanBlock) []PlanBlock {
	result := make([]PlanBlock, 0, len(blocks))
	for _, block := range blocks {
		block.Title = strings.TrimSpace(block.Title)
		block.Description = strings.TrimSpace(block.Description)
		block.StartTime = strings.TrimSpace(block.StartTime)
		block.EndTime = strings.TrimSpace(block.EndTime)
		block.Location = strings.TrimSpace(block.Location)
		block.Priority = strings.TrimSpace(block.Priority)
		block.Tags = dedupeStrings(block.Tags)
		if block.StartTime == "" || block.EndTime == "" {
			continue
		}
		result = append(result, block)
	}
	return result
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func parseBlockTime(planDate, raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02 15:04",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, nil
		}
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t, nil
		}
	}

	if planDate == "" {
		planDate = time.Now().Format("2006-01-02")
	}
	day, err := time.ParseInLocation("2006-01-02", planDate, time.Local)
	if err != nil {
		day = time.Now()
	}

	timeFormats := []string{"15:04", "3:04PM", "3PM"}
	for _, f := range timeFormats {
		if parsed, err := time.Parse(f, raw); err == nil {
			return time.Date(day.Year(), day.Month(), day.Day(), parsed.Hour(), parsed.Minute(), 0, 0, day.Location()), nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse time %q", raw)
}

func formatTimeZone(t time.Time) string {
	zone, offset := t.Zone()
	if zone != "" && zone != "UTC" {
		return zone
	}
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	hours := offset / 3600
	minutes := (offset % 3600) / 60
	return fmt.Sprintf("UTC%s%02d:%02d", sign, hours, minutes)
}
