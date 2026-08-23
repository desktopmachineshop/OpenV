package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func toolByName(t *testing.T, name string) Tool {
	t.Helper()
	for _, tool := range Tools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %s not found", name)
	return Tool{}
}

func TestListWorkItems(t *testing.T) {
	items := []map[string]interface{}{
		{"id": "wi-1", "project_id": "p1", "title": "Draft requirements", "description": "long text", "column": "todo", "sort_order": 1, "assignee_type": "agent", "assignee_id": "agent-1", "artifact_ids": []string{"a1"}},
		{"id": "wi-2", "project_id": "p1", "title": "Review coverage", "description": "long text", "column": "todo", "sort_order": 2, "assignee_type": "agent", "assignee_id": "agent-2", "artifact_ids": []string{}},
		{"id": "wi-3", "project_id": "p1", "title": "Ship it", "description": "long text", "column": "done", "sort_order": 1, "assignee_type": "user", "artifact_ids": []string{}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/projects/p1/work-items" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("unexpected auth header %q", got)
		}
		json.NewEncoder(w).Encode(items)
	}))
	defer server.Close()

	client := NewClient(server.URL, "tok")
	tool := toolByName(t, "list_work_items")

	decode := func(t *testing.T, raw string) []map[string]interface{} {
		t.Helper()
		var out []map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("bad JSON %q: %v", raw, err)
		}
		return out
	}

	t.Run("all cards", func(t *testing.T) {
		raw, err := tool.Handler(client, map[string]interface{}{"project_id": "p1"})
		if err != nil {
			t.Fatal(err)
		}
		cards := decode(t, raw)
		if len(cards) != 3 {
			t.Fatalf("want 3 cards, got %d", len(cards))
		}
		if _, ok := cards[0]["description"]; ok {
			t.Error("list should not include card descriptions")
		}
		if cards[0]["id"] != "wi-1" || cards[0]["column"] != "todo" {
			t.Errorf("unexpected first card: %v", cards[0])
		}
	})

	t.Run("column filter", func(t *testing.T) {
		raw, err := tool.Handler(client, map[string]interface{}{"project_id": "p1", "column": "todo"})
		if err != nil {
			t.Fatal(err)
		}
		cards := decode(t, raw)
		if len(cards) != 2 {
			t.Fatalf("want 2 todo cards, got %d", len(cards))
		}
	})

	t.Run("assignee filter", func(t *testing.T) {
		raw, err := tool.Handler(client, map[string]interface{}{"project_id": "p1", "column": "todo", "assignee_id": "agent-2"})
		if err != nil {
			t.Fatal(err)
		}
		cards := decode(t, raw)
		if len(cards) != 1 || cards[0]["id"] != "wi-2" {
			t.Fatalf("want only wi-2, got %s", raw)
		}
	})

	t.Run("empty result is a JSON array", func(t *testing.T) {
		raw, err := tool.Handler(client, map[string]interface{}{"project_id": "p1", "column": "review"})
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(raw) != "[]" {
			t.Fatalf("want [], got %q", raw)
		}
	})

	t.Run("unknown column errors", func(t *testing.T) {
		_, err := tool.Handler(client, map[string]interface{}{"project_id": "p1", "column": "doing"})
		if err == nil || !strings.Contains(err.Error(), "valid columns") {
			t.Fatalf("want unknown-column error, got %v", err)
		}
	})
}
