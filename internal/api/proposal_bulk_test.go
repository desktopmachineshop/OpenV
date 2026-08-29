package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeProposalService drives BulkReviewProposals: byID answers Get, and
// approveErr/rejectErr simulate per-proposal apply failures while still
// recording the call — mirroring how the real Approve returns both the
// mutated proposal and the apply error.
type fakeProposalService struct {
	proposals.Service
	byID       map[string]*proposals.Proposal
	approveErr map[string]error
	rejectErr  map[string]error
	approved   []string
	rejected   []string
}

func (f *fakeProposalService) Get(id string) (*proposals.Proposal, error) {
	if p, ok := f.byID[id]; ok {
		return p, nil
	}
	return nil, proposals.ErrNotFound
}

func (f *fakeProposalService) Approve(id string, reviewedBy *string, note string) (*proposals.Proposal, error) {
	f.approved = append(f.approved, id)
	if err := f.approveErr[id]; err != nil {
		return nil, err
	}
	return f.byID[id], nil
}

func (f *fakeProposalService) Reject(id string, reviewedBy *string, note string) (*proposals.Proposal, error) {
	f.rejected = append(f.rejected, id)
	if err := f.rejectErr[id]; err != nil {
		return nil, err
	}
	return f.byID[id], nil
}

type bulkResponse struct {
	Results []bulkOutcome `json:"results"`
}

func bulkReq(t *testing.T, body string, user *users.User) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/proposals/bulk", strings.NewReader(body))
	if user != nil {
		r = r.WithContext(context.WithValue(r.Context(), ctxUser, user))
	}
	return r
}

func decodeBulk(t *testing.T, w *httptest.ResponseRecorder) bulkResponse {
	t.Helper()
	var resp bulkResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response %q is not valid bulk JSON: %v", w.Body.String(), err)
	}
	return resp
}

// TestBulkReviewProposalsValidation locks in the request contract: auth is
// required, the action must be approve|reject, ids must be present, and a
// request over the per-run proposal cap is refused outright (no partial
// processing).
func TestBulkReviewProposalsValidation(t *testing.T) {
	user := &users.User{ID: "root", IsAdmin: true}

	overCap := make([]string, proposals.MaxProposalsPerRun+1)
	for i := range overCap {
		overCap[i] = fmt.Sprintf("\"p-%d\"", i)
	}
	overCapBody := fmt.Sprintf(`{"ids":[%s],"action":"approve"}`, strings.Join(overCap, ","))

	cases := []struct {
		name     string
		body     string
		user     *users.User
		wantCode int
	}{
		{"no user answers 401", `{"ids":["p-1"],"action":"approve"}`, nil, http.StatusUnauthorized},
		{"invalid body answers 400", `{nope`, user, http.StatusBadRequest},
		{"unknown action answers 400", `{"ids":["p-1"],"action":"defer"}`, user, http.StatusBadRequest},
		{"missing action answers 400", `{"ids":["p-1"]}`, user, http.StatusBadRequest},
		{"empty ids answers 400", `{"ids":[],"action":"approve"}`, user, http.StatusBadRequest},
		{"over the cap answers 400", overCapBody, user, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeProposalService{byID: map[string]*proposals.Proposal{}}
			h := &Handler{proposalService: svc}
			w := httptest.NewRecorder()
			h.BulkReviewProposals(w, bulkReq(t, tc.body, tc.user))
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.wantCode, w.Body.String())
			}
			if len(svc.approved)+len(svc.rejected) != 0 {
				t.Fatal("no proposal may be reviewed when the request is refused")
			}
		})
	}
}

// TestBulkReviewProposalsPartialFailure locks in the partial-failure
// contract: the batch answers 200 with one outcome per requested id, in
// request order — applied, unknown id, and apply-failed rows side by side.
// An applier failure (a non-sentinel error) is sanitized in the outcome per
// the #146 contract: the client sees a stable message, not the internal
// detail, while a genuine proposal-domain validation sentinel passes through.
func TestBulkReviewProposalsPartialFailure(t *testing.T) {
	svc := &fakeProposalService{
		byID: map[string]*proposals.Proposal{
			"p-ok":    {ID: "p-ok", ProjectID: "proj-1", Status: proposals.StatusPending},
			"p-boom":  {ID: "p-boom", ProjectID: "proj-1", Status: proposals.StatusPending},
			"p-stale": {ID: "p-stale", ProjectID: "proj-1", Status: proposals.StatusApplied},
		},
		approveErr: map[string]error{
			// A raw applier error carrying an internal detail: must be sanitized.
			"p-boom": errors.New("apply failed: pq: relation \"artifacts\" does not exist"),
			// A genuine validation sentinel: safe to surface verbatim.
			"p-stale": proposals.ErrNotPending,
		},
	}
	h := &Handler{proposalService: svc}

	w := httptest.NewRecorder()
	body := `{"ids":["p-ok","p-missing","p-boom","p-stale"],"action":"approve","note":"batch"}`
	h.BulkReviewProposals(w, bulkReq(t, body, &users.User{ID: "root", IsAdmin: true}))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even on partial failure (body %q)", w.Code, w.Body.String())
	}
	resp := decodeBulk(t, w)
	want := []bulkOutcome{
		{ID: "p-ok", OK: true},
		{ID: "p-missing", Error: "proposal not found"},
		{ID: "p-boom", Error: "failed to apply approved proposal"},
		{ID: "p-stale", Error: proposals.ErrNotPending.Error()},
	}
	if len(resp.Results) != len(want) {
		t.Fatalf("results = %+v, want %d rows", resp.Results, len(want))
	}
	for i, wantRow := range want {
		if resp.Results[i] != wantRow {
			t.Fatalf("results[%d] = %+v, want %+v", i, resp.Results[i], wantRow)
		}
	}
	// The sanitized outcome must not leak the internal error text.
	if strings.Contains(w.Body.String(), "pq:") {
		t.Errorf("bulk response leaked internal error detail: %q", w.Body.String())
	}
	if len(svc.approved) != 3 {
		t.Fatalf("approved = %v, want three attempted approvals in request order", svc.approved)
	}
}

// TestBulkReviewProposalsAppliesArtifactsBeforeLinks locks in the dependency
// ordering for issue #235: a create_link proposal may reference a sibling
// create_artifact proposal's temporary ref, which only resolves after the
// artifact is applied. Even when the client lists the link first, the batch
// must approve the artifact proposal before the link proposal.
func TestBulkReviewProposalsAppliesArtifactsBeforeLinks(t *testing.T) {
	svc := &fakeProposalService{
		byID: map[string]*proposals.Proposal{
			"p-link": {ID: "p-link", ProjectID: "proj-1", Op: proposals.OpCreateLink, Status: proposals.StatusPending},
			"p-art":  {ID: "p-art", ProjectID: "proj-1", Op: proposals.OpCreateArtifact, Status: proposals.StatusPending},
		},
	}
	h := &Handler{proposalService: svc}

	w := httptest.NewRecorder()
	// Client lists the link before the artifact on purpose.
	body := `{"ids":["p-link","p-art"],"action":"approve"}`
	h.BulkReviewProposals(w, bulkReq(t, body, &users.User{ID: "root", IsAdmin: true}))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if len(svc.approved) != 2 || svc.approved[0] != "p-art" || svc.approved[1] != "p-link" {
		t.Fatalf("approve order = %v, want [p-art p-link] (artifact before its dependent link)", svc.approved)
	}
}

// TestBulkReviewProposalsAuthz locks in that each id is authorized against
// its own project with the same editor bar as a single review: rows the
// user cannot edit fail per-row without touching the service, while rows
// they can edit still proceed in the same batch.
func TestBulkReviewProposalsAuthz(t *testing.T) {
	svc := &fakeProposalService{
		byID: map[string]*proposals.Proposal{
			"p-mine":   {ID: "p-mine", ProjectID: "proj-mine", Status: proposals.StatusPending},
			"p-theirs": {ID: "p-theirs", ProjectID: "proj-theirs", Status: proposals.StatusPending},
			"p-viewer": {ID: "p-viewer", ProjectID: "proj-viewed", Status: proposals.StatusPending},
		},
	}
	h := &Handler{
		proposalService: svc,
		projectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-mine":   {ID: "proj-mine", OrgID: "org-1"},
			"proj-theirs": {ID: "proj-theirs", OrgID: "org-1"},
			"proj-viewed": {ID: "proj-viewed", OrgID: "org-1"},
		}},
		memberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-mine":   {"eve": members.RoleEditor},
			"proj-viewed": {"eve": members.RoleViewer},
		}},
	}

	w := httptest.NewRecorder()
	body := `{"ids":["p-mine","p-theirs","p-viewer"],"action":"reject"}`
	h.BulkReviewProposals(w, bulkReq(t, body, &users.User{ID: "eve"}))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	resp := decodeBulk(t, w)
	if len(resp.Results) != 3 {
		t.Fatalf("results = %+v, want 3 rows", resp.Results)
	}
	if !resp.Results[0].OK {
		t.Fatalf("editor's own row should pass, got %+v", resp.Results[0])
	}
	for i, id := range []string{"p-theirs", "p-viewer"} {
		row := resp.Results[i+1]
		if row.OK || row.Error == "" {
			t.Fatalf("row for %s should fail authz, got %+v", id, row)
		}
	}
	if len(svc.rejected) != 1 || svc.rejected[0] != "p-mine" {
		t.Fatalf("rejected = %v: unauthorized rows must never reach the service", svc.rejected)
	}
}
