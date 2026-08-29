package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/teams"
)

// Scheduler enqueues runs for scheduled automations. It runs inside the API
// process so scheduling never depends on a worker being up; the run simply
// waits in the queue.
type Scheduler struct {
	repo        automations.Repository
	runService  agentruns.Service
	teamService teams.Service
	interval    time.Duration
}

// New creates a scheduler.
func New(repo automations.Repository, runService agentruns.Service, teamService teams.Service) *Scheduler {
	return &Scheduler{repo: repo, runService: runService, teamService: teamService, interval: 30 * time.Second}
}

// Start begins the polling loop and performs startup catch-up.
func (s *Scheduler) Start(ctx context.Context) {
	s.catchUp()
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.tick()
			}
		}
	}()
}

// catchUp handles automations whose next_run_at passed while the API was
// down: catch_up=true gets exactly one run; otherwise just advance the clock.
// Both paths go through the atomic claim so a multi-replica deployment never
// double-fires (or double-advances) a due automation.
func (s *Scheduler) catchUp() {
	due, err := s.repo.ListDueScheduled(time.Now())
	if err != nil {
		log.Printf("scheduler: catch-up query failed: %v", err)
		return
	}
	for _, a := range due {
		if a.CatchUp {
			s.fire(a)
		} else {
			// Skip the missed run, but still claim the row so the clock
			// advances exactly once across replicas.
			s.claim(a)
		}
	}
}

func (s *Scheduler) tick() {
	due, err := s.repo.ListDueScheduled(time.Now())
	if err != nil {
		log.Printf("scheduler: due query failed: %v", err)
		return
	}
	for _, a := range due {
		s.fire(a)
	}
}

// fire atomically claims the automation for this replica and, only if the
// claim is won, enqueues exactly one run. Claiming advances next_run_at in the
// same statement, so a peer replica that lost the race for this row simply
// finds it no longer due and never fires it too.
func (s *Scheduler) fire(a *automations.Automation) {
	if !s.claim(a) {
		return
	}

	agentID, teamID, teamNodeID, err := ResolveTarget(a, s.teamService)
	if err != nil {
		// Already advanced by the claim; just skip this occurrence.
		log.Printf("scheduler: automation %s (%s) target unresolvable: %v", a.Name, a.ID, err)
		return
	}

	prompt := automations.RenderPrompt(a.PromptTemplate, map[string]string{
		"automation.name": a.Name,
	})
	if prompt == "" {
		prompt = "Scheduled run of automation: " + a.Name
	}

	automationID := a.ID
	req := agentruns.LaunchRequest{
		OrgID:        a.OrgID,
		AgentID:      agentID,
		ProjectID:    a.ProjectID,
		AutomationID: &automationID,
		TeamID:       teamID,
		TeamNodeID:   teamNodeID,
		Prompt:       prompt,
	}
	if _, _, err := s.runService.Launch(req); err != nil {
		log.Printf("scheduler: failed to launch run for automation %s: %v", a.ID, err)
	}
}

// claim atomically claims the automation for this replica, advancing its
// next_run_at to the next cron occurrence (an invalid cron disables it by
// advancing to NULL). It reports whether THIS replica won the claim; only the
// winner should fire. A lost claim means a peer replica already took the row.
func (s *Scheduler) claim(a *automations.Automation) bool {
	now := time.Now()
	next, err := automations.NextAfter(a.CronExpr, now)
	if err != nil {
		log.Printf("scheduler: automation %s has invalid cron %q: %v", a.ID, a.CronExpr, err)
		claimed, cerr := s.repo.ClaimDueScheduled(a.ID, now, nil)
		if cerr != nil {
			log.Printf("scheduler: failed to claim %s: %v", a.ID, cerr)
		}
		return claimed
	}
	claimed, err := s.repo.ClaimDueScheduled(a.ID, now, &next)
	if err != nil {
		log.Printf("scheduler: failed to claim %s: %v", a.ID, err)
		return false
	}
	return claimed
}

// ResolveTarget resolves an automation's agent target. Team automations
// resolve to the team's entry node's agent.
func ResolveTarget(a *automations.Automation, teamService teams.Service) (agentID string, teamID, teamNodeID *string, err error) {
	if a.AgentID != nil && *a.AgentID != "" {
		return *a.AgentID, nil, nil, nil
	}
	if a.TeamID == nil || *a.TeamID == "" {
		return "", nil, nil, ErrNoTarget
	}
	graph, err := teamService.GetTeam(*a.TeamID)
	if err != nil {
		return "", nil, nil, err
	}
	if graph.Team.EntryNodeID == nil {
		return "", nil, nil, ErrNoEntryNode
	}
	for _, node := range graph.Nodes {
		if node.ID == *graph.Team.EntryNodeID {
			return node.AgentID, a.TeamID, graph.Team.EntryNodeID, nil
		}
	}
	return "", nil, nil, ErrNoEntryNode
}

// Sentinel errors for target resolution.
var (
	ErrNoTarget    = errString("automation has neither agent nor team target")
	ErrNoEntryNode = errString("team has no entry node")
)

type errString string

func (e errString) Error() string { return string(e) }
