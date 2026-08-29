package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// fakeVVService embeds the interface so only the methods UpsertTestResult
// touches need implementations.
type fakeVVService struct {
	vv.Service
	run       *vv.TestRun
	upsertErr error
	upserts   int
}

func (f *fakeVVService) GetRun(id string) (*vv.TestRun, error) {
	if f.run == nil {
		return nil, vv.ErrRunNotFound
	}
	return f.run, nil
}

func (f *fakeVVService) UpsertResult(runID string, req vv.UpsertResultRequest, executedBy *string, actor, agentRunID string) (*vv.TestResult, error) {
	f.upserts++
	if f.upsertErr != nil {
		return nil, f.upsertErr
	}
	return &vv.TestResult{ID: "res-1", RunID: runID, TestCaseID: req.TestCaseID}, nil
}

// TestUpsertTestResultErrorContract locks in the sentinel-vs-internal split:
// domain sentinels keep their status and text, while any other failure (a DB
// blip, say) answers 5xx — an agent recording a result retries on 5xx, and
// its outcome must not be lost to an internal error mislabeled as 4xx — and
// never leaks internal error text.
func TestUpsertTestResultErrorContract(t *testing.T) {
	const internalDetail = "pq: deadlock detected"

	upsert := func(t *testing.T, svc *fakeVVService) *httptest.ResponseRecorder {
		t.Helper()
		h := &Handler{vvService: svc}
		body := `{"test_case_id":"tc-1","status":"pass"}`
		r := httptest.NewRequest(http.MethodPost, "/api/v1/test-runs/trun-1/results", strings.NewReader(body))
		// Platform admin passes the role check without further services.
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "root", IsAdmin: true}))
		r = mux.SetURLVars(r, map[string]string{"id": "trun-1"})
		w := httptest.NewRecorder()
		h.UpsertTestResult(w, r)
		return w
	}

	newSvc := func(err error) *fakeVVService {
		return &fakeVVService{run: &vv.TestRun{ID: "trun-1", ProjectID: "proj-1", Status: vv.RunStatusInProgress}, upsertErr: err}
	}

	cases := []struct {
		name     string
		err      error
		wantCode int
		wantText string // must appear in the body
	}{
		{"agent-barred case answers 403", fmt.Errorf("%w (Drop test: physical)", vv.ErrNotAgentExecutable), http.StatusForbidden, vv.ErrNotAgentExecutable.Error()},
		{"invalid status answers 400", vv.ErrInvalidStatus, http.StatusBadRequest, vv.ErrInvalidStatus.Error()},
		{"non-test-case artifact answers 400", vv.ErrNotTestCase, http.StatusBadRequest, vv.ErrNotTestCase.Error()},
		{"unknown test case answers 404", artifacts.ErrNotFound, http.StatusNotFound, artifacts.ErrNotFound.Error()},
		{"success answers 200", nil, http.StatusOK, "res-1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := upsert(t, newSvc(tc.err))
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.wantText) {
				t.Fatalf("body %q is missing %q", w.Body.String(), tc.wantText)
			}
		})
	}

	t.Run("internal error answers 500 without leaking", func(t *testing.T) {
		w := upsert(t, newSvc(errors.New(internalDetail)))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 (body %q)", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), internalDetail) {
			t.Fatalf("500 body %q leaks the internal error text", w.Body.String())
		}
	})
}
