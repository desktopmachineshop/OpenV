package automations

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

// Automation kinds.
const (
	KindManual    = "manual"
	KindScheduled = "scheduled"
	KindTriggered = "triggered"
)

// Automation launches an agent or a team on a schedule, on an event, or manually.
type Automation struct {
	ID              string                 `json:"id"`
	OrgID           string                 `json:"org_id"`
	Name            string                 `json:"name"`
	AgentID         *string                `json:"agent_id,omitempty"`
	TeamID          *string                `json:"team_id,omitempty"`
	ProjectID       *string                `json:"project_id,omitempty"`
	Kind            string                 `json:"kind"`
	Enabled         bool                   `json:"enabled"`
	PromptTemplate  string                 `json:"prompt_template"`
	CronExpr        string                 `json:"cron_expr"`
	CatchUp         bool                   `json:"catch_up"`
	NextRunAt       *time.Time             `json:"next_run_at,omitempty"`
	LastRunAt       *time.Time             `json:"last_run_at,omitempty"`
	EventType       string                 `json:"event_type"`
	EventFilter     map[string]interface{} `json:"event_filter"`
	CooldownSeconds int                    `json:"cooldown_seconds"`
	MaxRunsPerHour  int                    `json:"max_runs_per_hour"`
	CreatedBy       *string                `json:"created_by,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

// CreateAutomationRequest is the payload for creating an automation.
// OrgID is stamped server-side from the caller's active workspace.
type CreateAutomationRequest struct {
	OrgID           string                 `json:"-"`
	Name            string                 `json:"name"`
	AgentID         *string                `json:"agent_id,omitempty"`
	TeamID          *string                `json:"team_id,omitempty"`
	ProjectID       *string                `json:"project_id,omitempty"`
	Kind            string                 `json:"kind"`
	Enabled         *bool                  `json:"enabled,omitempty"` // nil defaults to true
	PromptTemplate  string                 `json:"prompt_template"`
	CronExpr        string                 `json:"cron_expr"`
	CatchUp         bool                   `json:"catch_up"`
	EventType       string                 `json:"event_type"`
	EventFilter     map[string]interface{} `json:"event_filter,omitempty"`
	CooldownSeconds *int                   `json:"cooldown_seconds,omitempty"`
	MaxRunsPerHour  *int                   `json:"max_runs_per_hour,omitempty"`
	CreatedBy       *string                `json:"created_by,omitempty"`
}

// UpdateAutomationRequest carries the editable fields; nil fields are left unchanged.
// AgentID/TeamID set to the empty string clear the target.
type UpdateAutomationRequest struct {
	Name            *string                `json:"name,omitempty"`
	AgentID         *string                `json:"agent_id,omitempty"`
	TeamID          *string                `json:"team_id,omitempty"`
	Enabled         *bool                  `json:"enabled,omitempty"`
	PromptTemplate  *string                `json:"prompt_template,omitempty"`
	CronExpr        *string                `json:"cron_expr,omitempty"`
	CatchUp         *bool                  `json:"catch_up,omitempty"`
	EventType       *string                `json:"event_type,omitempty"`
	EventFilter     map[string]interface{} `json:"event_filter,omitempty"`
	CooldownSeconds *int                   `json:"cooldown_seconds,omitempty"`
	MaxRunsPerHour  *int                   `json:"max_runs_per_hour,omitempty"`
}

// Repository defines persistence operations for automations.
// Find methods return (nil, nil) when no row exists.
type Repository interface {
	Save(a *Automation) error
	Update(a *Automation) error
	FindByID(id string) (*Automation, error)
	// List returns an org's automations; projectID "" lists all in the org.
	List(orgID, projectID string) ([]*Automation, error)
	Delete(id string) error
	// ListDueScheduled returns due scheduled automations as firing candidates
	// only; callers must claim each with ClaimDueScheduled before firing.
	ListDueScheduled(now time.Time) ([]*Automation, error)
	// ClaimDueScheduled atomically claims one due scheduled automation and
	// advances its next_run_at (nil disables it) in a single statement,
	// reporting whether the caller won the claim. Concurrent callers across
	// replicas partition the due set with no overlap, so no automation
	// double-fires.
	ClaimDueScheduled(id string, lastRun time.Time, nextRun *time.Time) (bool, error)
	ListEnabledTriggered(eventType string) ([]*Automation, error)
	MarkRun(id string, lastRun time.Time, nextRun *time.Time) error
}

// Service defines automation domain logic.
type Service interface {
	Create(req CreateAutomationRequest) (*Automation, error)
	Get(id string) (*Automation, error)
	List(orgID, projectID string) ([]*Automation, error)
	Update(id string, req UpdateAutomationRequest) (*Automation, error)
	Delete(id string) error
}

// DefaultService implements the Service interface.
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates a new automation service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// NextAfter returns the next activation of a standard 5-field cron expression after t.
func NextAfter(expr string, t time.Time) (time.Time, error) {
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	return sched.Next(t), nil
}

var placeholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}`)

// RenderPrompt replaces {{key}} placeholders with values from vars.
// Unknown placeholders render as the empty string. Only flat string vars are
// supported by design: templates carry identifiers, not content dumps.
func RenderPrompt(template string, vars map[string]string) string {
	return placeholderRe.ReplaceAllStringFunc(template, func(m string) string {
		key := placeholderRe.FindStringSubmatch(m)[1]
		return vars[key]
	})
}

func validKind(kind string) bool {
	return kind == KindManual || kind == KindScheduled || kind == KindTriggered
}

// validateTarget ensures exactly one of AgentID/TeamID is set.
func validateTarget(a *Automation) error {
	hasAgent := a.AgentID != nil && *a.AgentID != ""
	hasTeam := a.TeamID != nil && *a.TeamID != ""
	if hasAgent == hasTeam {
		return errors.New("exactly one of agent_id or team_id must be set")
	}
	return nil
}

// Create validates and persists a new automation.
func (s *DefaultService) Create(req CreateAutomationRequest) (*Automation, error) {
	if req.OrgID == "" {
		return nil, errors.New("organization id is required")
	}
	if req.Name == "" {
		return nil, errors.New("name is required")
	}
	if !validKind(req.Kind) {
		return nil, fmt.Errorf("invalid kind %q", req.Kind)
	}

	now := time.Now()
	a := &Automation{
		ID:              uuid.New().String(),
		OrgID:           req.OrgID,
		Name:            req.Name,
		AgentID:         req.AgentID,
		TeamID:          req.TeamID,
		ProjectID:       req.ProjectID,
		Kind:            req.Kind,
		Enabled:         true,
		PromptTemplate:  req.PromptTemplate,
		CronExpr:        req.CronExpr,
		CatchUp:         req.CatchUp,
		EventType:       req.EventType,
		EventFilter:     req.EventFilter,
		CooldownSeconds: 60,
		MaxRunsPerHour:  10,
		CreatedBy:       req.CreatedBy,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if req.Enabled != nil {
		a.Enabled = *req.Enabled
	}
	if req.CooldownSeconds != nil {
		a.CooldownSeconds = *req.CooldownSeconds
	}
	if req.MaxRunsPerHour != nil {
		a.MaxRunsPerHour = *req.MaxRunsPerHour
	}
	if a.EventFilter == nil {
		a.EventFilter = map[string]interface{}{}
	}

	if err := validateTarget(a); err != nil {
		return nil, err
	}

	switch a.Kind {
	case KindScheduled:
		next, err := NextAfter(a.CronExpr, now)
		if err != nil {
			return nil, err
		}
		if a.Enabled {
			a.NextRunAt = &next
		}
	case KindTriggered:
		if a.EventType == "" {
			return nil, errors.New("event_type is required for triggered automations")
		}
	}

	if err := s.repo.Save(a); err != nil {
		return nil, err
	}
	return a, nil
}

// Get retrieves an automation by ID.
func (s *DefaultService) Get(id string) (*Automation, error) {
	a, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errors.New("automation not found")
	}
	return a, nil
}

// List returns an org's automations, optionally filtered by project.
func (s *DefaultService) List(orgID, projectID string) ([]*Automation, error) {
	return s.repo.List(orgID, projectID)
}

// Update applies the non-nil fields of req and recomputes next_run_at when the
// cron expression or enabled flag changes.
func (s *DefaultService) Update(id string, req UpdateAutomationRequest) (*Automation, error) {
	a, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errors.New("automation not found")
	}

	recompute := false
	if req.Name != nil {
		if *req.Name == "" {
			return nil, errors.New("name is required")
		}
		a.Name = *req.Name
	}
	if req.AgentID != nil {
		if *req.AgentID == "" {
			a.AgentID = nil
		} else {
			v := *req.AgentID
			a.AgentID = &v
		}
	}
	if req.TeamID != nil {
		if *req.TeamID == "" {
			a.TeamID = nil
		} else {
			v := *req.TeamID
			a.TeamID = &v
		}
	}
	if req.Enabled != nil && *req.Enabled != a.Enabled {
		a.Enabled = *req.Enabled
		recompute = true
	}
	if req.PromptTemplate != nil {
		a.PromptTemplate = *req.PromptTemplate
	}
	if req.CronExpr != nil && *req.CronExpr != a.CronExpr {
		a.CronExpr = *req.CronExpr
		recompute = true
	}
	if req.CatchUp != nil {
		a.CatchUp = *req.CatchUp
	}
	if req.EventType != nil {
		a.EventType = *req.EventType
	}
	if req.EventFilter != nil {
		a.EventFilter = req.EventFilter
	}
	if req.CooldownSeconds != nil {
		a.CooldownSeconds = *req.CooldownSeconds
	}
	if req.MaxRunsPerHour != nil {
		a.MaxRunsPerHour = *req.MaxRunsPerHour
	}

	if err := validateTarget(a); err != nil {
		return nil, err
	}

	switch a.Kind {
	case KindScheduled:
		if recompute {
			if a.Enabled {
				next, err := NextAfter(a.CronExpr, time.Now())
				if err != nil {
					return nil, err
				}
				a.NextRunAt = &next
			} else {
				a.NextRunAt = nil
			}
		}
	case KindTriggered:
		if a.EventType == "" {
			return nil, errors.New("event_type is required for triggered automations")
		}
	}

	a.UpdatedAt = time.Now()
	if err := s.repo.Update(a); err != nil {
		return nil, err
	}
	return a, nil
}

// Delete removes an automation.
func (s *DefaultService) Delete(id string) error {
	return s.repo.Delete(id)
}
