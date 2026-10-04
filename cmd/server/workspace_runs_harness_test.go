//go:build unix

package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// TestTheWorkspaceRunsListShowsEachMemberTheRunsTheyMayOpen lists the runs
// with no project, GET /api/v1/agent-runs?project=none (the workspace Runs
// page, #379 bug 168), on the real server in a database of its own
// (OPENV_TEST_DATABASE_URL; skipped when unset), as a workspace admin, as
// a member who is no admin, and as someone of another workspace. The runs
// with no project are the admin's own, the member's, one a workspace key
// launched (as an automation's run has no launcher), and the member's run
// in a project the admin then deleted. The list must hold exactly the runs
// of the caller's workspace with no project that the caller may open by
// id: the admin every one, the member only the two they launched, and
// nobody another workspace's, not even by naming it in X-Org-ID. A
// project's run is never listed.
func TestTheWorkspaceRunsListShowsEachMemberTheRunsTheyMayOpen(t *testing.T) {
	bin := serverBinary(t)
	s, db := bootServer(t, bin, nil)

	type account struct {
		id      string
		session *http.Cookie
	}
	// call sends one request as an account (or a runner key, bearer) acting
	// in a workspace (org, through X-Org-ID; "" for its default).
	call := func(method, path, body string, as account, bearer, org string, want int, into interface{}) {
		t.Helper()
		req, err := http.NewRequest(method, s.base+path, bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if org != "" {
			req.Header.Set("X-Org-ID", org)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		} else if as.session != nil {
			req.AddCookie(as.session)
		}
		resp, err := s.client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", method, path, err, s.output())
		}
		answer, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s %s: %d %s, want %d", method, path, resp.StatusCode, strings.TrimSpace(string(answer)), want)
		}
		if into != nil {
			if err := json.Unmarshal(answer, into); err != nil {
				t.Fatalf("%s %s: %v in %s", method, path, err, answer)
			}
		}
	}
	register := func(email, name string) (account, string) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"email": email, "password": harnessPassword, "name": name})
		resp, err := s.client().Post(s.base+"/api/v1/auth/register", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		var a account
		for _, c := range resp.Cookies() {
			if c.Name == "openv_session" {
				a.session = c
			}
		}
		if resp.StatusCode != http.StatusOK || a.session == nil {
			t.Fatalf("register %s: %s and no session cookie\n%s", email, resp.Status, s.output())
		}
		var me struct {
			ID string `json:"id"`
		}
		call(http.MethodGet, "/api/v1/auth/me", "", a, "", "", http.StatusOK, &me)
		a.id = me.ID
		var workspaces struct {
			ActiveOrg string `json:"active_org"`
		}
		call(http.MethodGet, "/api/v1/orgs", "", a, "", "", http.StatusOK, &workspaces)
		return a, workspaces.ActiveOrg
	}
	type created struct {
		ID string `json:"id"`
	}
	addAgent := func(as account, org string) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"slug": "harness-worker", "name": "Harness Worker", "provider": "claude-code",
			"allowed_tools": []string{"get_artifact"}, "write_mode": "direct", "system_prompt": "Answer in one line."})
		call(http.MethodPost, "/api/v1/agents", string(body), as, "", org, http.StatusCreated, nil)
	}
	launch := func(as account, bearer, org, projectID string) string {
		t.Helper()
		body := `{"prompt":"Summarise."}`
		if projectID != "" {
			body = `{"project_id":"` + projectID + `","prompt":"Summarise."}`
		}
		var run created
		call(http.MethodPost, "/api/v1/agents/harness-worker/runs", body, as, bearer, org, http.StatusCreated, &run)
		return run.ID
	}

	admin, org := register(harnessEmail, "Workspace Admin")
	member, _ := register("member-"+harnessEmail, "Member")
	outsider, elsewhere := register("outsider-"+harnessEmail, "Outsider")
	if org == "" || elsewhere == "" || org == elsewhere {
		t.Fatalf("workspaces %q and %q, want two", org, elsewhere)
	}
	var kept, doomed created
	call(http.MethodPost, "/api/v1/projects", `{"name":"Kept"}`, admin, "", org, http.StatusCreated, &kept)
	call(http.MethodPost, "/api/v1/projects", `{"name":"Doomed"}`, admin, "", org, http.StatusCreated, &doomed)
	var workspaceKey struct {
		Key string `json:"key"`
	}
	call(http.MethodPost, "/api/v1/orgs/"+org+"/worker-keys", `{"name":"pool"}`, admin, "", "", http.StatusCreated, &workspaceKey)

	// The member joins the admin's workspace, with no admin rights, and
	// edits both projects.
	conn, err := sql.Open("postgres", db.url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, []any{org, member.id}},
		{`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'editor')`, []any{kept.ID, member.id}},
		{`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'editor')`, []any{doomed.ID, member.id}},
	} {
		if _, err := conn.Exec(stmt.query, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	addAgent(admin, org)
	addAgent(outsider, elsewhere)

	adminsRun := launch(admin, "", org, "")
	membersRun := launch(member, "", org, "")
	keysRun := launch(account{}, workspaceKey.Key, "", "")
	projectRun := launch(member, "", org, kept.ID)
	leftBehind := launch(member, "", org, doomed.ID)
	outsidersRun := launch(outsider, "", elsewhere, "")
	call(http.MethodDelete, "/api/v1/projects/"+doomed.ID, "", admin, "", org, http.StatusNoContent, nil)

	noProject := []string{adminsRun, membersRun, keysRun, leftBehind, outsidersRun}
	names := map[string]string{adminsRun: "the admin's run", membersRun: "the member's run", keysRun: "the key's run",
		projectRun: "the project's run", leftBehind: "the run the delete left behind", outsidersRun: "the outsider's run"}
	named := func(ids []string) []string {
		out := []string{}
		for _, id := range ids {
			if n, ok := names[id]; ok {
				out = append(out, n)
			} else {
				out = append(out, id)
			}
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		as     account
		org    string
		active string // the workspace the caller acts in
		want   []string
	}{
		{"the workspace admin", admin, org, org, []string{leftBehind, keysRun, membersRun, adminsRun}},
		{"the member", member, org, org, []string{leftBehind, membersRun}},
		{"the outsider", outsider, "", elsewhere, []string{outsidersRun}},
		{"the outsider naming the admin's workspace", outsider, org, elsewhere, []string{outsidersRun}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var listed []struct {
				ID        string  `json:"id"`
				OrgID     string  `json:"org_id"`
				ProjectID *string `json:"project_id"`
			}
			call(http.MethodGet, "/api/v1/agent-runs?project=none", "", tc.as, "", tc.org, http.StatusOK, &listed)
			got := []string{}
			for _, run := range listed {
				got = append(got, run.ID)
				if run.ProjectID != nil {
					t.Errorf("listed %s, of project %s, want only runs with no project", names[run.ID], *run.ProjectID)
				}
				if run.OrgID != tc.active {
					t.Errorf("listed %s, of workspace %s, want only the caller's %s", names[run.ID], run.OrgID, tc.active)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("listed %v, want %v, newest first", named(got), named(tc.want))
			}
			// Exactly the runs with no project of the caller's workspace
			// that the caller may open by id.
			opens := []string{}
			for _, id := range noProject {
				var run struct {
					OrgID string `json:"org_id"`
				}
				req, _ := http.NewRequest(http.MethodGet, s.base+"/api/v1/agent-runs/"+id, nil)
				req.AddCookie(tc.as.session)
				resp, err := s.client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				answer, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				switch resp.StatusCode {
				case http.StatusOK:
					if err := json.Unmarshal(answer, &run); err != nil {
						t.Fatal(err)
					}
					if run.OrgID == tc.active {
						opens = append(opens, id)
					}
				case http.StatusForbidden, http.StatusNotFound:
					// A member of the run's workspace is refused, anyone
					// else told there is no such run.
				default:
					t.Fatalf("GET %s: %d %s", names[id], resp.StatusCode, answer)
				}
			}
			listedSet, opensSet := map[string]bool{}, map[string]bool{}
			for _, id := range got {
				listedSet[id] = true
			}
			for _, id := range opens {
				opensSet[id] = true
			}
			if !reflect.DeepEqual(listedSet, opensSet) {
				t.Errorf("listed %v, but may open %v of the runs with no project", named(got), named(opens))
			}
		})
	}
}
