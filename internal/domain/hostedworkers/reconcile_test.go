package hostedworkers

import (
	"bytes"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

// The boot reconcile's own tests, against the Service interface. cmd/server's
// TestReconcileHostedRunnersByContainerState and
// TestReconcileHostedRunnersFailures (refactor step X7a) are its
// characterization, through the real service and the boot's call.

// statesOf answers a state, or an error, per container, and records the
// containers asked about.
type statesOf struct {
	states map[string]string
	errs   map[string]error
	asked  []string
}

func (p *statesOf) ContainerState(name string) (string, error) {
	p.asked = append(p.asked, name)
	return p.states[name], p.errs[name]
}

// reconcileService is a Service that lists workers and records each
// SetStatus as "<id> <status> <detail>"; Create, Get and Delete fail the
// test, since the reconcile makes none of them.
type reconcileService struct {
	t       *testing.T
	workers []*HostedWorker
	listErr error
	setErr  map[string]error
	set     []string
}

func (s *reconcileService) ListAll() ([]*HostedWorker, error) { return s.workers, s.listErr }

func (s *reconcileService) SetStatus(id, status, detail string) (*HostedWorker, error) {
	s.set = append(s.set, id+" "+status+" "+detail)
	return nil, s.setErr[id]
}

func (s *reconcileService) Create(string, string, *string, *string) (*HostedWorker, error) {
	s.t.Error("the reconcile creates no hosted worker")
	return nil, nil
}

func (s *reconcileService) Get(string) (*HostedWorker, error) {
	s.t.Error("the reconcile reads no hosted worker by workspace")
	return nil, nil
}

func (s *reconcileService) Delete(string) error {
	s.t.Error("the reconcile deletes no hosted worker")
	return nil
}

// logInto points slog's default at a text buffer, without times, until the
// test ends.
func logInto(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestReconcileMapsEachKnownStateAndLeavesOthers(t *testing.T) {
	for _, c := range []struct {
		state, status, detail string
		want                  []string // SetStatus calls
	}{
		{"missing", StatusRunning, "", []string{"w error container not found"}},
		{"running", StatusStopped, "", []string{"w running "}},
		{"running", StatusError, "container not found", []string{"w running "}},
		{"exited", StatusRunning, "", []string{"w stopped "}},
		{"created", StatusProvisioning, "", []string{"w stopped "}},
		{"paused", StatusRunning, "", []string{"w stopped "}},
		{"dead", StatusError, "container not found", []string{"w stopped "}},
		{"running", StatusRunning, "", nil},
		{"exited", StatusStopped, "", nil},
		{"missing", StatusError, "container not found", nil},
		// No default branch: a state none of the cases names is no change.
		{"restarting", StatusRunning, "", nil},
		{"Running", StatusProvisioning, "", nil},
		{"", StatusError, "image pull failed", nil},
	} {
		t.Run(c.state+" over "+c.status, func(t *testing.T) {
			logged := logInto(t)
			svc := &reconcileService{t: t, workers: []*HostedWorker{{ID: "w", ContainerName: "c", Status: c.status, Detail: c.detail}}}
			containers := &statesOf{states: map[string]string{"c": c.state}}

			Reconcile(containers, svc)

			if !reflect.DeepEqual(svc.set, c.want) {
				t.Errorf("SetStatus calls %q, want %q", svc.set, c.want)
			}
			if !reflect.DeepEqual(containers.asked, []string{"c"}) {
				t.Errorf("containers asked %q, want [c]", containers.asked)
			}
			if logged.Len() != 0 {
				t.Errorf("logged %q, want nothing", logged.String())
			}
		})
	}
}

func TestReconcileLogsAFailureAndGoesOn(t *testing.T) {
	logged := logInto(t)
	svc := &reconcileService{t: t, workers: []*HostedWorker{
		{ID: "w1", ContainerName: "c1", Status: StatusRunning},
		{ID: "w2", ContainerName: "c2", Status: StatusRunning},
		{ID: "w3", ContainerName: "c3", Status: StatusRunning},
	}, setErr: map[string]error{"w2": errors.New("deadlock detected")}}
	containers := &statesOf{
		states: map[string]string{"c2": "missing", "c3": "exited"},
		errs:   map[string]error{"c1": errors.New("timeout")},
	}

	Reconcile(containers, svc)

	if want := []string{"c1", "c2", "c3"}; !reflect.DeepEqual(containers.asked, want) {
		t.Errorf("containers asked %q, want %q", containers.asked, want)
	}
	if want := []string{"w2 error container not found", "w3 stopped "}; !reflect.DeepEqual(svc.set, want) {
		t.Errorf("SetStatus calls %q, want %q", svc.set, want)
	}
	want := `level=WARN msg="failed to inspect hosted runner" container=c1 error=timeout
level=WARN msg="failed to reconcile hosted runner" container=c2 error="deadlock detected"
`
	if logged.String() != want {
		t.Errorf("logged\n%s\nwant\n%s", logged.String(), want)
	}
}

func TestReconcileLogsAListItCannotReadAndAsksNothing(t *testing.T) {
	logged := logInto(t)
	svc := &reconcileService{t: t, listErr: errors.New("relation hosted_workers does not exist")}
	containers := &statesOf{}

	Reconcile(containers, svc)

	if len(containers.asked) != 0 || len(svc.set) != 0 {
		t.Errorf("asked %q and set %q, want neither", containers.asked, svc.set)
	}
	if got := logged.String(); !strings.Contains(got, `msg="failed to list hosted workers for reconcile" error="relation hosted_workers does not exist"`) ||
		strings.Count(got, "\n") != 1 {
		t.Errorf("logged %q, want the one list warning", got)
	}
}
