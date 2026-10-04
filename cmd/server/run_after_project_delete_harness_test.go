//go:build unix

package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestAMembersRunnerStopsItsRunWhenItsProjectIsDeleted deletes a project on
// the real server, in a database of its own (OPENV_TEST_DATABASE_URL;
// skipped when unset), while a member's personal runner holds one of its
// runs (#379 bugs 147 and 149). The member is no admin of the workspace and
// edits the project; the run is one a workspace key launched, so it has no
// launcher, and the member's runner claimed it as a run of a project the
// member holds a role in; the board gave it a tracking card. The delete
// leaves the run with no project, which only workspace admins see, and asks
// it to stop. The member's runner still reads the cancel on its next log
// push and reports the run cancelled, and the run ends cancelled. Before
// the fix both answered 404 "agent run not found", and the reaper failed
// the run (which an auto-retry could launch again, with no project); and
// the board hook logged an ERROR "failed to move card" for the card the
// delete took, when the delete announced the cancel.
func TestAMembersRunnerStopsItsRunWhenItsProjectIsDeleted(t *testing.T) {
	bin := serverBinary(t)
	s, db := bootServer(t, bin, nil)

	type account struct {
		id      string
		session *http.Cookie
	}
	call := func(method, path, body string, as account, bearer string, want int, into interface{}) {
		t.Helper()
		req, err := http.NewRequest(method, s.base+path, bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
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
	register := func(email, name string) account {
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
		call(http.MethodGet, "/api/v1/auth/me", "", a, "", http.StatusOK, &me)
		a.id = me.ID
		return a
	}

	owner := register(harnessEmail, "Boot Harness")
	member := register("member-"+harnessEmail, "Member")
	var workspaces struct {
		ActiveOrg string `json:"active_org"`
	}
	call(http.MethodGet, "/api/v1/orgs", "", owner, "", http.StatusOK, &workspaces)
	org := workspaces.ActiveOrg
	var workspaceKey, personalKey struct {
		Key string `json:"key"`
	}
	call(http.MethodPost, "/api/v1/orgs/"+org+"/worker-keys", `{"name":"pool"}`, owner, "", http.StatusCreated, &workspaceKey)
	var doomed struct {
		ID string `json:"id"`
	}
	call(http.MethodPost, "/api/v1/projects", `{"name":"Doomed"}`, owner, "", http.StatusCreated, &doomed)

	// The member joins the workspace, with no admin rights, and edits the
	// project.
	conn, err := sql.Open("postgres", db.url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.Exec(`INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'member')`, org, member.id); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'editor')`, doomed.ID, member.id); err != nil {
		t.Fatal(err)
	}
	call(http.MethodPost, "/api/v1/orgs/"+org+"/my-runner-key", "", member, "", http.StatusCreated, &personalKey)

	agentBody, _ := json.Marshal(map[string]any{"slug": "harness-worker", "name": "Harness Worker", "provider": "claude-code",
		"allowed_tools": []string{"get_artifact"}, "write_mode": "direct", "system_prompt": "Answer in one line."})
	call(http.MethodPost, "/api/v1/agents", string(agentBody), owner, "", http.StatusCreated, nil)
	var launched struct {
		ID         string  `json:"id"`
		LaunchedBy *string `json:"launched_by"`
	}
	call(http.MethodPost, "/api/v1/agents/harness-worker/runs", `{"project_id":"`+doomed.ID+`","prompt":"Summarise."}`,
		account{}, workspaceKey.Key, http.StatusCreated, &launched)
	if launched.LaunchedBy != nil {
		t.Fatalf("a run a workspace key launched names its launcher %q, want none", *launched.LaunchedBy)
	}
	var claim struct {
		Run struct {
			ID         string  `json:"id"`
			WorkItemID *string `json:"work_item_id"`
		} `json:"run"`
	}
	call(http.MethodPost, "/api/v1/agent-runs/claim", `{"worker_id":"member-laptop","providers":["claude-code"]}`,
		account{}, personalKey.Key, http.StatusOK, &claim)
	if claim.Run.ID != launched.ID || claim.Run.WorkItemID == nil {
		t.Fatalf("the member's runner claimed %+v, want the run %s with its tracking card", claim.Run, launched.ID)
	}

	call(http.MethodDelete, "/api/v1/projects/"+doomed.ID, "", owner, "", http.StatusNoContent, nil)

	var push struct {
		CancelRequested bool   `json:"cancel_requested"`
		Status          string `json:"status"`
	}
	call(http.MethodPost, "/api/v1/agent-runs/"+launched.ID+"/logs", `{"entries":[]}`, account{}, personalKey.Key, http.StatusOK, &push)
	if !push.CancelRequested {
		t.Errorf("the member's runner's log push after the delete: %+v, want the cancel requested", push)
	}
	var finished struct {
		Status    string  `json:"status"`
		ProjectID *string `json:"project_id"`
	}
	call(http.MethodPost, "/api/v1/agent-runs/"+launched.ID+"/finish", `{"status":"cancelled"}`, account{}, personalKey.Key,
		http.StatusOK, &finished)
	if finished.Status != "cancelled" || finished.ProjectID != nil {
		t.Errorf("the run the member's runner reported: %+v, want cancelled, with no project", finished)
	}
	// Once the run is over, the runner no longer reaches it: it was never
	// the member's to see.
	call(http.MethodPost, "/api/v1/agent-runs/"+launched.ID+"/logs", `{"entries":[]}`, account{}, personalKey.Key, http.StatusNotFound, nil)

	if out := s.output(); strings.Contains(out, "failed to move card") {
		t.Errorf("the server logged a card it could not move:\n%s", out)
	}
}
