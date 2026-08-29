package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// captured records one request the fake API server received.
type captured struct {
	Method string
	Path   string
	Query  url.Values
	Body   map[string]interface{}
}

// captureServer returns a fake API that records every request and replies
// with the same status/response for all of them, plus the request log.
func captureServer(t *testing.T, status int, response string) (*httptest.Server, func() []captured) {
	t.Helper()
	var mu sync.Mutex
	var log []captured
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization header = %q, want Bearer test-token", got)
		}
		c := captured{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query()}
		if r.Body != nil {
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				c.Body = body
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("%s %s: content type = %q, want application/json", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
				}
			}
		}
		mu.Lock()
		log = append(log, c)
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	return server, func() []captured {
		mu.Lock()
		defer mu.Unlock()
		out := make([]captured, len(log))
		copy(out, log)
		return out
	}
}

// TestToolRequests drives every plain request-shaped tool through a fake API
// and checks the HTTP method, path, query, and body it produces, plus how the
// response is surfaced (passthrough, projection, or proposal-mode note).
func TestToolRequests(t *testing.T) {
	artifactList := `[
		{"id":"a1","type":"requirement","title":"Fast Boot","body":"boots in 2s","parent_id":null,"sort_order":1,"extra":"x"},
		{"id":"a2","type":"risk","title":"Overheat","body":"THERMAL runaway","parent_id":"a1","sort_order":2,"extra":"y"}
	]`
	linkList := `[
		{"id":"l1","from_id":"a1","to_id":"a2","type":"mitigates"},
		{"id":"l2","from_id":"a3","to_id":"a4","type":"refines"}
	]`

	cases := []struct {
		tool       string
		args       map[string]interface{}
		status     int
		response   string
		wantMethod string
		wantPath   string
		wantQuery  url.Values
		wantBody   map[string]interface{} // exact-match keys; nil = no body expected
		wantOut    string                 // exact expected handler output; "" = skip
		checkOut   func(t *testing.T, out string)
	}{
		{
			tool:       "list_projects",
			args:       map[string]interface{}{},
			status:     200,
			response:   `[{"id":"p1"}]`,
			wantMethod: "GET",
			wantPath:   "/api/v1/projects",
			wantOut:    `[{"id":"p1"}]`,
		},
		{
			tool:       "list_artifacts",
			args:       map[string]interface{}{"project_id": "p1"},
			status:     200,
			response:   artifactList,
			wantMethod: "GET",
			wantPath:   "/api/v1/artifacts",
			wantQuery:  url.Values{"project_id": {"p1"}},
		},
		{
			tool:       "list_artifacts",
			args:       map[string]interface{}{"project_id": "p1", "type": "risk"},
			status:     200,
			response:   `[]`,
			wantMethod: "GET",
			wantPath:   "/api/v1/artifacts",
			wantQuery:  url.Values{"project_id": {"p1"}, "type": {"risk"}},
		},
		{
			tool:       "get_artifact",
			args:       map[string]interface{}{"id": "a1"},
			status:     200,
			response:   `{"id":"a1"}`,
			wantMethod: "GET",
			wantPath:   "/api/v1/artifacts/a1",
			wantOut:    `{"id":"a1"}`,
		},
		{
			tool:       "get_project_tree",
			args:       map[string]interface{}{"project_id": "p1"},
			status:     200,
			response:   artifactList,
			wantMethod: "GET",
			wantPath:   "/api/v1/artifacts",
			wantQuery:  url.Values{"project_id": {"p1"}},
			checkOut: func(t *testing.T, out string) {
				var tree []map[string]interface{}
				if err := json.Unmarshal([]byte(out), &tree); err != nil {
					t.Fatalf("bad tree JSON: %v", err)
				}
				if len(tree) != 2 {
					t.Fatalf("tree size = %d, want 2", len(tree))
				}
				if _, ok := tree[0]["body"]; ok {
					t.Error("tree must not include bodies")
				}
				if _, ok := tree[0]["extra"]; ok {
					t.Error("tree must project only the known fields")
				}
				if tree[1]["parent_id"] != "a1" || tree[0]["title"] != "Fast Boot" {
					t.Errorf("unexpected tree: %v", tree)
				}
			},
		},
		{
			tool:       "search_artifacts",
			args:       map[string]interface{}{"project_id": "p1", "query": "thermal"},
			status:     200,
			response:   artifactList,
			wantMethod: "GET",
			wantPath:   "/api/v1/artifacts",
			checkOut: func(t *testing.T, out string) {
				var matches []map[string]interface{}
				if err := json.Unmarshal([]byte(out), &matches); err != nil {
					t.Fatalf("bad JSON: %v", err)
				}
				// "THERMAL" appears in a2's body only; match is case-insensitive.
				if len(matches) != 1 || matches[0]["id"] != "a2" {
					t.Fatalf("matches = %v, want just a2", matches)
				}
				if _, ok := matches[0]["body"]; ok {
					t.Error("search results must not include bodies")
				}
			},
		},
		{
			tool:       "search_artifacts",
			args:       map[string]interface{}{"project_id": "p1", "query": "boot"},
			status:     200,
			response:   artifactList,
			wantMethod: "GET",
			wantPath:   "/api/v1/artifacts",
			checkOut: func(t *testing.T, out string) {
				// "boot" matches a1's title and body, case-insensitively.
				var matches []map[string]interface{}
				_ = json.Unmarshal([]byte(out), &matches)
				if len(matches) != 1 || matches[0]["id"] != "a1" {
					t.Fatalf("matches = %v, want just a1", matches)
				}
			},
		},
		{
			tool: "create_artifact",
			args: map[string]interface{}{
				"project_id": "p1", "type": "requirement", "title": "T", "body": "B",
				"parent_id": "par", "attributes": map[string]interface{}{"k": "v"},
			},
			status:     201,
			response:   `{"id":"new"}`,
			wantMethod: "POST",
			wantPath:   "/api/v1/artifacts",
			wantBody: map[string]interface{}{
				"project_id": "p1", "type": "requirement", "title": "T", "body": "B",
				"parent_id": "par", "attributes": map[string]interface{}{"k": "v"},
			},
			wantOut: `{"id":"new"}`,
		},
		{
			tool:       "create_artifact",
			args:       map[string]interface{}{"project_id": "p1", "type": "requirement", "title": "T"},
			status:     200,
			response:   `{"id":"new"}`,
			wantMethod: "POST",
			wantPath:   "/api/v1/artifacts",
			checkOut: func(t *testing.T, out string) {
				if strings.Contains(out, "Proposal created") {
					t.Errorf("non-202 response must not be labeled a proposal: %q", out)
				}
			},
		},
		{
			tool:       "create_artifact",
			args:       map[string]interface{}{"project_id": "p1", "type": "requirement", "title": "T"},
			status:     202,
			response:   `{"proposal_id":"prop-1"}`,
			wantMethod: "POST",
			wantPath:   "/api/v1/artifacts",
			wantOut:    `Proposal created (pending human review, not yet applied): {"proposal_id":"prop-1"}`,
		},
		{
			tool:       "update_artifact",
			args:       map[string]interface{}{"id": "a1", "type": "requirement", "title": "T2", "body": "B2"},
			status:     200,
			response:   `{"id":"a1"}`,
			wantMethod: "PUT",
			wantPath:   "/api/v1/artifacts/a1",
			wantBody:   map[string]interface{}{"type": "requirement", "title": "T2", "body": "B2"},
			wantOut:    `{"id":"a1"}`,
		},
		{
			tool:       "update_artifact",
			args:       map[string]interface{}{"id": "a1", "parent_id": "par-1"},
			status:     200,
			response:   `{"id":"a1"}`,
			wantMethod: "PUT",
			wantPath:   "/api/v1/artifacts/a1",
			wantBody:   map[string]interface{}{"parent_id": "par-1"},
			wantOut:    `{"id":"a1"}`,
		},
		{
			tool:       "update_artifact",
			args:       map[string]interface{}{"id": "a1", "type": "requirement", "title": "T2"},
			status:     202,
			response:   `{"proposal_id":"prop-2"}`,
			wantMethod: "PUT",
			wantPath:   "/api/v1/artifacts/a1",
			wantOut:    `Proposal created (pending human review, not yet applied): {"proposal_id":"prop-2"}`,
		},
		{
			tool:       "create_link",
			args:       map[string]interface{}{"from_id": "a1", "to_id": "a2", "type": "mitigates"},
			status:     201,
			response:   `{"id":"l1"}`,
			wantMethod: "POST",
			wantPath:   "/api/v1/links",
			wantBody:   map[string]interface{}{"from_id": "a1", "to_id": "a2", "type": "mitigates"},
			wantOut:    `{"id":"l1"}`,
		},
		{
			tool:       "delete_link",
			args:       map[string]interface{}{"id": "l1"},
			status:     200,
			response:   ``,
			wantMethod: "DELETE",
			wantPath:   "/api/v1/links/l1",
			wantOut:    "link deleted",
		},
		{
			tool:       "list_links_for_artifact",
			args:       map[string]interface{}{"artifact_id": "a1", "project_id": "p1"},
			status:     200,
			response:   linkList,
			wantMethod: "GET",
			wantPath:   "/api/v1/links",
			wantQuery:  url.Values{"project_id": {"p1"}},
			checkOut: func(t *testing.T, out string) {
				var links []map[string]interface{}
				_ = json.Unmarshal([]byte(out), &links)
				if len(links) != 1 || links[0]["id"] != "l1" {
					t.Fatalf("links = %v, want just l1 (touches a1)", links)
				}
			},
		},
		{
			tool:       "add_comment",
			args:       map[string]interface{}{"artifact_id": "a1", "message": "looks good"},
			status:     201,
			response:   `{"id":"c1"}`,
			wantMethod: "POST",
			wantPath:   "/api/v1/chatter",
			wantBody:   map[string]interface{}{"artifact_id": "a1", "message": "looks good"},
			wantOut:    `{"id":"c1"}`,
		},
		{
			tool:       "list_baselines",
			args:       map[string]interface{}{"project_id": "p1"},
			status:     200,
			response:   `[]`,
			wantMethod: "GET",
			wantPath:   "/api/v1/projects/p1/baselines",
			wantOut:    `[]`,
		},
		{
			tool:       "get_baseline",
			args:       map[string]interface{}{"id": "b1"},
			status:     200,
			response:   `{"id":"b1"}`,
			wantMethod: "GET",
			wantPath:   "/api/v1/baselines/b1",
			wantOut:    `{"id":"b1"}`,
		},
		{
			tool:       "create_test_run",
			args:       map[string]interface{}{"project_id": "p1", "name": "Smoke", "description": "d"},
			status:     201,
			response:   `{"id":"tr1"}`,
			wantMethod: "POST",
			wantPath:   "/api/v1/projects/p1/test-runs",
			wantBody:   map[string]interface{}{"name": "Smoke", "description": "d"},
			wantOut:    `{"id":"tr1"}`,
		},
		{
			tool:       "record_test_result",
			args:       map[string]interface{}{"run_id": "tr1", "test_case_id": "tc1", "status": "pass", "notes": "n", "evidence": "e"},
			status:     201,
			response:   `{"id":"res1"}`,
			wantMethod: "POST",
			wantPath:   "/api/v1/test-runs/tr1/results",
			wantBody:   map[string]interface{}{"test_case_id": "tc1", "status": "pass", "notes": "n", "evidence": "e"},
			wantOut:    `{"id":"res1"}`,
		},
		{
			tool:       "get_work_item",
			args:       map[string]interface{}{"id": "w1"},
			status:     200,
			response:   `{"id":"w1"}`,
			wantMethod: "GET",
			wantPath:   "/api/v1/work-items/w1",
			wantOut:    `{"id":"w1"}`,
		},
		{
			tool:       "get_work_item_history",
			args:       map[string]interface{}{"id": "w1"},
			status:     200,
			response:   `{"id":"w1","activity":[{"kind":"comment","content":"hi"}]}`,
			wantMethod: "GET",
			wantPath:   "/api/v1/work-items/w1",
			wantOut:    `[{"content":"hi","kind":"comment"}]`,
		},
		{
			tool:       "update_work_item",
			args:       map[string]interface{}{"id": "w1", "comment": "progress"},
			status:     201,
			response:   `{"id":"act1"}`,
			wantMethod: "POST",
			wantPath:   "/api/v1/work-items/w1/comments",
			wantBody:   map[string]interface{}{"comment": "progress"},
			wantOut:    `{"id":"act1"}`,
		},
		{
			tool:       "record_candidate_need",
			args:       map[string]interface{}{"project_id": "p1", "need": "Fast setup", "rationale": "saves time", "quote": "it took hours"},
			status:     201,
			response:   `{"id":"n1"}`,
			wantMethod: "POST",
			wantPath:   "/api/v1/artifacts",
			wantBody: map[string]interface{}{
				"project_id": "p1", "type": "user-need", "title": "Fast setup",
				"body": "Rationale: saves time\n\nQuote: \"it took hours\"",
			},
			wantOut: `{"id":"n1"}`,
		},
		{
			tool:       "record_candidate_need",
			args:       map[string]interface{}{"project_id": "p1", "need": "Fast setup"},
			status:     202,
			response:   `{"proposal_id":"prop-3"}`,
			wantMethod: "POST",
			wantPath:   "/api/v1/artifacts",
			wantBody:   map[string]interface{}{"body": ""},
			wantOut:    `Candidate need recorded as a proposal (pending review): {"proposal_id":"prop-3"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.tool+"/"+tc.wantMethod+tc.wantPath, func(t *testing.T) {
			server, requests := captureServer(t, tc.status, tc.response)
			client := NewClient(server.URL, "test-token")
			tool := toolByName(t, tc.tool)

			out, err := tool.Handler(client, tc.args)
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}

			log := requests()
			if len(log) != 1 {
				t.Fatalf("requests = %d, want 1", len(log))
			}
			req := log[0]
			if req.Method != tc.wantMethod || req.Path != tc.wantPath {
				t.Errorf("request = %s %s, want %s %s", req.Method, req.Path, tc.wantMethod, tc.wantPath)
			}
			for key, want := range tc.wantQuery {
				if got := req.Query[key]; len(got) == 0 || got[0] != want[0] {
					t.Errorf("query %s = %v, want %v", key, got, want)
				}
			}
			for key, want := range tc.wantBody {
				gotJSON, _ := json.Marshal(req.Body[key])
				wantJSON, _ := json.Marshal(want)
				if string(gotJSON) != string(wantJSON) {
					t.Errorf("body %s = %s, want %s", key, gotJSON, wantJSON)
				}
			}
			if tc.wantOut != "" && out != tc.wantOut {
				t.Errorf("output = %q, want %q", out, tc.wantOut)
			}
			if tc.checkOut != nil {
				tc.checkOut(t, out)
			}
		})
	}
}

// TestCreateArtifactOmitsEmptyOptionalFields ensures optional args don't leak
// into the request body when absent.
func TestCreateArtifactOmitsEmptyOptionalFields(t *testing.T) {
	server, requests := captureServer(t, 201, `{"id":"new"}`)
	client := NewClient(server.URL, "test-token")
	tool := toolByName(t, "create_artifact")

	if _, err := tool.Handler(client, map[string]interface{}{"project_id": "p1", "type": "req", "title": "T"}); err != nil {
		t.Fatal(err)
	}
	body := requests()[0].Body
	if _, ok := body["parent_id"]; ok {
		t.Error("empty parent_id must be omitted")
	}
	if _, ok := body["attributes"]; ok {
		t.Error("absent attributes must be omitted")
	}
}

// TestUpdateArtifactOmittedVsEmptyFields locks in the issue-#170 contract:
// an omitted optional arg must be ABSENT from the PUT payload (the API
// treats absent as "no change"), while an explicitly empty string must be
// sent as "" (an intentional clear). The old behavior — always sending
// body:"" — silently wiped artifact bodies on every body-less update.
func TestUpdateArtifactOmittedVsEmptyFields(t *testing.T) {
	t.Run("omitted body, title and type stay out of the payload", func(t *testing.T) {
		server, requests := captureServer(t, 200, `{"id":"a1"}`)
		client := NewClient(server.URL, "test-token")
		tool := toolByName(t, "update_artifact")

		if _, err := tool.Handler(client, map[string]interface{}{"id": "a1", "title": "T2"}); err != nil {
			t.Fatal(err)
		}
		body := requests()[0].Body
		if _, ok := body["body"]; ok {
			t.Error("omitted body arg must not appear in the payload (it would wipe the artifact body)")
		}
		if _, ok := body["type"]; ok {
			t.Error("omitted type arg must not appear in the payload")
		}
		if got := body["title"]; got != "T2" {
			t.Errorf("title = %v, want T2", got)
		}
		if _, ok := body["attributes"]; ok {
			t.Error("absent attributes must be omitted")
		}
	})

	t.Run("omitted parent_id stays out of the payload", func(t *testing.T) {
		// Issue #172: the server reads an ABSENT parent_id as "keep the
		// current parent" and an explicit null as "move to root", so a
		// parent-less update must not include the key at all.
		server, requests := captureServer(t, 200, `{"id":"a1"}`)
		client := NewClient(server.URL, "test-token")
		tool := toolByName(t, "update_artifact")

		if _, err := tool.Handler(client, map[string]interface{}{"id": "a1", "title": "T2"}); err != nil {
			t.Fatal(err)
		}
		if _, ok := requests()[0].Body["parent_id"]; ok {
			t.Error("omitted parent_id arg must not appear in the payload (it would reparent the artifact)")
		}
	})

	t.Run("empty parent_id is sent as JSON null to move to top level", func(t *testing.T) {
		server, requests := captureServer(t, 200, `{"id":"a1"}`)
		client := NewClient(server.URL, "test-token")
		tool := toolByName(t, "update_artifact")

		if _, err := tool.Handler(client, map[string]interface{}{"id": "a1", "parent_id": ""}); err != nil {
			t.Fatal(err)
		}
		body := requests()[0].Body
		got, ok := body["parent_id"]
		if !ok {
			t.Fatal("explicit empty parent_id must be present in the payload as null")
		}
		if got != nil {
			t.Errorf("parent_id = %v, want JSON null", got)
		}
	})

	t.Run("explicit null parent_id arg also maps to JSON null", func(t *testing.T) {
		server, requests := captureServer(t, 200, `{"id":"a1"}`)
		client := NewClient(server.URL, "test-token")
		tool := toolByName(t, "update_artifact")

		if _, err := tool.Handler(client, map[string]interface{}{"id": "a1", "parent_id": nil}); err != nil {
			t.Fatal(err)
		}
		body := requests()[0].Body
		got, ok := body["parent_id"]
		if !ok || got != nil {
			t.Errorf("parent_id = (%v, present=%v), want present JSON null", got, ok)
		}
	})

	t.Run("explicit empty body is sent as an intentional clear", func(t *testing.T) {
		server, requests := captureServer(t, 200, `{"id":"a1"}`)
		client := NewClient(server.URL, "test-token")
		tool := toolByName(t, "update_artifact")

		if _, err := tool.Handler(client, map[string]interface{}{"id": "a1", "body": ""}); err != nil {
			t.Fatal(err)
		}
		body := requests()[0].Body
		got, ok := body["body"]
		if !ok {
			t.Fatal("explicit empty body must be present in the payload")
		}
		if got != "" {
			t.Errorf("body = %v, want empty string", got)
		}
	})
}

// TestToolsMapAPIErrors verifies every direct-request tool surfaces an API
// error status as a Go error containing the status and server detail.
func TestToolsMapAPIErrors(t *testing.T) {
	// Arguments valid for every tool so no handler fails before the request.
	genericArgs := map[string]interface{}{
		"project_id": "p1", "id": "x1", "query": "q", "artifact_id": "a1",
		"from_id": "a1", "to_id": "a2", "type": "requirement", "title": "T",
		"message": "m", "run_id": "tr1", "test_case_id": "tc1", "status": "pass",
		"name": "N", "comment": "c", "need": "n",
	}
	server, _ := captureServer(t, http.StatusForbidden, `{"error":"forbidden"}`)
	client := NewClient(server.URL, "test-token")

	for _, tool := range Tools() {
		if tool.Name == "delegate_to_agent" {
			continue // polling tool, covered separately
		}
		t.Run(tool.Name, func(t *testing.T) {
			_, err := tool.Handler(client, genericArgs)
			if err == nil {
				t.Fatalf("%s swallowed the API error", tool.Name)
			}
			if !strings.Contains(err.Error(), "API 403") || !strings.Contains(err.Error(), "forbidden") {
				t.Errorf("%s error = %v, want API 403 with server detail", tool.Name, err)
			}
		})
	}
}

// TestListToolsRejectMalformedListResponses covers the decodeList error path.
func TestListToolsRejectMalformedListResponses(t *testing.T) {
	for _, name := range []string{"get_project_tree", "search_artifacts", "list_links_for_artifact", "list_work_items"} {
		t.Run(name, func(t *testing.T) {
			server, _ := captureServer(t, 200, `{"not":"a list"}`)
			client := NewClient(server.URL, "test-token")
			_, err := toolByName(t, name).Handler(client, map[string]interface{}{
				"project_id": "p1", "query": "q", "artifact_id": "a1",
			})
			if err == nil || !strings.Contains(err.Error(), "unexpected") {
				t.Fatalf("err = %v, want unexpected-response error", err)
			}
		})
	}
}

func TestGetWorkItemHistoryRejectsMalformedObject(t *testing.T) {
	server, _ := captureServer(t, 200, `[1,2,3]`)
	client := NewClient(server.URL, "test-token")
	_, err := toolByName(t, "get_work_item_history").Handler(client, map[string]interface{}{"id": "w1"})
	if err == nil || !strings.Contains(err.Error(), "unexpected work item response") {
		t.Fatalf("err = %v, want unexpected-response error", err)
	}
}

// --- delegate_to_agent ---

// delegateServer fakes the delegate launch + status endpoints.
func delegateServer(t *testing.T, statusResponses ...string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	poll := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/agent-runs/delegate":
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["role_label"] != "builder" || body["prompt"] != "do it" {
				t.Errorf("delegate body = %v", body)
			}
			_, _ = w.Write([]byte(`{"run_id":"dr1","status":"queued"}`))
		case r.Method == "GET" && r.URL.Path == "/api/v1/agent-runs/delegate/dr1":
			mu.Lock()
			i := poll
			if poll < len(statusResponses)-1 {
				poll++
			}
			mu.Unlock()
			_, _ = w.Write([]byte(statusResponses[i]))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDelegateToAgentSucceeded(t *testing.T) {
	t.Parallel() // the poll loop sleeps 5s before its first status check
	server := delegateServer(t, `{"run_id":"dr1","status":"succeeded","final_text":"built it"}`)
	out, err := toolByName(t, "delegate_to_agent").Handler(NewClient(server.URL, "test-token"),
		map[string]interface{}{"role_label": "builder", "prompt": "do it"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if out != "built it" {
		t.Errorf("out = %q, want the delegate's final text", out)
	}
}

func TestDelegateToAgentAwaitingApprovalAddsNote(t *testing.T) {
	t.Parallel()
	server := delegateServer(t, `{"run_id":"dr1","status":"awaiting_approval","final_text":"proposed changes"}`)
	out, err := toolByName(t, "delegate_to_agent").Handler(NewClient(server.URL, "test-token"),
		map[string]interface{}{"role_label": "builder", "prompt": "do it"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.HasPrefix(out, "proposed changes") || !strings.Contains(out, "awaiting human approval") {
		t.Errorf("out = %q, want final text plus approval note", out)
	}
}

func TestDelegateToAgentFailure(t *testing.T) {
	t.Parallel()
	server := delegateServer(t, `{"run_id":"dr1","status":"failed","error":"crashed hard"}`)
	_, err := toolByName(t, "delegate_to_agent").Handler(NewClient(server.URL, "test-token"),
		map[string]interface{}{"role_label": "builder", "prompt": "do it"})
	if err == nil || !strings.Contains(err.Error(), "crashed hard") || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("err = %v, want failure with detail", err)
	}
}

func TestDelegateToAgentPollsThroughRunningStates(t *testing.T) {
	t.Parallel()
	server := delegateServer(t,
		`{"run_id":"dr1","status":"running"}`,
		`{"run_id":"dr1","status":"succeeded","final_text":"done after wait"}`,
	)
	out, err := toolByName(t, "delegate_to_agent").Handler(NewClient(server.URL, "test-token"),
		map[string]interface{}{"role_label": "builder", "prompt": "do it"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if out != "done after wait" {
		t.Errorf("out = %q", out)
	}
}

func TestDelegateToAgentBadLaunchResponseFailsFast(t *testing.T) {
	server, _ := captureServer(t, 200, `not json`)
	client := NewClient(server.URL, "test-token")
	_, err := toolByName(t, "delegate_to_agent").Handler(client, map[string]interface{}{"role_label": "builder", "prompt": "do it"})
	if err == nil || !strings.Contains(err.Error(), "unexpected delegate response") {
		t.Fatalf("err = %v, want unexpected delegate response", err)
	}
}

func TestDelegateToAgentLaunchAPIError(t *testing.T) {
	server, _ := captureServer(t, 500, `{"error":"boom"}`)
	client := NewClient(server.URL, "test-token")
	_, err := toolByName(t, "delegate_to_agent").Handler(client, map[string]interface{}{"role_label": "builder", "prompt": "do it"})
	if err == nil || !strings.Contains(err.Error(), "API 500") {
		t.Fatalf("err = %v, want API 500", err)
	}
}

// TestToolTableIntegrity guards the tool table shape every host relies on.
func TestToolTableIntegrity(t *testing.T) {
	tools := Tools()
	if len(tools) < 21 {
		t.Fatalf("tool table shrank: %d tools", len(tools))
	}
	seen := map[string]bool{}
	for _, tool := range tools {
		if tool.Name == "" || tool.Description == "" || tool.Handler == nil {
			t.Errorf("tool %+v missing name, description, or handler", tool.Name)
		}
		if seen[tool.Name] {
			t.Errorf("duplicate tool name %s", tool.Name)
		}
		seen[tool.Name] = true
		schema := tool.InputSchema
		if schema["type"] != "object" {
			t.Errorf("%s: schema type = %v", tool.Name, schema["type"])
		}
		props, _ := schema["properties"].(map[string]interface{})
		required, _ := schema["required"].([]string)
		if required == nil {
			t.Errorf("%s: required must be a non-nil list for MCP clients", tool.Name)
		}
		for _, req := range required {
			if _, ok := props[req]; !ok {
				t.Errorf("%s: required arg %q missing from properties", tool.Name, req)
			}
		}
	}
}
