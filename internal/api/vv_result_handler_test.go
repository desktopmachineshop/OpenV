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
	// latest answers LatestResults per project, as the real service does:
	// a project it does not name has no results.
	latest map[string]map[string]*vv.TestResult
}

// vvHandler is a handler with only a V&V service, the one these handlers
// reach before they answer.
func vvHandler(svc vv.Service) *Handler { return &Handler{vvService: svc} }

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
		h := vvHandler(svc)
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
		{"closed run answers 409", fmt.Errorf("this test run is completed; %w", vv.ErrRunClosed), http.StatusConflict,
			"this test run is completed; only in-progress runs accept new results"},
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

// fakeVVDelete answers DeleteRun with a set error.
type fakeVVDelete struct {
	fakeVVService
	deleteErr error
	deletes   int
}

func (f *fakeVVDelete) DeleteRun(id string) error {
	f.deletes++
	return f.deleteErr
}

// TestDeleteTestRunKeepsARunWithResults: a run that holds results answers 409
// in the store's words, which say the completed run is already closed (REQ-13:
// its results are kept), an empty run is still deleted, and a run gone in
// between answers 404 rather than 500.
func TestDeleteTestRunKeepsARunWithResults(t *testing.T) {
	del := func(t *testing.T, err error) *httptest.ResponseRecorder {
		t.Helper()
		svc := &fakeVVDelete{deleteErr: err}
		svc.run = &vv.TestRun{ID: "trun-1", ProjectID: "proj-1", Status: vv.RunStatusCompleted}
		h := vvHandler(svc)
		r := httptest.NewRequest(http.MethodDelete, "/api/v1/test-runs/trun-1", nil)
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "root", IsAdmin: true}))
		r = mux.SetURLVars(r, map[string]string{"id": "trun-1"})
		w := httptest.NewRecorder()
		h.DeleteTestRun(w, r)
		if svc.deletes != 1 {
			t.Fatalf("DeleteRun called %d times, want 1", svc.deletes)
		}
		return w
	}

	for _, tc := range []struct {
		name     string
		err      error
		wantCode int
		wantText string
	}{
		{"a run with results answers 409", fmt.Errorf("%w: this one is already completed", vv.ErrRunHasResults),
			http.StatusConflict, `{"error":"a test run that holds results is kept: this one is already completed"}`},
		{"a run gone in between answers 404", vv.ErrRunNotFound, http.StatusNotFound, "test run not found"},
		{"an empty run is deleted", nil, http.StatusNoContent, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := del(t, tc.err)
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.wantText) {
				t.Fatalf("body %q is missing %q", w.Body.String(), tc.wantText)
			}
		})
	}
}
