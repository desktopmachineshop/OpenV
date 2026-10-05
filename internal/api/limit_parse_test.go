package api

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// Refactor plan X3a (docs/plans/codebase-refactor.md §6.7, quirk Q8):
// characterization of the seven inline ?limit= parsers that X3c replaces
// with named limitPolicy values and parseLimit. Each handler is driven with
// the same edge values plus its own default, maximum and maximum+1, and a
// row records the status and the limit the next layer was handed. Where
// the default and the cap live below the handler, the row records that too:
// the interview and shared-product services apply their own (a recording
// wrapper around the real service, over a recording repository), and the
// event and agent-run repositories reset an out-of-range limit to 100
// (persistence-v3), which internal/persistence/postgres/limit_clamp_test.go
// pins.
//
// strconv.Atoi saturates on overflow: "99999999999999999999" parses to
// math.MaxInt with an error, which a parser that discards the error keeps.

// limitParseEdges is the edge set every table covers, as raw query strings
// ("" sends no limit parameter at all). The leading space and the plus sign
// are URL-encoded, so the handler reads " 5" and "+5".
var limitParseEdges = []string{
	"",
	"limit=",
	"limit=0",
	"limit=-1",
	"limit=1",
	"limit=99999999999999999999",
	"limit=-99999999999999999999",
	"limit=abc",
	"limit=5x",
	"limit=%205",
	"limit=%2B5",
	"limit=0x10",
}

// limitParseCase is one ?limit= value and what the handler did with it.
type limitParseCase struct {
	query      string // the raw limit query, "" for none
	wantStatus int
	wantBody   string // compared for a refusal only (a status other than 200)
	wantNext   []int  // the limit of each call the next layer saw; nil when never called
}

// limitParseServiceCase adds what the real domain service handed its
// repository, for the two handlers whose default and cap live in the service.
type limitParseServiceCase struct {
	limitParseCase
	wantRepo []int
}

// limitParseCovers fails a table that leaves out an edge value.
func limitParseCovers(t *testing.T, queries []string) {
	t.Helper()
	have := map[string]bool{}
	for _, q := range queries {
		if have[q] {
			t.Errorf("query %q has two rows", q)
		}
		have[q] = true
	}
	for _, edge := range limitParseEdges {
		if !have[edge] {
			t.Errorf("no row for the edge value %q", edge)
		}
	}
}

// limitParseTarget joins a handler's fixed query with a row's limit query.
func limitParseTarget(path, fixed, limit string) string {
	q := fixed
	if limit != "" {
		if q != "" {
			q += "&"
		}
		q += limit
	}
	if q == "" {
		return path
	}
	return path + "?" + q
}

// limitParseCheck compares a response and the limits the next layer saw.
func limitParseCheck(t *testing.T, tc limitParseCase, w *httptest.ResponseRecorder, next []int) {
	t.Helper()
	if w.Code != tc.wantStatus {
		t.Errorf("status = %d, want %d (body %q)", w.Code, tc.wantStatus, w.Body.String())
	}
	if tc.wantStatus != http.StatusOK && w.Body.String() != tc.wantBody {
		t.Errorf("body = %q, want %q", w.Body.String(), tc.wantBody)
	}
	if !reflect.DeepEqual(next, tc.wantNext) {
		t.Errorf("next layer saw limits %v, want %v", next, tc.wantNext)
	}
}

// limitParseAs attaches a signed-in user and an active workspace.
func limitParseAs(r *http.Request, userID, orgID string) *http.Request {
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
	if orgID != "" {
		ctx = context.WithValue(ctx, ctxActiveOrg, orgID)
	}
	return r.WithContext(ctx)
}

// TestLimitParseAgentRuns pins agent_run_handlers.go's ListAgentRuns:
// `limit, _ := strconv.Atoi(...)` and nothing else, so whatever Atoi
// answers reaches RunService.List; the repository's 100/500 rule applies
// below it (limit_clamp_test.go).
func TestLimitParseAgentRuns(t *testing.T) {
	cases := []limitParseCase{
		{"", 200, "", []int{0}},
		{"limit=", 200, "", []int{0}},
		{"limit=0", 200, "", []int{0}},
		{"limit=-1", 200, "", []int{-1}},
		{"limit=1", 200, "", []int{1}},
		{"limit=100", 200, "", []int{100}}, // the repository's default
		{"limit=500", 200, "", []int{500}}, // the repository's maximum
		{"limit=501", 200, "", []int{501}},
		{"limit=99999999999999999999", 200, "", []int{math.MaxInt}},
		{"limit=-99999999999999999999", 200, "", []int{math.MinInt}},
		{"limit=abc", 200, "", []int{0}},
		{"limit=5x", 200, "", []int{0}},
		{"limit=%205", 200, "", []int{0}},
		{"limit=%2B5", 200, "", []int{5}},
		{"limit=0x10", 200, "", []int{0}},
	}
	queries := []string{}
	for _, tc := range cases {
		queries = append(queries, tc.query)
		t.Run("query "+tc.query, func(t *testing.T) {
			runs := &fakeRunService{}
			h := newTestHandler(t, func(h *Handler) { h.RunService = runs })
			r := limitParseAs(httptest.NewRequest(http.MethodGet, limitParseTarget("/api/v1/agent-runs", "", tc.query), nil), "u1", "org-1")
			w := httptest.NewRecorder()
			h.ListAgentRuns(w, r)
			var next []int
			for _, f := range runs.listFilters {
				next = append(next, f.Limit)
			}
			limitParseCheck(t, tc, w, next)
		})
	}
	limitParseCovers(t, queries)
}

// limitParseArtifactService records the page ListArtifacts asks for.
type limitParseArtifactService struct {
	artifacts.Service
	limits, offsets []int
}

func (f *limitParseArtifactService) ListArtifactsPage(projectID, artifactType, owner string, limit, offset int) ([]*artifacts.Artifact, int, error) {
	f.limits = append(f.limits, limit)
	f.offsets = append(f.offsets, offset)
	return nil, 0, nil
}

// TestLimitParseArtifacts pins artifact_handlers.go's ListArtifacts: Atoi,
// then anything <= 0 or above 1000 is 1000, the default and the maximum at
// once. The offset stays 0 throughout.
func TestLimitParseArtifacts(t *testing.T) {
	cases := []limitParseCase{
		{"", 200, "", []int{1000}},
		{"limit=", 200, "", []int{1000}},
		{"limit=0", 200, "", []int{1000}},
		{"limit=-1", 200, "", []int{1000}},
		{"limit=1", 200, "", []int{1}},
		{"limit=999", 200, "", []int{999}},
		{"limit=1000", 200, "", []int{1000}}, // the default and the maximum
		{"limit=1001", 200, "", []int{1000}},
		{"limit=99999999999999999999", 200, "", []int{1000}},
		{"limit=-99999999999999999999", 200, "", []int{1000}},
		{"limit=abc", 200, "", []int{1000}},
		{"limit=5x", 200, "", []int{1000}},
		{"limit=%205", 200, "", []int{1000}},
		{"limit=%2B5", 200, "", []int{5}},
		{"limit=0x10", 200, "", []int{1000}},
	}
	queries := []string{}
	for _, tc := range cases {
		queries = append(queries, tc.query)
		t.Run("query "+tc.query, func(t *testing.T) {
			svc := &limitParseArtifactService{}
			h := newTestHandler(t, func(h *Handler) {
				h.ArtifactService = svc
				h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
					"proj-1": {ID: "proj-1", OrgID: "org-1"},
				}}
				h.MemberService = &fakeMemberService{roles: map[string]map[string]string{
					"proj-1": {"viewer": members.RoleViewer},
				}}
			})
			r := withUser(httptest.NewRequest(http.MethodGet, limitParseTarget("/api/v1/artifacts", "project_id=proj-1", tc.query), nil), "viewer")
			w := httptest.NewRecorder()
			h.ListArtifacts(w, r)
			limitParseCheck(t, tc, w, svc.limits)
			for _, offset := range svc.offsets {
				if offset != 0 {
					t.Errorf("offset = %d, want 0", offset)
				}
			}
		})
	}
	limitParseCovers(t, queries)
}

// limitParseEventRepo records the limit ListDomainEvents hands the
// repository.
type limitParseEventRepo struct {
	events.Repository
	limits []int
}

func (f *limitParseEventRepo) List(orgID, projectID, eventType, beforeID string, limit int) ([]events.Event, error) {
	f.limits = append(f.limits, limit)
	return nil, nil
}

// TestLimitParseDomainEvents pins domain_event_handlers.go's
// ListDomainEvents: Atoi, then anything <= 0 or above 500 is reset to 100
// (not capped at 500). The repository applies the same rule again
// (limit_clamp_test.go).
func TestLimitParseDomainEvents(t *testing.T) {
	cases := []limitParseCase{
		{"", 200, "", []int{100}},
		{"limit=", 200, "", []int{100}},
		{"limit=0", 200, "", []int{100}},
		{"limit=-1", 200, "", []int{100}},
		{"limit=1", 200, "", []int{1}},
		{"limit=100", 200, "", []int{100}}, // the default
		{"limit=500", 200, "", []int{500}}, // the maximum
		{"limit=501", 200, "", []int{100}},
		{"limit=99999999999999999999", 200, "", []int{100}},
		{"limit=-99999999999999999999", 200, "", []int{100}},
		{"limit=abc", 200, "", []int{100}},
		{"limit=5x", 200, "", []int{100}},
		{"limit=%205", 200, "", []int{100}},
		{"limit=%2B5", 200, "", []int{5}},
		{"limit=0x10", 200, "", []int{100}},
	}
	queries := []string{}
	for _, tc := range cases {
		queries = append(queries, tc.query)
		t.Run("query "+tc.query, func(t *testing.T) {
			repo := &limitParseEventRepo{}
			h := newTestHandler(t, func(h *Handler) { h.EventRepo = repo })
			r := limitParseAs(httptest.NewRequest(http.MethodGet, limitParseTarget("/api/v1/events", "", tc.query), nil), "u1", "org-1")
			w := httptest.NewRecorder()
			h.ListDomainEvents(w, r)
			limitParseCheck(t, tc, w, repo.limits)
		})
	}
	limitParseCovers(t, queries)
}

// limitParseSearchService records the limit GlobalSearch's keyword path
// hands the artifact service.
type limitParseSearchService struct {
	artifacts.Service
	limits []int
}

func (f *limitParseSearchService) SearchArtifacts(projectIDs []string, query string, limit int, opts artifacts.SearchOptions) ([]*artifacts.SearchHit, error) {
	f.limits = append(f.limits, limit)
	return nil, nil
}

// TestLimitParseSearch pins search_handlers.go's GlobalSearch: Atoi, then
// anything <= 0 is the default 20 and anything above 50 is capped at 50, so
// an overflowing value is the maximum here, not the default.
func TestLimitParseSearch(t *testing.T) {
	cases := []limitParseCase{
		{"", 200, "", []int{20}},
		{"limit=", 200, "", []int{20}},
		{"limit=0", 200, "", []int{20}},
		{"limit=-1", 200, "", []int{20}},
		{"limit=1", 200, "", []int{1}},
		{"limit=20", 200, "", []int{20}}, // the default
		{"limit=50", 200, "", []int{50}}, // the maximum
		{"limit=51", 200, "", []int{50}},
		{"limit=99999999999999999999", 200, "", []int{50}},
		{"limit=-99999999999999999999", 200, "", []int{20}},
		{"limit=abc", 200, "", []int{20}},
		{"limit=5x", 200, "", []int{20}},
		{"limit=%205", 200, "", []int{20}},
		{"limit=%2B5", 200, "", []int{5}},
		{"limit=0x10", 200, "", []int{20}},
	}
	queries := []string{}
	for _, tc := range cases {
		queries = append(queries, tc.query)
		t.Run("query "+tc.query, func(t *testing.T) {
			svc := &limitParseSearchService{}
			h := newTestHandler(t, func(h *Handler) {
				h.ArtifactService = svc
				h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
					"proj-1": {ID: "proj-1", OrgID: "org-1", Name: "Alpha"},
				}}
				h.OrgService = &fakeOrgService{roles: map[string]map[string]string{
					"org-1": {"admin": orgs.RoleAdmin},
				}}
				h.MemberService = &fakeMemberService{roles: map[string]map[string]string{}}
			})
			r := limitParseAs(httptest.NewRequest(http.MethodGet, limitParseTarget("/api/v1/search", "q=login", tc.query), nil), "admin", "org-1")
			w := httptest.NewRecorder()
			h.GlobalSearch(w, r)
			limitParseCheck(t, tc, w, svc.limits)
		})
	}
	limitParseCovers(t, queries)
}

// limitParseInterviewRepo records the limit the interview service hands its
// repository.
type limitParseInterviewRepo struct {
	interviews.Repository
	limits []int
}

func (f *limitParseInterviewRepo) ListSessionsByProject(projectID string, limit int) ([]*interviews.Session, error) {
	f.limits = append(f.limits, limit)
	return nil, nil
}

// limitParseInterviewService records the limit the handler hands the
// service, then lets the real service apply its default and cap.
type limitParseInterviewService struct {
	interviews.Service
	limits []int
}

func (f *limitParseInterviewService) ListProjectSessions(projectID string, limit int) ([]*interviews.Session, error) {
	f.limits = append(f.limits, limit)
	return f.Service.ListProjectSessions(projectID, limit)
}

// TestLimitParseInterviewSessions pins interview_handlers.go's
// ListProjectInterviewSessions: a non-empty value Atoi refuses is a 400,
// overflow included; any integer, zero and negatives too, reaches the
// service, which makes <= 0 its default 20 and caps at 100.
func TestLimitParseInterviewSessions(t *testing.T) {
	const notAnInteger = `{"error":"limit must be an integer"}` + "\n"
	cases := []limitParseServiceCase{
		{limitParseCase{"", 200, "", []int{0}}, []int{20}},
		{limitParseCase{"limit=", 200, "", []int{0}}, []int{20}},
		{limitParseCase{"limit=0", 200, "", []int{0}}, []int{20}},
		{limitParseCase{"limit=-1", 200, "", []int{-1}}, []int{20}},
		{limitParseCase{"limit=1", 200, "", []int{1}}, []int{1}},
		{limitParseCase{"limit=20", 200, "", []int{20}}, []int{20}},    // the service's default
		{limitParseCase{"limit=100", 200, "", []int{100}}, []int{100}}, // the service's maximum
		{limitParseCase{"limit=101", 200, "", []int{101}}, []int{100}},
		{limitParseCase{"limit=99999999999999999999", 400, notAnInteger, nil}, nil},
		{limitParseCase{"limit=-99999999999999999999", 400, notAnInteger, nil}, nil},
		{limitParseCase{"limit=abc", 400, notAnInteger, nil}, nil},
		{limitParseCase{"limit=5x", 400, notAnInteger, nil}, nil},
		{limitParseCase{"limit=%205", 400, notAnInteger, nil}, nil},
		{limitParseCase{"limit=%2B5", 200, "", []int{5}}, []int{5}},
		{limitParseCase{"limit=0x10", 400, notAnInteger, nil}, nil},
	}
	queries := []string{}
	for _, tc := range cases {
		queries = append(queries, tc.query)
		t.Run("query "+tc.query, func(t *testing.T) {
			repo := &limitParseInterviewRepo{}
			svc := &limitParseInterviewService{Service: interviews.NewDefaultService(repo)}
			h := newTestHandler(t, func(h *Handler) {
				h.InterviewService = svc
				h.ProjectService = &fakeProjectService{byID: map[string]*projects.Project{
					"proj-1": {ID: "proj-1", OrgID: "org-1"},
				}}
				h.OrgService = &fakeOrgService{roles: map[string]map[string]string{"org-1": {}}}
				h.MemberService = &fakeMemberService{roles: map[string]map[string]string{
					"proj-1": {"viewer": members.RoleViewer},
				}}
			})
			r := withUser(httptest.NewRequest(http.MethodGet, limitParseTarget("/api/v1/projects/proj-1/interview-sessions", "", tc.query), nil), "viewer")
			r = mux.SetURLVars(r, map[string]string{"id": "proj-1"})
			w := httptest.NewRecorder()
			h.ListProjectInterviewSessions(w, r)
			limitParseCheck(t, tc.limitParseCase, w, svc.limits)
			if !reflect.DeepEqual(repo.limits, tc.wantRepo) {
				t.Errorf("repository saw limits %v, want %v", repo.limits, tc.wantRepo)
			}
		})
	}
	limitParseCovers(t, queries)
}

// limitParseNotificationService records the page size ListNotifications
// asks for.
type limitParseNotificationService struct {
	notifications.Service
	limits []int
}

func (f *limitParseNotificationService) List(userID string, q notifications.ListQuery) ([]*notifications.Notification, error) {
	f.limits = append(f.limits, q.Limit)
	return nil, nil
}

func (f *limitParseNotificationService) CountUnread(userID string) (int, error) { return 0, nil }

// TestLimitParseNotifications pins notification_handlers.go's
// ListNotifications: no value is the default 50; a non-empty value Atoi
// refuses, or one below 1, is a 400; anything above 200 is capped at 200.
func TestLimitParseNotifications(t *testing.T) {
	const notPositive = `{"error":"limit must be a positive integer"}` + "\n"
	cases := []limitParseCase{
		{"", 200, "", []int{50}},
		{"limit=", 200, "", []int{50}},
		{"limit=0", 400, notPositive, nil},
		{"limit=-1", 400, notPositive, nil},
		{"limit=1", 200, "", []int{1}},
		{"limit=50", 200, "", []int{50}},   // the default
		{"limit=200", 200, "", []int{200}}, // the maximum
		{"limit=201", 200, "", []int{200}},
		{"limit=99999999999999999999", 400, notPositive, nil},
		{"limit=-99999999999999999999", 400, notPositive, nil},
		{"limit=abc", 400, notPositive, nil},
		{"limit=5x", 400, notPositive, nil},
		{"limit=%205", 400, notPositive, nil},
		{"limit=%2B5", 200, "", []int{5}},
		{"limit=0x10", 400, notPositive, nil},
	}
	queries := []string{}
	for _, tc := range cases {
		queries = append(queries, tc.query)
		t.Run("query "+tc.query, func(t *testing.T) {
			svc := &limitParseNotificationService{}
			h := newTestHandler(t, func(h *Handler) { h.NotificationService = svc })
			r := withUser(httptest.NewRequest(http.MethodGet, limitParseTarget("/api/v1/notifications", "", tc.query), nil), "u1")
			w := httptest.NewRecorder()
			h.ListNotifications(w, r)
			limitParseCheck(t, tc, w, svc.limits)
		})
	}
	limitParseCovers(t, queries)
}

// limitParseSharedProductRepo records the limit the shared-product service
// hands its repository.
type limitParseSharedProductRepo struct {
	sharedproducts.Repository
	limits []int
}

func (f *limitParseSharedProductRepo) ListVisible(opts sharedproducts.ListOptions) ([]*sharedproducts.Product, error) {
	f.limits = append(f.limits, opts.Limit)
	return nil, nil
}

// limitParseSharedProductService records the limit the handler hands the
// service, then lets the real service apply its default and cap.
type limitParseSharedProductService struct {
	sharedproducts.Service
	limits []int
}

func (f *limitParseSharedProductService) List(opts sharedproducts.ListOptions) ([]*sharedproducts.Product, error) {
	f.limits = append(f.limits, opts.Limit)
	return f.Service.List(opts)
}

// TestLimitParseSharedProducts pins shared_product_handlers.go's
// ListSharedProducts: a value Atoi refuses is dropped (0), overflow included;
// any integer, zero and negatives too, reaches the service, which makes
// <= 0 its default 200 and caps at 500.
func TestLimitParseSharedProducts(t *testing.T) {
	cases := []limitParseServiceCase{
		{limitParseCase{"", 200, "", []int{0}}, []int{200}},
		{limitParseCase{"limit=", 200, "", []int{0}}, []int{200}},
		{limitParseCase{"limit=0", 200, "", []int{0}}, []int{200}},
		{limitParseCase{"limit=-1", 200, "", []int{-1}}, []int{200}},
		{limitParseCase{"limit=1", 200, "", []int{1}}, []int{1}},
		{limitParseCase{"limit=200", 200, "", []int{200}}, []int{200}}, // the service's default
		{limitParseCase{"limit=500", 200, "", []int{500}}, []int{500}}, // the service's maximum
		{limitParseCase{"limit=501", 200, "", []int{501}}, []int{500}},
		{limitParseCase{"limit=99999999999999999999", 200, "", []int{0}}, []int{200}},
		{limitParseCase{"limit=-99999999999999999999", 200, "", []int{0}}, []int{200}},
		{limitParseCase{"limit=abc", 200, "", []int{0}}, []int{200}},
		{limitParseCase{"limit=5x", 200, "", []int{0}}, []int{200}},
		{limitParseCase{"limit=%205", 200, "", []int{0}}, []int{200}},
		{limitParseCase{"limit=%2B5", 200, "", []int{5}}, []int{5}},
		{limitParseCase{"limit=0x10", 200, "", []int{0}}, []int{200}},
	}
	queries := []string{}
	for _, tc := range cases {
		queries = append(queries, tc.query)
		t.Run("query "+tc.query, func(t *testing.T) {
			repo := &limitParseSharedProductRepo{}
			svc := &limitParseSharedProductService{Service: sharedproducts.NewDefaultService(repo, 0, 0)}
			h := newTestHandler(t, func(h *Handler) { h.SharedProductService = svc })
			r := httptest.NewRequest(http.MethodGet, limitParseTarget("/api/v1/shared-products", "", tc.query), nil)
			w := httptest.NewRecorder()
			h.ListSharedProducts(w, r)
			limitParseCheck(t, tc.limitParseCase, w, svc.limits)
			if !reflect.DeepEqual(repo.limits, tc.wantRepo) {
				t.Errorf("repository saw limits %v, want %v", repo.limits, tc.wantRepo)
			}
		})
	}
	limitParseCovers(t, queries)
}
