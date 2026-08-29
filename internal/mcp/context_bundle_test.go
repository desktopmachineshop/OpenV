package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// aiContextServer fakes the API endpoints behind the AI context tools: the
// project artifact/link lists, single-artifact fetch, and the ai-map.
func aiContextServer(t *testing.T) *httptest.Server {
	t.Helper()
	artifacts := []map[string]interface{}{
		{"id": "h1", "ref": "HDG-1", "project_id": "p1", "type": "heading", "title": "System", "body": "", "sort_order": 1},
		{"id": "r1", "ref": "REQ-1", "project_id": "p1", "parent_id": "h1", "type": "requirement", "title": "Authentication", "body": "The system shall support OIDC login.", "status": "approved", "sort_order": 1},
		{"id": "r2", "ref": "REQ-2", "project_id": "p1", "parent_id": "r1", "type": "requirement", "title": "Lockout", "body": "Lock after 5 failures.", "sort_order": 1},
		{"id": "t1", "ref": "TC-1", "project_id": "p1", "type": "test-case", "title": "Login round-trip", "body": strings.Repeat("Very long test body. ", 30), "sort_order": 2},
	}
	links := []map[string]interface{}{
		{"id": "l1", "from_id": "t1", "to_id": "r1", "type": "verifies", "suspect": true},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/artifacts" && r.URL.Query().Get("project_id") == "p1":
			json.NewEncoder(w).Encode(artifacts)
		case r.URL.Path == "/api/v1/artifacts/r1":
			json.NewEncoder(w).Encode(artifacts[1])
		case r.URL.Path == "/api/v1/links" && r.URL.Query().Get("project_id") == "p1":
			json.NewEncoder(w).Encode(links)
		case r.URL.Path == "/api/v1/projects/p1/ai-map":
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			if r.URL.Query().Get("baseline_id") == "b1" {
				w.Write([]byte("# Demo — AI map\nSource: baseline \"v1\" (b1)\n"))
				return
			}
			w.Write([]byte("# Demo — AI map\nSource: live state\n"))
		default:
			http.NotFound(w, r)
		}
	}))
}

// TestGetArtifactByRef: a stable ref plus project_id resolves to the UUID
// fetch; a bare ref without project_id errors with guidance.
func TestGetArtifactByRef(t *testing.T) {
	server := aiContextServer(t)
	defer server.Close()
	client := NewClient(server.URL, "tok")
	tool := toolByName(t, "get_artifact")

	raw, err := tool.Handler(client, map[string]interface{}{"id": "REQ-1", "project_id": "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"Authentication"`) {
		t.Errorf("by-ref fetch = %q", raw)
	}

	if _, err := tool.Handler(client, map[string]interface{}{"id": "REQ-1"}); err == nil ||
		!strings.Contains(err.Error(), "project_id") {
		t.Errorf("ref without project_id should ask for project_id, got %v", err)
	}

	// UUIDs still pass straight through.
	raw, err = tool.Handler(client, map[string]interface{}{"id": "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"Authentication"`) {
		t.Errorf("by-id fetch = %q", raw)
	}
}

// TestGetProjectMap: the tool proxies the ai-map endpoint verbatim,
// forwarding baseline_id when given.
func TestGetProjectMap(t *testing.T) {
	server := aiContextServer(t)
	defer server.Close()
	client := NewClient(server.URL, "tok")
	tool := toolByName(t, "get_project_map")

	raw, err := tool.Handler(client, map[string]interface{}{"project_id": "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "Source: live state") {
		t.Errorf("live map = %q", raw)
	}

	raw, err = tool.Handler(client, map[string]interface{}{"project_id": "p1", "baseline_id": "b1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `baseline "v1" (b1)`) {
		t.Errorf("baseline map = %q", raw)
	}
}

// TestGetContext: one call bundles body, ancestry, children, and linked
// neighbors (with truncated excerpts and the suspect mark).
func TestGetContext(t *testing.T) {
	server := aiContextServer(t)
	defer server.Close()
	client := NewClient(server.URL, "tok")
	tool := toolByName(t, "get_context")

	raw, err := tool.Handler(client, map[string]interface{}{"project_id": "p1", "id": "REQ-1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"REQ-1 Authentication (requirement, approved)",
		"Path: HDG-1 System",
		"The system shall support OIDC login.",
		"Children: REQ-2 Lockout",
		"← verifies TC-1 Login round-trip (suspect)",
		"…", // the long neighbor body is excerpted, not inlined
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("bundle missing %q\n--- got:\n%s", want, raw)
		}
	}
	if strings.Contains(raw, strings.Repeat("Very long test body. ", 30)) {
		t.Errorf("neighbor body should be truncated:\n%s", raw)
	}

	if _, err := tool.Handler(client, map[string]interface{}{"project_id": "p1", "id": "NOPE-9"}); err == nil {
		t.Error("unknown ref should error")
	}
}
