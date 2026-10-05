package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/snapshot"
)

// The bodies below are built from the server's own types, so the fake API
// answers in the shapes the real one does: a fake that answered GET
// /baselines/{id} with the baseline's fields hid #379 bug 202.

// snapshotBody is GET /api/v1/baselines/{id}'s answer: the baseline's stored
// snapshot, the project's JSON export as exports.Snapshot renders it, which
// names the project and none of the baseline's own fields.
func snapshotBody(t *testing.T, projectID string) string {
	t.Helper()
	raw, err := json.MarshalIndent(&snapshot.ProjectExport{
		ExportedAt:  time.Date(2026, 1, 1, 9, 29, 59, 0, time.UTC),
		Version:     "1.0",
		ProjectID:   projectID,
		ProjectName: "Seal rig",
		Artifacts:   []*artifacts.Artifact{{ID: "a1", ProjectID: projectID, Type: "requirement", Title: "Seal integrity"}},
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// baselineListBody is GET /api/v1/projects/{id}/baselines's answer: the
// project's baselines newest first, without their snapshots, as
// ListBaselines encodes them (null when there are none).
func baselineListBody(t *testing.T, list ...*baselines.Baseline) string {
	t.Helper()
	raw, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// baselineAPI fakes the API get_baseline reads: each path in answers is
// answered 200 with its body, any other 404 as GetBaseline answers an
// unknown id. It records every request as "METHOD path", in order.
func baselineAPI(t *testing.T, answers map[string]string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var log []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		log = append(log, r.Method+" "+r.URL.Path)
		mu.Unlock()
		body, ok := answers[r.URL.Path]
		if !ok || r.Method != http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"baseline not found"}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), log...)
	}
}

func TestGetBaseline(t *testing.T) {
	author := "u1"
	v1 := &baselines.Baseline{ID: "b1", ProjectID: "p1", Name: "v1",
		CreatedAt: time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC), CreatedBy: &author, CreatedByName: "Dave"}
	v2 := &baselines.Baseline{ID: "b2", ProjectID: "p1", Name: "v2",
		CreatedAt: time.Date(2026, 2, 1, 9, 30, 0, 0, time.UTC)}
	both := []string{"GET /api/v1/baselines/b1", "GET /api/v1/projects/p1/baselines"}

	run := func(t *testing.T, answers map[string]string) (string, []string, error) {
		t.Helper()
		server, requests := baselineAPI(t, answers)
		out, err := toolByName(t, "get_baseline").Handler(
			NewClient(server.URL, "test-token"), map[string]interface{}{"id": "b1"})
		return out, requests(), err
	}
	checkRequests := func(t *testing.T, got, want []string) {
		t.Helper()
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("requests = %q, want %q", got, want)
		}
	}

	// #379 bug 202: from the snapshot alone the tool answered
	// {"id":"","project_id":"p1","name":"","created_at":""}.
	t.Run("answers with the name and capture time from the project's list", func(t *testing.T) {
		out, requests, err := run(t, map[string]string{
			"/api/v1/baselines/b1":          snapshotBody(t, "p1"),
			"/api/v1/projects/p1/baselines": baselineListBody(t, v2, v1),
		})
		if err != nil {
			t.Fatal(err)
		}
		checkRequests(t, requests, both)
		if want := `{"id":"b1","project_id":"p1","name":"v1","created_at":"2026-01-01T09:30:00Z"}`; out != want {
			t.Errorf("output = %s, want %s", out, want)
		}
	})

	t.Run("a baseline missing from its project's list is an error", func(t *testing.T) {
		for name, list := range map[string]string{
			"other baselines": baselineListBody(t, v2),
			"none":            baselineListBody(t),
		} {
			t.Run(name, func(t *testing.T) {
				out, requests, err := run(t, map[string]string{
					"/api/v1/baselines/b1":          snapshotBody(t, "p1"),
					"/api/v1/projects/p1/baselines": list,
				})
				if err == nil || !strings.Contains(err.Error(), "baseline b1 is not in the baseline list of its project p1") {
					t.Fatalf("err = %v (output %q), want baseline b1 named as missing from p1's list", err, out)
				}
				checkRequests(t, requests, both)
			})
		}
	})

	t.Run("a snapshot that names no project is an error", func(t *testing.T) {
		out, requests, err := run(t, map[string]string{
			"/api/v1/baselines/b1": snapshotBody(t, ""),
		})
		if err == nil || !strings.Contains(err.Error(), "baseline b1: its snapshot names no project") {
			t.Fatalf("err = %v (output %q), want the snapshot named as having no project", err, out)
		}
		checkRequests(t, requests, both[:1])
	})

	t.Run("an unknown baseline is the API's 404", func(t *testing.T) {
		_, requests, err := run(t, map[string]string{
			"/api/v1/projects/p1/baselines": baselineListBody(t, v1),
		})
		if err == nil || !strings.Contains(err.Error(), "API 404") || !strings.Contains(err.Error(), "baseline not found") {
			t.Fatalf("err = %v, want the API's 404", err)
		}
		checkRequests(t, requests, both[:1])
	})
}
