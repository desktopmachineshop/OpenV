package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/hostedworkers"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/hosting"
)

// Refactor step X7a (docs/plans/codebase-refactor.md §6.7, row X7): the
// characterization of the boot rules that X7b and X7c move out of
// cmd/server to their owners. This file holds the unit tables, with fakes
// for every store; boot_rules_pg_test.go holds the rules that are SQL,
// against a real database.
//
// Each test reaches its rule through one variable at its top, the rule as
// cmd/server defines it today. X7b and X7c re-point that variable at the
// new owner (hostedworkers.Reconcile, agentruns.BudgetGuard) and leave the
// rows as they are: the rows are the judge of the move ("X7a unchanged").
// Where a row pins a quirk, its name says so.

// ruleLog is the slog default while a test runs: it keeps each record as
// "LEVEL message key=value ...", in the order written.
type ruleLog struct {
	mu    sync.Mutex
	lines []string
}

// captureRuleLog installs a ruleLog until the test ends. slog.SetDefault
// also points the log package at it, and setting the old default back does
// not undo that, so both are restored.
func captureRuleLog(t *testing.T) *ruleLog {
	t.Helper()
	l := &ruleLog{}
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(l))
	t.Cleanup(func() {
		slog.SetDefault(prev)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return l
}

func (l *ruleLog) Enabled(context.Context, slog.Level) bool { return true }
func (l *ruleLog) WithAttrs([]slog.Attr) slog.Handler       { return l }
func (l *ruleLog) WithGroup(string) slog.Handler            { return l }

func (l *ruleLog) Handle(_ context.Context, r slog.Record) error {
	line := r.Level.String() + " " + r.Message
	r.Attrs(func(a slog.Attr) bool {
		line += " " + a.Key + "=" + a.Value.String()
		return true
	})
	l.mu.Lock()
	l.lines = append(l.lines, line)
	l.mu.Unlock()
	return nil
}

func (l *ruleLog) written() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// reconcileProvisioner reports a state, or an error, per container, and
// records every call made to it.
type reconcileProvisioner struct {
	states     map[string]string
	inspectErr map[string]error
	calls      []string
}

func (p *reconcileProvisioner) Enabled() bool {
	p.calls = append(p.calls, "Enabled")
	return true
}

func (p *reconcileProvisioner) Provision(orgID, containerName, _ string, _ map[string]string, _ hosting.ResourceLimits) error {
	p.calls = append(p.calls, "Provision "+containerName)
	return nil
}

func (p *reconcileProvisioner) Start(containerName string) error {
	p.calls = append(p.calls, "Start "+containerName)
	return nil
}

func (p *reconcileProvisioner) Stop(containerName string) error {
	p.calls = append(p.calls, "Stop "+containerName)
	return nil
}

func (p *reconcileProvisioner) Remove(containerName string, _ bool, _ string) error {
	p.calls = append(p.calls, "Remove "+containerName)
	return nil
}

func (p *reconcileProvisioner) ContainerState(containerName string) (string, error) {
	p.calls = append(p.calls, "ContainerState "+containerName)
	if err := p.inspectErr[containerName]; err != nil {
		return "", err
	}
	return p.states[containerName], nil
}

// reconcileStore is a hosted worker repository in memory. It records each
// Update attempted as "<id> <status> <detail quoted>", and fails a read or
// a write of the ids its maps name.
type reconcileStore struct {
	order     []string
	byID      map[string]hostedworkers.HostedWorker
	listErr   error
	findErr   map[string]error
	gone      map[string]bool // FindByID answers nil, as for a deleted row
	updateErr map[string]error
	updates   []string
}

func newReconcileStore(workers ...hostedworkers.HostedWorker) *reconcileStore {
	s := &reconcileStore{byID: map[string]hostedworkers.HostedWorker{}}
	for _, w := range workers {
		s.order = append(s.order, w.ID)
		s.byID[w.ID] = w
	}
	return s
}

func (s *reconcileStore) ListAll() ([]*hostedworkers.HostedWorker, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	var out []*hostedworkers.HostedWorker
	for _, id := range s.order {
		w := s.byID[id]
		out = append(out, &w)
	}
	return out, nil
}

func (s *reconcileStore) FindByID(id string) (*hostedworkers.HostedWorker, error) {
	if err := s.findErr[id]; err != nil {
		return nil, err
	}
	w, ok := s.byID[id]
	if !ok || s.gone[id] {
		return nil, nil
	}
	return &w, nil
}

func (s *reconcileStore) Update(w *hostedworkers.HostedWorker) error {
	s.updates = append(s.updates, fmt.Sprintf("%s %s %q", w.ID, w.Status, w.Detail))
	if err := s.updateErr[w.ID]; err != nil {
		return err
	}
	s.byID[w.ID] = *w
	return nil
}

func (s *reconcileStore) Save(*hostedworkers.HostedWorker) error {
	return errors.New("the reconcile creates no hosted worker")
}

func (s *reconcileStore) FindByOrg(string) (*hostedworkers.HostedWorker, error) {
	return nil, errors.New("the reconcile reads no hosted worker by workspace")
}

func (s *reconcileStore) Delete(string) error {
	return errors.New("the reconcile deletes no hosted worker")
}

// statuses is each stored worker's "<status>/<detail>", in list order.
func (s *reconcileStore) statuses() []string {
	var out []string
	for _, id := range s.order {
		out = append(out, s.byID[id].Status+"/"+s.byID[id].Detail)
	}
	return out
}

// TestReconcileHostedRunnersByContainerState pins how the boot reconcile
// maps one stored hosted runner and the state its container reports to the
// status and detail it stores: "missing" is an error with the detail
// "container not found"; "running" is running; "exited", "created",
// "paused" and "dead" are stopped; running and stopped clear the detail. A
// state none of these names leaves the record as it is (X7b's
// hostedworkers.Reconcile has no default branch). A record that already
// says what the state means is not written. The reconcile only reads
// container states: it never starts, stops, provisions or removes one, nor
// asks whether hosting is enabled (stage runners asks, before calling it).
func TestReconcileHostedRunnersByContainerState(t *testing.T) {
	// X7b re-points this at hostedworkers.Reconcile with identical rows.
	reconcile := func(p hosting.Provisioner, s *hostedworkers.DefaultService) { reconcileHostedRunners(p, s) }

	for _, c := range []struct {
		name                   string
		status, detail         string // stored
		state                  string // the container's
		wantStatus, wantDetail string
		written                bool
	}{
		{"running and running: not written", "running", "", "running", "running", "", false},
		{"provisioning, now running", "provisioning", "", "running", "running", "", true},
		{"stopped, now running", "stopped", "", "running", "running", "", true},
		{"an error, now running: the detail is cleared", "error", "container not found", "running", "running", "", true},
		{"running, now missing", "running", "", "missing", "error", "container not found", true},
		{"provisioning, now missing", "provisioning", "", "missing", "error", "container not found", true},
		{"missing and recorded so: not written", "error", "container not found", "missing", "error", "container not found", false},
		{"an error with another detail, now missing: the detail is replaced", "error", "image pull failed", "missing", "error", "container not found", true},
		{"running, now exited", "running", "", "exited", "stopped", "", true},
		{"running, now created", "running", "", "created", "stopped", "", true},
		{"running, now paused", "running", "", "paused", "stopped", "", true},
		{"running, now dead", "running", "", "dead", "stopped", "", true},
		{"stopped and exited: not written", "stopped", "", "exited", "stopped", "", false},
		{"stopped with a detail, exited: the detail is cleared", "stopped", "left over", "exited", "stopped", "", true},
		{"quirk: provisioning and created reads as stopped", "provisioning", "", "created", "stopped", "", true},
		{"an error, now dead: stopped, the detail cleared", "error", "container not found", "dead", "stopped", "", true},
		{"unknown state restarting: left as it is", "running", "", "restarting", "running", "", false},
		{"unknown state removing: left as it is", "stopped", "", "removing", "stopped", "", false},
		{"unknown empty state: left as it is, detail kept", "error", "image pull failed", "", "error", "image pull failed", false},
		{"quirk: states are matched case-sensitively, Running is unknown", "provisioning", "", "Running", "provisioning", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			logged := captureRuleLog(t)
			store := newReconcileStore(hostedworkers.HostedWorker{ID: "hw-1", OrgID: "org-1", ContainerName: "openv-runner-1",
				Status: c.status, Detail: c.detail})
			prov := &reconcileProvisioner{states: map[string]string{"openv-runner-1": c.state}}

			reconcile(prov, hostedworkers.NewDefaultService(store))

			if want := []string{"ContainerState openv-runner-1"}; !reflect.DeepEqual(prov.calls, want) {
				t.Errorf("provisioner calls %q, want %q", prov.calls, want)
			}
			var wantWrites []string
			if c.written {
				wantWrites = []string{fmt.Sprintf("hw-1 %s %q", c.wantStatus, c.wantDetail)}
			}
			if !reflect.DeepEqual(store.updates, wantWrites) {
				t.Errorf("writes %q, want %q", store.updates, wantWrites)
			}
			if got, want := store.statuses(), []string{c.wantStatus + "/" + c.wantDetail}; !reflect.DeepEqual(got, want) {
				t.Errorf("stored %q, want %q", got, want)
			}
			if got := logged.written(); len(got) != 0 {
				t.Errorf("logged %q, want nothing", got)
			}
		})
	}
}

// TestReconcileHostedRunnersFailures pins what the boot reconcile does when
// a store or the provisioner fails: a list that cannot be read is logged
// and no container is inspected; a container that cannot be inspected, or a
// status that cannot be read back or written, is logged with its container
// name and skips that runner only; the runners are inspected one by one in
// the order the list gives them. Nothing fails the boot.
func TestReconcileHostedRunnersFailures(t *testing.T) {
	// X7b re-points this at hostedworkers.Reconcile with identical rows.
	reconcile := func(p hosting.Provisioner, s *hostedworkers.DefaultService) { reconcileHostedRunners(p, s) }

	worker := func(n, status, detail string) hostedworkers.HostedWorker {
		return hostedworkers.HostedWorker{ID: "hw-" + n, OrgID: "org-" + n, ContainerName: "openv-runner-" + n, Status: status, Detail: detail}
	}
	for _, c := range []struct {
		name       string
		workers    []hostedworkers.HostedWorker
		states     map[string]string
		inspectErr map[string]error
		listErr    error
		findErr    map[string]error
		gone       map[string]bool
		updateErr  map[string]error
		wantCalls  []string
		wantWrites []string
		wantStored []string
		wantLog    []string
	}{
		{
			name:    "the list cannot be read: logged, no container inspected",
			listErr: errors.New("relation hosted_workers does not exist"),
			wantLog: []string{"WARN failed to list hosted workers for reconcile error=relation hosted_workers does not exist"},
		},
		{
			name: "no hosted runners: nothing asked, nothing written, nothing logged",
		},
		{
			name:       "a container that cannot be inspected is skipped, the next is reconciled",
			workers:    []hostedworkers.HostedWorker{worker("1", "running", ""), worker("2", "running", "")},
			states:     map[string]string{"openv-runner-2": "exited"},
			inspectErr: map[string]error{"openv-runner-1": errors.New("docker daemon not answering")},
			wantCalls:  []string{"ContainerState openv-runner-1", "ContainerState openv-runner-2"},
			wantWrites: []string{`hw-2 stopped ""`},
			wantStored: []string{"running/", "stopped/"},
			wantLog:    []string{"WARN failed to inspect hosted runner container=openv-runner-1 error=docker daemon not answering"},
		},
		{
			name:       "a record that cannot be read back is logged, the next is reconciled",
			workers:    []hostedworkers.HostedWorker{worker("1", "running", ""), worker("2", "stopped", "")},
			states:     map[string]string{"openv-runner-1": "missing", "openv-runner-2": "running"},
			findErr:    map[string]error{"hw-1": errors.New("connection reset by peer")},
			wantCalls:  []string{"ContainerState openv-runner-1", "ContainerState openv-runner-2"},
			wantWrites: []string{`hw-2 running ""`},
			wantStored: []string{"running/", "running/"},
			wantLog:    []string{"WARN failed to reconcile hosted runner container=openv-runner-1 error=connection reset by peer"},
		},
		{
			name:       "a record deleted between the list and the write: not found, logged",
			workers:    []hostedworkers.HostedWorker{worker("1", "running", "")},
			states:     map[string]string{"openv-runner-1": "dead"},
			gone:       map[string]bool{"hw-1": true},
			wantCalls:  []string{"ContainerState openv-runner-1"},
			wantStored: []string{"running/"},
			wantLog:    []string{"WARN failed to reconcile hosted runner container=openv-runner-1 error=hosted worker not found"},
		},
		{
			name:       "a status that cannot be written is logged, the next is reconciled",
			workers:    []hostedworkers.HostedWorker{worker("1", "running", ""), worker("2", "provisioning", "")},
			states:     map[string]string{"openv-runner-1": "missing", "openv-runner-2": "created"},
			updateErr:  map[string]error{"hw-1": errors.New("deadlock detected")},
			wantCalls:  []string{"ContainerState openv-runner-1", "ContainerState openv-runner-2"},
			wantWrites: []string{`hw-1 error "container not found"`, `hw-2 stopped ""`},
			wantStored: []string{"running/", "stopped/"},
			wantLog:    []string{"WARN failed to reconcile hosted runner container=openv-runner-1 error=deadlock detected"},
		},
		{
			name: "each runner is inspected once, in list order, and only a change is written",
			workers: []hostedworkers.HostedWorker{worker("3", "provisioning", ""), worker("1", "running", ""),
				worker("2", "running", ""), worker("4", "error", "container not found")},
			states: map[string]string{"openv-runner-3": "running", "openv-runner-1": "running", "openv-runner-2": "paused",
				"openv-runner-4": "restarting"},
			wantCalls: []string{"ContainerState openv-runner-3", "ContainerState openv-runner-1", "ContainerState openv-runner-2",
				"ContainerState openv-runner-4"},
			wantWrites: []string{`hw-3 running ""`, `hw-2 stopped ""`},
			wantStored: []string{"running/", "running/", "stopped/", "error/container not found"},
		},
		{
			name:       "every container fails to answer: each is logged, nothing written",
			workers:    []hostedworkers.HostedWorker{worker("1", "running", ""), worker("2", "stopped", "")},
			inspectErr: map[string]error{"openv-runner-1": errors.New("timeout"), "openv-runner-2": errors.New("timeout")},
			wantCalls:  []string{"ContainerState openv-runner-1", "ContainerState openv-runner-2"},
			wantStored: []string{"running/", "stopped/"},
			wantLog: []string{"WARN failed to inspect hosted runner container=openv-runner-1 error=timeout",
				"WARN failed to inspect hosted runner container=openv-runner-2 error=timeout"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			logged := captureRuleLog(t)
			store := newReconcileStore(c.workers...)
			store.listErr, store.findErr, store.gone, store.updateErr = c.listErr, c.findErr, c.gone, c.updateErr
			prov := &reconcileProvisioner{states: c.states, inspectErr: c.inspectErr}

			reconcile(prov, hostedworkers.NewDefaultService(store))

			if !reflect.DeepEqual(prov.calls, c.wantCalls) {
				t.Errorf("provisioner calls %q, want %q", prov.calls, c.wantCalls)
			}
			if !reflect.DeepEqual(store.updates, c.wantWrites) {
				t.Errorf("writes %q, want %q", store.updates, c.wantWrites)
			}
			if got := store.statuses(); !reflect.DeepEqual(got, c.wantStored) {
				t.Errorf("stored %q, want %q", got, c.wantStored)
			}
			if got := logged.written(); !reflect.DeepEqual(got, c.wantLog) {
				t.Errorf("logged %q, want %q", got, c.wantLog)
			}
		})
	}
}

// guardOrgs is a workspace repository that answers FindOrgByID with org
// (nil: no such workspace) or err, and records the ids asked for.
type guardOrgs struct {
	orgs.Repository
	org   *orgs.Org
	err   error
	asked []string
}

func (r *guardOrgs) FindOrgByID(id string) (*orgs.Org, error) {
	r.asked = append(r.asked, id)
	if r.err != nil || r.org == nil {
		return nil, r.err
	}
	o := *r.org
	o.ID = id
	return &o, nil
}

// guardRuns is a run repository that answers MonthlySpend with spend and
// err, and records each call.
type guardRuns struct {
	agentruns.Repository
	spend float64
	err   error
	calls []guardSpendCall
}

type guardSpendCall struct {
	orgID      string
	monthStart time.Time
}

func (r *guardRuns) MonthlySpend(orgID string, monthStart time.Time) (float64, error) {
	r.calls = append(r.calls, guardSpendCall{orgID, monthStart})
	return r.spend, r.err
}

// TestBudgetGuardRule pins the over-budget soft-block whole: a launch is
// refused once the workspace's month-to-date spend has reached a positive
// monthly budget, with the refusal's exact text, and let through, with no
// reason, when the workspace cannot be read, has no budget, a budget of
// zero or less, or its spend cannot be read. Its spend is read only when it
// has a budget, from the first instant of the current month in UTC. The
// amounts print to the cent, rounded, so a budget under half a cent reads
// $0.00 (quirk). TestBudgetGuardBlocksAtExactlyTheBudget holds #379 bug
// 100's three rows; this table is X7c's judge.
func TestBudgetGuardRule(t *testing.T) {
	// X7c re-points this at agentruns.BudgetGuard with identical rows.
	guard := func(o *orgs.DefaultService, r *agentruns.DefaultService) func(string) (bool, string) {
		return budgetGuard(o, r)
	}

	budget := func(v float64) *float64 { return &v }
	const tail = "; new runs are blocked until next month or the budget is raised"
	for _, c := range []struct {
		name       string
		org        *orgs.Org // nil: no such workspace
		orgErr     error
		spend      float64
		spendErr   error
		wantBlock  bool
		wantReason string
		spendRead  bool
	}{
		{name: "no such workspace", spend: 100},
		{name: "the workspace cannot be read", orgErr: errors.New("connection refused"), spend: 100},
		{name: "no budget set", org: &orgs.Org{}, spend: 100},
		{name: "a budget of zero", org: &orgs.Org{MonthlyBudgetUSD: budget(0)}, spend: 100},
		{name: "a negative budget", org: &orgs.Org{MonthlyBudgetUSD: budget(-5)}, spend: 100},
		{name: "nothing spent", org: &orgs.Org{MonthlyBudgetUSD: budget(25)}, spend: 0, spendRead: true},
		{name: "a cent under the budget", org: &orgs.Org{MonthlyBudgetUSD: budget(25)}, spend: 24.99, spendRead: true},
		{name: "a tenth of a cent under, which would print as the budget", org: &orgs.Org{MonthlyBudgetUSD: budget(25)},
			spend: 24.999, spendRead: true},
		{name: "exactly the budget", org: &orgs.Org{MonthlyBudgetUSD: budget(25)}, spend: 25, spendRead: true, wantBlock: true,
			wantReason: "this workspace has reached its $25.00 monthly budget ($25.00 spent)" + tail},
		{name: "a cent over the budget", org: &orgs.Org{MonthlyBudgetUSD: budget(25)}, spend: 25.01, spendRead: true, wantBlock: true,
			wantReason: "this workspace has reached its $25.00 monthly budget ($25.01 spent)" + tail},
		{name: "far over the budget: no thousands separator", org: &orgs.Org{MonthlyBudgetUSD: budget(10)}, spend: 1234.5,
			spendRead: true, wantBlock: true,
			wantReason: "this workspace has reached its $10.00 monthly budget ($1234.50 spent)" + tail},
		{name: "a budget in fractions of a cent prints rounded", org: &orgs.Org{MonthlyBudgetUSD: budget(9.999)}, spend: 9.999,
			spendRead: true, wantBlock: true,
			wantReason: "this workspace has reached its $10.00 monthly budget ($10.00 spent)" + tail},
		{name: "quirk: a budget under half a cent reads $0.00", org: &orgs.Org{MonthlyBudgetUSD: budget(0.004)}, spend: 0.004,
			spendRead: true, wantBlock: true,
			wantReason: "this workspace has reached its $0.00 monthly budget ($0.00 spent)" + tail},
		{name: "the spend cannot be read", org: &orgs.Org{MonthlyBudgetUSD: budget(25)}, spendErr: errors.New("statement timeout"),
			spendRead: true},
		{name: "the spend cannot be read, though an amount over came with the error", org: &orgs.Org{MonthlyBudgetUSD: budget(25)},
			spend: 30, spendErr: errors.New("statement timeout"), spendRead: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			orgRepo := &guardOrgs{org: c.org, err: c.orgErr}
			runRepo := &guardRuns{spend: c.spend, err: c.spendErr}
			check := guard(orgs.NewDefaultService(orgRepo), agentruns.NewDefaultService(runRepo, nil, nil))

			before := time.Now().UTC()
			blocked, reason := check("org-7")
			after := time.Now().UTC()

			if blocked != c.wantBlock || reason != c.wantReason {
				t.Errorf("guard(org-7) = %v, %q; want %v, %q", blocked, reason, c.wantBlock, c.wantReason)
			}
			if want := []string{"org-7"}; !reflect.DeepEqual(orgRepo.asked, want) {
				t.Errorf("workspaces read %q, want %q", orgRepo.asked, want)
			}
			if !c.spendRead {
				if len(runRepo.calls) != 0 {
					t.Errorf("the spend was read (%v); want it left alone", runRepo.calls)
				}
				return
			}
			if len(runRepo.calls) != 1 {
				t.Fatalf("the spend was read %d times, want once", len(runRepo.calls))
			}
			got := runRepo.calls[0]
			if got.orgID != "org-7" {
				t.Errorf("spend read for %q, want org-7", got.orgID)
			}
			monthStart := func(now time.Time) time.Time { return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC) }
			if !got.monthStart.Equal(monthStart(before)) && !got.monthStart.Equal(monthStart(after)) {
				t.Errorf("spend read from %v, want the month's first instant in UTC, %v", got.monthStart, monthStart(before))
			}
			if got.monthStart.Location() != time.UTC {
				t.Errorf("spend read from a time in %v, want UTC", got.monthStart.Location())
			}
		})
	}
}
