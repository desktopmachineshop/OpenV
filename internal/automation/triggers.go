package automation

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/automations"
	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/scheduler"
)

// TriggerMatcher subscribes to the event bus and launches runs for matching
// triggered automations.
type TriggerMatcher struct {
	repo        automations.Repository
	runService  agentruns.Service
	teamService teams.Service
}

// NewTriggerMatcher creates a matcher.
func NewTriggerMatcher(repo automations.Repository, runService agentruns.Service, teamService teams.Service) *TriggerMatcher {
	return &TriggerMatcher{repo: repo, runService: runService, teamService: teamService}
}

// Start subscribes to the bus.
func (m *TriggerMatcher) Start(bus domainevents.Bus) {
	bus.Subscribe(m.handle)
}

func (m *TriggerMatcher) handle(e domainevents.Event) {
	list, err := m.repo.ListEnabledTriggered(e.EventType)
	if err != nil {
		slog.Error("triggers: automation query failed",
			slog.String("event_type", e.EventType),
			slog.Any("error", err))
		return
	}
	for _, a := range list {
		if m.matches(a, e) && m.passesGuards(a, e) {
			m.fire(a, e)
		}
	}
}

func (m *TriggerMatcher) matches(a *automations.Automation, e domainevents.Event) bool {
	// Project scope.
	if a.ProjectID != nil && *a.ProjectID != "" && *a.ProjectID != e.ProjectID {
		return false
	}
	// Event filter: flat equality against payload values (string compare).
	for key, want := range a.EventFilter {
		got, ok := e.Payload[key]
		if !ok {
			return false
		}
		if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
			return false
		}
	}
	return true
}

func (m *TriggerMatcher) passesGuards(a *automations.Automation, e domainevents.Event) bool {
	// Self-trigger loop guard: skip events produced by this automation's own runs.
	if strings.HasPrefix(e.Actor, "agent:") {
		runID := strings.TrimPrefix(e.Actor, "agent:")
		if run, err := m.runService.Get(runID); err == nil && run != nil {
			if run.AutomationID != nil && *run.AutomationID == a.ID {
				return false
			}
		}
	}

	// Cooldown.
	if a.LastRunAt != nil && a.CooldownSeconds > 0 {
		if time.Since(*a.LastRunAt) < time.Duration(a.CooldownSeconds)*time.Second {
			return false
		}
	}

	// Hourly rate cap.
	if a.MaxRunsPerHour > 0 {
		count, err := m.runService.CountRunsSince(a.ID, time.Now().Add(-time.Hour))
		if err == nil && count >= a.MaxRunsPerHour {
			slog.Info("triggers: automation hit max_runs_per_hour",
				slog.String("automation_id", a.ID),
				slog.Int("max_runs_per_hour", a.MaxRunsPerHour))
			return false
		}
	}
	return true
}

func (m *TriggerMatcher) fire(a *automations.Automation, e domainevents.Event) {
	agentID, teamID, teamNodeID, err := scheduler.ResolveTarget(a, m.teamService)
	if err != nil {
		slog.Warn("triggers: automation target unresolvable",
			slog.String("automation_id", a.ID),
			slog.Any("error", err))
		return
	}

	// Lean-context rendering: identifiers and event metadata only.
	vars := map[string]string{
		"automation.name": a.Name,
		"event.type":      e.EventType,
		"event.entity_id": e.EntityID,
		"event.actor":     e.Actor,
		"project.id":      e.ProjectID,
	}
	for key, value := range e.Payload {
		switch v := value.(type) {
		case string:
			vars["event."+key] = v
		case fmt.Stringer:
			vars["event."+key] = v.String()
		case float64, int, int64, bool:
			vars["event."+key] = fmt.Sprintf("%v", v)
		}
	}

	prompt := automations.RenderPrompt(a.PromptTemplate, vars)
	if strings.TrimSpace(prompt) == "" {
		prompt = fmt.Sprintf("Automation %q fired on event %s (entity %s). Investigate via your OpenV tools and act per your instructions.", a.Name, e.EventType, e.EntityID)
	}

	automationID := a.ID
	eventID := e.ID
	projectID := a.ProjectID
	if projectID == nil && e.ProjectID != "" {
		p := e.ProjectID
		projectID = &p
	}
	req := agentruns.LaunchRequest{
		OrgID:          a.OrgID,
		AgentID:        agentID,
		ProjectID:      projectID,
		AutomationID:   &automationID,
		TriggerEventID: &eventID,
		TeamID:         teamID,
		TeamNodeID:     teamNodeID,
		Prompt:         prompt,
	}
	if _, _, err := m.runService.Launch(req); err != nil {
		slog.Error("triggers: failed to launch run for automation",
			slog.String("automation_id", a.ID),
			slog.Any("error", err))
		return
	}
	if err := m.repo.MarkRun(a.ID, time.Now(), a.NextRunAt); err != nil {
		slog.Warn("triggers: failed to stamp last_run_at",
			slog.String("automation_id", a.ID),
			slog.Any("error", err))
	}
}
