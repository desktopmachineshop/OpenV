package agentruns

import (
	"errors"
	"testing"
	"time"
)

// The routing policy's own tests, against its two ports. cmd/server's
// TestRoutingPolicyRule (refactor step X7a) is its characterization, through
// the real runner keys and workspaces over a database.

// runnersOnline answers the online check and records the instant each call
// asks about.
type runnersOnline struct {
	online bool
	err    error
	since  []time.Time
}

func (r *runnersOnline) HasOnlinePersonalRunner(orgID, userID string, since time.Time) (bool, error) {
	if orgID != "org-1" || userID != "user-1" {
		return false, errors.New("asked about another workspace or account")
	}
	r.since = append(r.since, since)
	return r.online, r.err
}

// graceOf answers a fixed runner grace.
type graceOf int

func (g graceOf) RunnerGraceSeconds(string) int { return int(g) }

func TestRoutingPolicyPersonalRunner(t *testing.T) {
	for _, c := range []struct {
		name   string
		online bool
		err    error
		want   bool
	}{
		{"online", true, nil, true},
		{"offline", false, nil, false},
		{"the check fails, though it says online", true, errors.New("timeout"), false},
		{"the check fails", false, errors.New("timeout"), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			runners := &runnersOnline{online: c.online, err: c.err}
			hasPersonalRunner, _ := RoutingPolicy(runners, graceOf(0))

			before := time.Now()
			got := hasPersonalRunner("org-1", "user-1")
			after := time.Now()

			if got != c.want {
				t.Errorf("hasPersonalRunner = %v, want %v", got, c.want)
			}
			if len(runners.since) != 1 {
				t.Fatalf("asked %d times, want once", len(runners.since))
			}
			if since := runners.since[0]; since.Before(before.Add(-30*time.Second)) || since.After(after.Add(-30*time.Second)) {
				t.Errorf("asked since %v, want 30 s before the call (%v)", since, before.Add(-30*time.Second))
			}
		})
	}
}

// Launch reserves a run for an online personal runner for the workspace's
// grace, or DefaultGraceSeconds when it gives 0 or less.
func TestRoutingPolicyGrace(t *testing.T) {
	for _, c := range []struct {
		grace graceOf
		want  time.Duration
	}{
		{90, 90 * time.Second},
		{1, time.Second},
		{0, DefaultGraceSeconds * time.Second},
		{-30, DefaultGraceSeconds * time.Second},
	} {
		svc, _ := newRetryService()
		svc.SetRoutingPolicy(RoutingPolicy(&runnersOnline{online: true}, c.grace))
		run, _, err := svc.Launch(LaunchRequest{OrgID: "org-1", AgentID: "agent-1", Prompt: "go", LaunchedBy: strptr("user-1")})
		if err != nil {
			t.Fatal(err)
		}
		if run.PreferredUserID == nil || *run.PreferredUserID != "user-1" || run.HostedAfter == nil {
			t.Fatalf("grace %d: run not reserved for user-1 (preferred %v, hosted_after %v)", c.grace, run.PreferredUserID, run.HostedAfter)
		}
		if got := run.HostedAfter.Sub(run.CreatedAt).Round(time.Second); got != c.want {
			t.Errorf("grace %d: hosted_after is %v after created_at, want %v", c.grace, got, c.want)
		}
	}
}
