package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCreateArtifactRefRoundTrip checks that a proposal-mode agent can attach a
// temporary ref token to create_artifact: the token rides in the request body,
// and on a 202 the tool tells the agent how to reference the artifact from a
// later create_link (issue #235).
func TestCreateArtifactRefRoundTrip(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/artifacts" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]interface{}{"proposed": true, "proposal_id": "prop-1"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "tok")
	tool := toolByName(t, "create_artifact")

	out, err := tool.Handler(client, map[string]interface{}{
		"project_id": "p1",
		"type":       "test-case",
		"title":      "Verify login",
		"ref":        "tc1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["ref"] != "tc1" {
		t.Fatalf("ref not sent in body: %v", gotBody)
	}
	if !strings.Contains(out, "tc1") || !strings.Contains(out, "create_link") {
		t.Fatalf("proposal message should tell the agent to reference tc1 from create_link, got: %q", out)
	}
}

// TestCreateArtifactNoRefOmitsField ensures the ref key is absent when no token
// is supplied — a direct-write create must not carry an empty proposal-only
// field.
func TestCreateArtifactNoRefOmitsField(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "a1"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "tok")
	tool := toolByName(t, "create_artifact")
	if _, err := tool.Handler(client, map[string]interface{}{"project_id": "p1", "type": "requirement", "title": "T"}); err != nil {
		t.Fatal(err)
	}
	if _, present := gotBody["ref"]; present {
		t.Fatalf("ref should be omitted when not supplied, got: %v", gotBody)
	}
}

// TestCreateLinkRefEndpoints checks that create_link forwards a ref token in
// from_id and reports a 202 diversion as a proposal (issue #235).
func TestCreateLinkRefEndpoints(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/links" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]interface{}{"proposed": true, "proposal_id": "prop-2"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "tok")
	tool := toolByName(t, "create_link")

	out, err := tool.Handler(client, map[string]interface{}{
		"from_id": "tc1",
		"to_id":   "req-real-id",
		"type":    "verifies",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["from_id"] != "tc1" || gotBody["to_id"] != "req-real-id" {
		t.Fatalf("ref endpoints not forwarded: %v", gotBody)
	}
	if !strings.Contains(out, "Proposal created") {
		t.Fatalf("202 create_link should report a proposal, got: %q", out)
	}
}
