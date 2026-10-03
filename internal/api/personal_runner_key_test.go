package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
)

// TestPersonalRunnerKeyActsWithItsHoldersRole is the regression test for a
// member's personal runner key that approved agent proposals and wrote
// artifacts in any project of its workspace, whatever its holder's role
// there: requireProjectRole passed every worker key of the project's
// workspace and never looked at whose key it was, so a member who only
// views a project approved a proposal there, or created an artifact, with
// the key their own session is refused (OpenV REQ-16, REQ-79, REQ-21 with
// TC-7). A personal key is its holder acting: an editor's key and a
// workspace admin's key pass, a viewer's is refused with the project's 403,
// and a roleless member's is answered as its holder's session is, as for
// what no row has (I3: the proposal's or the project's 404), and neither
// reaches anything. A workspace runner key has no holder and keeps REQ-42's
// workspace-wide editor rights.
func TestPersonalRunnerKeyActsWithItsHoldersRole(t *testing.T) {
	const (
		org     = "o1" // copyHandler's p1 is in o1
		project = "p1"
		denied  = `403 {"error":"you do not have access to this project"}`
	)
	keys := []struct {
		name     string
		holder   string // "" for a workspace runner key
		wantPass bool
		roleless bool // the holder has no role in the project: its 404
	}{
		{"a viewer's personal key", "val", false, false},
		{"a roleless member's personal key", "mo", false, true},
		{"an editor's personal key", "eve", true, false},
		{"a workspace admin's personal key", "ada", true, false},
		{"a workspace runner key", "", true, false},
	}
	roles := func() (*fakeOrgService, *fakeMemberService) {
		return &fakeOrgService{roles: map[string]map[string]string{
				org: {"val": orgs.RoleMember, "mo": orgs.RoleMember, "eve": orgs.RoleMember, "ada": orgs.RoleAdmin},
			}}, &fakeMemberService{roles: map[string]map[string]string{
				project: {"val": members.RoleViewer, "eve": members.RoleEditor},
			}}
	}
	keyCtx := func(r *http.Request, holder string) *http.Request {
		ctx := context.WithValue(r.Context(), ctxWorkerOrg, org)
		if holder != "" {
			ctx = context.WithValue(ctx, ctxWorkerUser, holder)
		}
		return r.WithContext(ctx)
	}
	check := func(t *testing.T, w *httptest.ResponseRecorder, wantPass bool, okCode int, reached int, refusal string) {
		t.Helper()
		if wantPass {
			if w.Code != okCode || reached != 1 {
				t.Fatalf("status = %d, reached %d: want %d, reached once (body %q)", w.Code, reached, okCode, w.Body.String())
			}
			return
		}
		if got := strconv.Itoa(w.Code) + " " + strings.TrimSpace(w.Body.String()); got != refusal {
			t.Fatalf("answer = %s: want %s", got, refusal)
		}
		if reached != 0 {
			t.Fatalf("a refused key reached the service %d times, want none", reached)
		}
	}

	for _, key := range keys {
		for _, approve := range []bool{true, false} {
			action, review := "approve", (*Handler).ApproveProposal
			if !approve {
				action, review = "reject", (*Handler).RejectProposal
			}
			t.Run(action+" by "+key.name, func(t *testing.T) {
				svc := &fakeProposalService{byID: map[string]*proposals.Proposal{"pr-1": {ID: "pr-1", RunID: "run-1",
					ProjectID: project, Op: proposals.OpCreateArtifact, Status: proposals.StatusPending}}}
				h := proposalTestHandler(t, svc, map[string]*projects.Project{project: {ID: project, OrgID: org}}, nil)
				h.orgService, h.memberService = roles()
				r := httptest.NewRequest(http.MethodPost, "/api/v1/proposals/pr-1/"+action, strings.NewReader(`{}`))
				r = mux.SetURLVars(keyCtx(r, key.holder), map[string]string{"id": "pr-1"})
				w := httptest.NewRecorder()
				review(h, w, r)
				refusal := denied
				if key.roleless {
					refusal = `404 {"error":"proposal not found"}`
				}
				check(t, w, key.wantPass, http.StatusOK, len(svc.approved)+len(svc.rejected), refusal)
			})
		}
		t.Run("create an artifact with "+key.name, func(t *testing.T) {
			h, _ := copyHandler(t)
			h.orgService, h.memberService = roles()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts",
				strings.NewReader(`{"project_id":"p1","type":"requirement","title":"New","body":"x"}`))
			w := httptest.NewRecorder()
			h.CreateArtifact(w, keyCtx(r, key.holder))
			refusal := denied
			if key.roleless {
				refusal = `404 {"error":"project not found"}`
			}
			check(t, w, key.wantPass, http.StatusCreated, len(h.artifactService.(*copyArtifactFake).created), refusal)
		})
	}
}

// fakeRepoConnService answers every project with one connection, carrying
// the local path paths holds for the user a read is made for, and records
// each read as the project, or "<project> as <user>" for a user's.
type fakeRepoConnService struct {
	repoconns.Service
	paths map[string]string // userID -> local path of rc-1
	reads []string
}

func (f *fakeRepoConnService) conns(projectID string) []*repoconns.RepoConnection {
	return []*repoconns.RepoConnection{{ID: "rc-1", ProjectID: projectID, Name: "firmware",
		RemoteURL: "git@example.com:acme/firmware.git", DefaultBranch: "main"}}
}

func (f *fakeRepoConnService) ListByProject(projectID string) ([]*repoconns.RepoConnection, error) {
	f.reads = append(f.reads, projectID)
	return f.conns(projectID), nil
}

func (f *fakeRepoConnService) ListByProjectForUser(projectID, userID string) ([]*repoconns.RepoConnection, error) {
	f.reads = append(f.reads, projectID+" as "+userID)
	list := f.conns(projectID)
	for _, c := range list {
		c.MyLocalPath = f.paths[userID]
	}
	return list, nil
}

// personalKeyReadsHandler has two projects of workspace o1: a viewer of p1
// (val), a member with no role in either (mo) and the workspace's admin
// (ada), with local paths for val and ada.
func personalKeyReadsHandler(t *testing.T) (*Handler, *fakeRepoConnService) {
	h := proposalTestHandler(t, nil, map[string]*projects.Project{
		"p1": {ID: "p1", OrgID: "o1", Name: "Pump"},
		"p2": {ID: "p2", OrgID: "o1", Name: "Valve"},
	}, map[string]map[string]string{"p1": {"val": members.RoleViewer}})
	h.orgService = &fakeOrgService{roles: map[string]map[string]string{
		"o1": {"val": orgs.RoleMember, "mo": orgs.RoleMember, "ada": orgs.RoleAdmin}}}
	conns := &fakeRepoConnService{paths: map[string]string{"val": "/home/val/firmware", "ada": "/home/ada/firmware"}}
	h.repoConnService = conns
	return h, conns
}

// TestPersonalRunnerKeyReadsOnlyWhereItsHolderCan is the regression test for
// a member's personal runner key that read every project of its workspace:
// requireProjectRole passed any personal key a viewer's read, whatever its
// holder's role, and the project list gave it every project of the
// workspace, so a member with no role in a project read its content with the
// key their own session is refused (OpenV REQ-16, REQ-42). A personal key is
// its holder acting, reads included: a viewer's key reads the project it
// views, a workspace admin's every project, and a member with no role in a
// project gets there what their session gets, the 404 of a project no row
// has (I3), and does not see it in the list. A workspace runner key has no
// holder and keeps REQ-42's workspace-wide rights.
func TestPersonalRunnerKeyReadsOnlyWhereItsHolderCan(t *testing.T) {
	const denied = `{"error":"project not found"}`
	keyCtx := func(r *http.Request, holder string) *http.Request {
		ctx := context.WithValue(r.Context(), ctxWorkerOrg, "o1")
		if holder != "" {
			ctx = context.WithValue(ctx, ctxWorkerUser, holder)
		}
		return r.WithContext(ctx)
	}
	keys := []struct {
		name     string
		holder   string // "" for a workspace runner key
		reads    string // the repository read the key's read of p1 makes, "" when refused
		projects string // the projects the key lists
	}{
		{"a viewer's personal key", "val", "p1 as val", "p1"},
		{"a roleless member's personal key", "mo", "", ""},
		{"a workspace admin's personal key", "ada", "p1 as ada", "p1 p2"},
		{"a workspace runner key", "", "p1", "p1 p2"},
	}
	for _, key := range keys {
		t.Run("read p1's repository connections with "+key.name, func(t *testing.T) {
			h, conns := personalKeyReadsHandler(t)
			r := httptest.NewRequest(http.MethodGet, "/api/v1/projects/p1/repo-connections", nil)
			r = mux.SetURLVars(keyCtx(r, key.holder), map[string]string{"id": "p1"})
			w := httptest.NewRecorder()
			h.ListRepoConnections(w, r)
			if key.reads == "" {
				if w.Code != http.StatusNotFound || strings.TrimSpace(w.Body.String()) != denied {
					t.Fatalf("status = %d, body %q: want 404 %s, as the holder's own session gets", w.Code, w.Body.String(), denied)
				}
				if len(conns.reads) != 0 {
					t.Fatalf("a refused key read %v, want nothing", conns.reads)
				}
				return
			}
			if w.Code != http.StatusOK || strings.Join(conns.reads, ", ") != key.reads {
				t.Fatalf("status = %d, reads %v: want 200 and the read %q (body %q)", w.Code, conns.reads, key.reads, w.Body.String())
			}
		})
		t.Run("list the workspace's projects with "+key.name, func(t *testing.T) {
			h, _ := personalKeyReadsHandler(t)
			w := httptest.NewRecorder()
			h.ListProjects(w, keyCtx(httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil), key.holder))
			var got []projects.Project
			if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil {
				t.Fatalf("status = %d, decode %v (body %q): want 200 and a list", w.Code, err, w.Body.String())
			}
			var ids []string
			for _, p := range got {
				ids = append(ids, p.ID)
			}
			sort.Strings(ids)
			if strings.Join(ids, " ") != key.projects {
				t.Fatalf("listed %v, want [%s]: a personal key lists what its holder's session lists", ids, key.projects)
			}
		})
	}
}

// TestRunTokenReadsItsProjectsRepoConnections pins the read the runner makes
// of a claimed run's repository connections, now with the run's own token
// rather than its worker key, which for a member's personal runner reads only
// the projects its member can (REQ-16). The token reads its own project's
// connections, read only, with the local paths of the member whose personal
// runner key claimed the run, since a personal runner works on its member's
// own checkout (REQ-86), and none for a run a workspace key holds, and
// nothing of any other project.
func TestRunTokenReadsItsProjectsRepoConnections(t *testing.T) {
	p1, p2, val := "p1", "p2", "val"
	cases := []struct {
		name     string
		run      *agentruns.Run
		read     string // the repository read the request makes, "" when refused
		wantPath string
		refusal  string
	}{
		{"a run in p1 the viewer's personal runner claimed: the viewer's path",
			&agentruns.Run{ID: "run-1", OrgID: "o1", ProjectID: &p1, ClaimedBy: &val}, "p1 as val", "/home/val/firmware", ""},
		{"a run in p1 a workspace key holds: no member, so no path",
			&agentruns.Run{ID: "run-2", OrgID: "o1", ProjectID: &p1}, "p1", "", ""},
		{"a run in p2: another project's connections answer as a project no row has",
			&agentruns.Run{ID: "run-3", OrgID: "o1", ProjectID: &p2, ClaimedBy: &val}, "", "",
			`{"error":"project not found"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, conns := personalKeyReadsHandler(t)
			r := httptest.NewRequest(http.MethodGet, "/api/v1/projects/p1/repo-connections", nil)
			r = mux.SetURLVars(r.WithContext(context.WithValue(r.Context(), ctxRun, tc.run)), map[string]string{"id": "p1"})
			w := httptest.NewRecorder()
			h.ListRepoConnections(w, r)
			if tc.read == "" {
				if w.Code != http.StatusNotFound || strings.TrimSpace(w.Body.String()) != tc.refusal || len(conns.reads) != 0 {
					t.Fatalf("status = %d, body %q, reads %v: want 404 %s and nothing read", w.Code, w.Body.String(), conns.reads, tc.refusal)
				}
				return
			}
			var got []repoconns.RepoConnection
			if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil || len(got) != 1 {
				t.Fatalf("status = %d, decode %v (body %q): want 200 and p1's connection", w.Code, err, w.Body.String())
			}
			if strings.Join(conns.reads, ", ") != tc.read || got[0].MyLocalPath != tc.wantPath {
				t.Fatalf("reads %v, my_local_path %q: want the read %q and the path %q", conns.reads, got[0].MyLocalPath, tc.read, tc.wantPath)
			}
		})
	}
	t.Run("the token connects no repository: a run is at most an editor, the route an owner's", func(t *testing.T) {
		h, _ := personalKeyReadsHandler(t)
		r := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p1/repo-connections",
			strings.NewReader(`{"name":"x","remote_url":"https://example.com/x.git"}`))
		run := &agentruns.Run{ID: "run-1", OrgID: "o1", ProjectID: &p1, ClaimedBy: &val}
		r = mux.SetURLVars(r.WithContext(context.WithValue(r.Context(), ctxRun, run)), map[string]string{"id": "p1"})
		w := httptest.NewRecorder()
		h.CreateRepoConnection(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, body %q: want 403", w.Code, w.Body.String())
		}
	})
}
