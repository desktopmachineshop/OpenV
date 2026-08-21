package postgres

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/openv/requirements-platform/internal/domain/automations"
)

// AutomationRepository implements the automations.Repository interface.
type AutomationRepository struct {
	db *sql.DB
}

// NewAutomationRepository creates a new automation repository.
func NewAutomationRepository(db *sql.DB) *AutomationRepository {
	return &AutomationRepository{db: db}
}

const automationColumns = `
	id, COALESCE(org_id::text, ''), name, COALESCE(agent_id::text, ''), COALESCE(team_id::text, ''), COALESCE(project_id::text, ''),
	kind, enabled, prompt_template, cron_expr, catch_up, next_run_at, last_run_at,
	event_type, event_filter, cooldown_seconds, max_runs_per_hour,
	COALESCE(created_by::text, ''), created_at, updated_at`

type automationScanner interface {
	Scan(dest ...interface{}) error
}

func scanAutomation(s automationScanner) (*automations.Automation, error) {
	a := new(automations.Automation)
	var agentID, teamID, projectID, createdBy string
	var nextRun, lastRun sql.NullTime
	var eventFilter []byte

	err := s.Scan(
		&a.ID,
		&a.OrgID,
		&a.Name,
		&agentID,
		&teamID,
		&projectID,
		&a.Kind,
		&a.Enabled,
		&a.PromptTemplate,
		&a.CronExpr,
		&a.CatchUp,
		&nextRun,
		&lastRun,
		&a.EventType,
		&eventFilter,
		&a.CooldownSeconds,
		&a.MaxRunsPerHour,
		&createdBy,
		&a.CreatedAt,
		&a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if agentID != "" {
		a.AgentID = &agentID
	}
	if teamID != "" {
		a.TeamID = &teamID
	}
	if projectID != "" {
		a.ProjectID = &projectID
	}
	if createdBy != "" {
		a.CreatedBy = &createdBy
	}
	if nextRun.Valid {
		t := nextRun.Time
		a.NextRunAt = &t
	}
	if lastRun.Valid {
		t := lastRun.Time
		a.LastRunAt = &t
	}
	a.EventFilter = map[string]interface{}{}
	if len(eventFilter) > 0 {
		if err := json.Unmarshal(eventFilter, &a.EventFilter); err != nil {
			a.EventFilter = map[string]interface{}{}
		}
	}
	return a, nil
}

func marshalEventFilter(m map[string]interface{}) ([]byte, error) {
	if m == nil {
		m = map[string]interface{}{}
	}
	return json.Marshal(m)
}

// Save inserts a new automation.
func (r *AutomationRepository) Save(a *automations.Automation) error {
	eventFilter, err := marshalEventFilter(a.EventFilter)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO automations (
			id, org_id, name, agent_id, team_id, project_id, kind, enabled,
			prompt_template, cron_expr, catch_up, next_run_at, last_run_at,
			event_type, event_filter, cooldown_seconds, max_runs_per_hour,
			created_by, created_at, updated_at
		)
		VALUES ($1, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
	`,
		a.ID, a.OrgID, a.Name, a.AgentID, a.TeamID, a.ProjectID, a.Kind, a.Enabled,
		a.PromptTemplate, a.CronExpr, a.CatchUp, a.NextRunAt, a.LastRunAt,
		a.EventType, eventFilter, a.CooldownSeconds, a.MaxRunsPerHour,
		a.CreatedBy, a.CreatedAt, a.UpdatedAt,
	)
	return err
}

// Update rewrites an automation's mutable columns.
func (r *AutomationRepository) Update(a *automations.Automation) error {
	eventFilter, err := marshalEventFilter(a.EventFilter)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		UPDATE automations SET
			name = $2, agent_id = $3, team_id = $4, project_id = $5, kind = $6, enabled = $7,
			prompt_template = $8, cron_expr = $9, catch_up = $10, next_run_at = $11, last_run_at = $12,
			event_type = $13, event_filter = $14, cooldown_seconds = $15, max_runs_per_hour = $16,
			updated_at = $17
		WHERE id = $1
	`,
		a.ID, a.Name, a.AgentID, a.TeamID, a.ProjectID, a.Kind, a.Enabled,
		a.PromptTemplate, a.CronExpr, a.CatchUp, a.NextRunAt, a.LastRunAt,
		a.EventType, eventFilter, a.CooldownSeconds, a.MaxRunsPerHour,
		a.UpdatedAt,
	)
	return err
}

// FindByID retrieves an automation, returning (nil, nil) when absent.
func (r *AutomationRepository) FindByID(id string) (*automations.Automation, error) {
	row := r.db.QueryRow(`SELECT `+automationColumns+` FROM automations WHERE id = $1`, id)
	a, err := scanAutomation(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// List returns an org's automations, optionally filtered by project
// (projectID "" lists all in the org).
func (r *AutomationRepository) List(orgID, projectID string) ([]*automations.Automation, error) {
	rows, err := r.db.Query(`
		SELECT `+automationColumns+`
		FROM automations
		WHERE org_id = NULLIF($1, '')::uuid
		  AND ($2 = '' OR project_id = NULLIF($2, '')::uuid)
		ORDER BY created_at DESC
	`, orgID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*automations.Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// Delete removes an automation.
func (r *AutomationRepository) Delete(id string) error {
	_, err := r.db.Exec(`DELETE FROM automations WHERE id = $1`, id)
	return err
}

// ListDueScheduled returns enabled scheduled automations whose next_run_at is due,
// locking the rows so concurrent schedulers skip each other's claims.
func (r *AutomationRepository) ListDueScheduled(now time.Time) ([]*automations.Automation, error) {
	rows, err := r.db.Query(`
		SELECT `+automationColumns+`
		FROM automations
		WHERE enabled AND kind = 'scheduled' AND next_run_at IS NOT NULL AND next_run_at <= $1
		ORDER BY next_run_at
		FOR UPDATE SKIP LOCKED
	`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*automations.Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// ListEnabledTriggered returns enabled triggered automations for an event type.
func (r *AutomationRepository) ListEnabledTriggered(eventType string) ([]*automations.Automation, error) {
	rows, err := r.db.Query(`
		SELECT `+automationColumns+`
		FROM automations
		WHERE enabled AND kind = 'triggered' AND event_type = $1
		ORDER BY created_at
	`, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*automations.Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// MarkRun records a run and advances the schedule.
func (r *AutomationRepository) MarkRun(id string, lastRun time.Time, nextRun *time.Time) error {
	_, err := r.db.Exec(`
		UPDATE automations SET last_run_at = $2, next_run_at = $3, updated_at = NOW()
		WHERE id = $1
	`, id, lastRun, nextRun)
	return err
}
