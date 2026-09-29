package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakeCrewGraphs serves fixed crew graphs and records CloneTeam calls; the
// other teams.Service methods are unused by the clone and launch handlers.
type fakeCrewGraphs struct {
	teams.Service
	graphs map[string]*teams.TeamGraph
	clones []*string // the project_id of each CloneTeam call
}

func (f *fakeCrewGraphs) GetTeam(id string) (*teams.TeamGraph, error) {
	if g, ok := f.graphs[id]; ok {
		return g, nil
	}
	return nil, errors.New("team not found")
}

func (f *fakeCrewGraphs) CloneTeam(id, newName string, projectID *string) (*teams.Team, error) {
	f.clones = append(f.clones, projectID)
	return &teams.Team{ID: "crew-copy", OrgID: f.graphs[id].Team.OrgID, Name: newName, ProjectID: projectID}, nil
}

// crewPinFixture is workspace W (org-w) with projects P and R, the
// workspace-wide crew "crew-w", the P-pinned crew "crew-p", and two crews of
// W whose pin no longer names a project of W: "crew-gone" (a deleted
// project) and "crew-other" (X's project Q), each with an entry node; and
// workspace X (org-x) with project Q. "admin" is an admin of both
// workspaces, "editor" a plain member of W who edits P and views R, and
// "qeditor" a plain member of X who edits Q.
func crewPinFixture() (*Handler, *fakeCrewGraphs, *fakeRunService) {
	pin := "proj-p"
	graph := func(id string, projectID *string) *teams.TeamGraph {
		entry := id + "-entry"
		return &teams.TeamGraph{
			Team:  &teams.Team{ID: id, OrgID: "org-w", Name: id, ProjectID: projectID, EntryNodeID: &entry},
			Nodes: []*teams.Node{{ID: entry, TeamID: id, NodeType: teams.NodeAgent, AgentID: "agent-lead"}},
		}
	}
	gone, elsewhere := "proj-gone", "proj-q"
	crews := &fakeCrewGraphs{graphs: map[string]*teams.TeamGraph{
		"crew-w":     graph("crew-w", nil),
		"crew-p":     graph("crew-p", &pin),
		"crew-gone":  graph("crew-gone", &gone),
		"crew-other": graph("crew-other", &elsewhere),
	}}
	runs := &fakeRunService{}
	h := NewHandler(HandlerDeps{
		TeamService: crews,
		RunService:  runs,
		ProjectService: &fakeProjectService{byID: map[string]*projects.Project{
			"proj-p": {ID: "proj-p", OrgID: "org-w"},
			"proj-r": {ID: "proj-r", OrgID: "org-w"},
			"proj-q": {ID: "proj-q", OrgID: "org-x"},
		}},
		OrgService: &fakeOrgService{roles: map[string]map[string]string{
			"org-w": {"admin": orgs.RoleAdmin, "editor": orgs.RoleMember},
			"org-x": {"admin": orgs.RoleAdmin, "qeditor": orgs.RoleMember},
		}},
		MemberService: &fakeMemberService{roles: map[string]map[string]string{
			"proj-p": {"editor": members.RoleEditor},
			"proj-r": {"editor": members.RoleViewer},
			"proj-q": {"qeditor": members.RoleEditor},
		}},
	})
	return h, crews, runs
}

func crewReq(userID, crewID, path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/crews/"+crewID+path, strings.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxUser, &users.User{ID: userID})
	ctx = context.WithValue(ctx, ctxActiveOrg, "org-w")
	return mux.SetURLVars(r.WithContext(ctx), map[string]string{"id": crewID})
}

// TestCloneTeamChecksWhereTheCopyLands pins that a clone's project_id is
// checked as CreateTeam checks a new crew's: a project of the source's
// workspace that the caller edits, or, with none, a workspace admin. A clone
// pinned to a project no one has used to be stored as sent, after which the
// crew-write guard refused the workspace's own admin every write to it; one
// pinned to another workspace's project handed its writes to that project's
// editors.
func TestCloneTeamChecksWhereTheCopyLands(t *testing.T) {
	cases := []struct {
		name, user, crew, body string
		want                   int
		wantErr                string
	}{
		{"pinned to a project no one has", "admin", "crew-w", `{"name":"Copy","project_id":"proj-none"}`,
			http.StatusBadRequest, "project not found"},
		{"pinned to another workspace's project", "admin", "crew-w", `{"name":"Copy","project_id":"proj-q"}`,
			http.StatusBadRequest, "project does not belong to this workspace"},
		{"pinned to a project of the workspace", "admin", "crew-w", `{"name":"Copy","project_id":"proj-p"}`,
			http.StatusCreated, ""},
		{"workspace-wide by a workspace admin", "admin", "crew-w", `{"name":"Copy"}`, http.StatusCreated, ""},
		{"a pinned crew pinned again by the project's editor", "editor", "crew-p",
			`{"name":"Copy","project_id":"proj-p"}`, http.StatusCreated, ""},
		{"pinned to a project the caller only views", "editor", "crew-p", `{"name":"Copy","project_id":"proj-r"}`,
			http.StatusForbidden, "you do not have access to this project"},
		{"workspace-wide by a plain member", "editor", "crew-p", `{"name":"Copy"}`,
			http.StatusForbidden, "workspace admin access required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, crews, _ := crewPinFixture()
			w := httptest.NewRecorder()
			h.CloneTeam(w, crewReq(tc.user, tc.crew, "/clone", tc.body))
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.want, w.Body.String())
			}
			if tc.want != http.StatusCreated {
				if !strings.Contains(w.Body.String(), tc.wantErr) {
					t.Fatalf("body = %q, want the error %q", w.Body.String(), tc.wantErr)
				}
				if len(crews.clones) != 0 {
					t.Fatalf("a refused clone reached the service: %d calls", len(crews.clones))
				}
				return
			}
			if len(crews.clones) != 1 {
				t.Fatalf("CloneTeam calls = %d, want 1", len(crews.clones))
			}
		})
	}
}

// TestLaunchTeamRunScopesAPinnedCrewToItsProject pins that a crew pinned to
// a project and launched with no project runs in that project, where the
// board gives it its tracking card; it used to run with no project at all.
// A launch that names a project, and a workspace-wide crew's launch with
// none, are as they were.
func TestLaunchTeamRunScopesAPinnedCrewToItsProject(t *testing.T) {
	cases := []struct {
		name, user, crew, body, wantProject string
	}{
		{"the pinned crew with no project", "editor", "crew-p", `{"prompt":"Plan."}`, "proj-p"},
		{"the pinned crew in its own project", "editor", "crew-p", `{"project_id":"proj-p","prompt":"Plan."}`, "proj-p"},
		{"the workspace-wide crew in a project", "admin", "crew-w", `{"project_id":"proj-r","prompt":"Plan."}`, "proj-r"},
		{"the workspace-wide crew with no project", "admin", "crew-w", `{"prompt":"Plan."}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _, runs := crewPinFixture()
			w := httptest.NewRecorder()
			h.LaunchTeamRun(w, crewReq(tc.user, tc.crew, "/runs", tc.body))
			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201 (body %q)", w.Code, w.Body.String())
			}
			if len(runs.launchReqs) != 1 {
				t.Fatalf("Launch calls = %d, want 1", len(runs.launchReqs))
			}
			got := ""
			if p := runs.launchReqs[0].ProjectID; p != nil {
				got = *p
			}
			if got != tc.wantProject {
				t.Fatalf("the run's project = %q, want %q", got, tc.wantProject)
			}
			if org := runs.launchReqs[0].OrgID; org != "org-w" {
				t.Fatalf("the run's workspace = %q, want org-w", org)
			}
		})
	}
}

// TestAStalePinDoesNotHandACrewAway pins that a crew's pin counts only while
// it names a project of the crew's own workspace. A crew pinned to a deleted
// project used to refuse its own workspace's admins every write, and one
// pinned to another workspace's project was written and launched by that
// project's editors, its runs placed in that project; a launch naming
// another workspace's project was taken too. Such a pin now counts as none,
// and a crew's run stays in the crew's workspace.
func TestAStalePinDoesNotHandACrewAway(t *testing.T) {
	writes := []struct {
		name, user, crew string
		want             bool
	}{
		{"a deleted project's crew, by its workspace admin", "admin", "crew-gone", true},
		{"a deleted project's crew, by a plain member", "editor", "crew-gone", false},
		{"a crew pinned elsewhere, by its workspace admin", "admin", "crew-other", true},
		{"a crew pinned elsewhere, by that project's editor", "qeditor", "crew-other", false},
		{"a crew pinned in its workspace, by the project's editor", "editor", "crew-p", true},
	}
	for _, tc := range writes {
		t.Run("write: "+tc.name, func(t *testing.T) {
			h, crews, _ := crewPinFixture()
			w := httptest.NewRecorder()
			got := h.requireTeamWrite(w, crewReq(tc.user, tc.crew, "", ""), crews.graphs[tc.crew].Team)
			if got != tc.want {
				t.Fatalf("requireTeamWrite = %v, want %v (status %d, body %q)", got, tc.want, w.Code, w.Body.String())
			}
		})
	}
	launches := []struct {
		name, user, crew, body string
		want                   int
		wantProject            string
	}{
		{"a crew pinned elsewhere, by that project's editor", "qeditor", "crew-other", `{"prompt":"Plan."}`,
			http.StatusForbidden, ""},
		{"a crew pinned elsewhere, by its workspace admin", "admin", "crew-other", `{"prompt":"Plan."}`,
			http.StatusCreated, ""},
		{"a deleted project's crew, by its workspace admin", "admin", "crew-gone", `{"prompt":"Plan."}`,
			http.StatusCreated, ""},
		{"a workspace-wide crew into another workspace's project", "admin", "crew-w",
			`{"project_id":"proj-q","prompt":"Plan."}`, http.StatusBadRequest, ""},
	}
	for _, tc := range launches {
		t.Run("launch: "+tc.name, func(t *testing.T) {
			h, _, runs := crewPinFixture()
			w := httptest.NewRecorder()
			h.LaunchTeamRun(w, crewReq(tc.user, tc.crew, "/runs", tc.body))
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.want, w.Body.String())
			}
			if tc.want != http.StatusCreated {
				if len(runs.launchReqs) != 0 {
					t.Fatalf("a refused launch reached the service: %d calls", len(runs.launchReqs))
				}
				return
			}
			if p := runs.launchReqs[0].ProjectID; p != nil && *p != tc.wantProject {
				t.Fatalf("the run's project = %q, want %q", *p, tc.wantProject)
			}
			if org := runs.launchReqs[0].OrgID; org != "org-w" {
				t.Fatalf("the run's workspace = %q, want org-w", org)
			}
		})
	}
}
