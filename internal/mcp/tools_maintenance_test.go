package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// coverageReport is the shape GET /vv/coverage returns: a summary plus one
// entry per requirement, each carrying its test cases and their results.
const coverageReport = `{
	"project_id": "p1",
	"entries": [
		{"requirement_id":"r1","title":"Fast boot","verification_method":"test","verification_status":"verified",
		 "test_case_ids":["tc1","tc2"],"latest_results":{"tc1":"pass","tc2":"pass"},"rollup":"pass"},
		{"requirement_id":"r2","title":"Cold start","verification_method":"","verification_status":"",
		 "test_case_ids":[],"latest_results":{},"rollup":"none"}
	],
	"summary": {"pass": 1, "none": 1}
}`

func TestCreateBaseline(t *testing.T) {
	t.Run("named", func(t *testing.T) {
		server, log := captureServer(t, http.StatusCreated, `{"id":"b1","name":"after MCP tools"}`)
		out, err := toolByName(t, "create_baseline").Handler(
			NewClient(server.URL, "test-token"),
			map[string]interface{}{"project_id": "p1", "name": "after MCP tools"},
		)
		if err != nil {
			t.Fatal(err)
		}
		if out != `{"id":"b1","name":"after MCP tools"}` {
			t.Errorf("output = %q, want the baseline passed through", out)
		}
		reqs := log()
		if len(reqs) != 1 {
			t.Fatalf("want 1 request, got %d", len(reqs))
		}
		if reqs[0].Method != "POST" || reqs[0].Path != "/api/v1/projects/p1/baselines" {
			t.Errorf("request = %s %s, want POST /api/v1/projects/p1/baselines", reqs[0].Method, reqs[0].Path)
		}
		if reqs[0].Body["name"] != "after MCP tools" {
			t.Errorf("body name = %v, want the given name", reqs[0].Body["name"])
		}
	})

	t.Run("unnamed leaves the server to name it", func(t *testing.T) {
		server, log := captureServer(t, http.StatusCreated, `{"id":"b2"}`)
		if _, err := toolByName(t, "create_baseline").Handler(
			NewClient(server.URL, "test-token"),
			map[string]interface{}{"project_id": "p1"},
		); err != nil {
			t.Fatal(err)
		}
		if got := log()[0].Body["name"]; got != "" {
			t.Errorf("body name = %v, want empty so the server dates one", got)
		}
	})
}

// TestRecordTestResultEvidence covers the shape the API decodes: evidence is
// a list of attachment IDs, and a string there makes it reject the whole body.
func TestRecordTestResultEvidence(t *testing.T) {
	cases := []struct {
		name string
		args map[string]interface{}
		want []interface{}
	}{
		{"omitted", map[string]interface{}{}, []interface{}{}},
		{"list", map[string]interface{}{"evidence": []interface{}{"att-1", "att-2"}}, []interface{}{"att-1", "att-2"}},
		{"bare string", map[string]interface{}{"evidence": "att-1"}, []interface{}{"att-1"}},
		{"empty string", map[string]interface{}{"evidence": ""}, []interface{}{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, log := captureServer(t, http.StatusCreated, `{"id":"res-1"}`)
			args := map[string]interface{}{"run_id": "tr1", "test_case_id": "tc1", "status": "pass"}
			for k, v := range tc.args {
				args[k] = v
			}
			if _, err := toolByName(t, "record_test_result").Handler(NewClient(server.URL, "test-token"), args); err != nil {
				t.Fatal(err)
			}
			body := log()[0].Body
			got, ok := body["evidence"].([]interface{})
			if !ok {
				t.Fatalf("evidence = %#v, want a JSON list", body["evidence"])
			}
			if len(got) != len(tc.want) {
				t.Fatalf("evidence = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("evidence[%d] = %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestGetVVCoverage(t *testing.T) {
	tool := toolByName(t, "get_vv_coverage")

	t.Run("lean by default", func(t *testing.T) {
		server, log := captureServer(t, http.StatusOK, coverageReport)
		out, err := tool.Handler(NewClient(server.URL, "test-token"), map[string]interface{}{"project_id": "p1"})
		if err != nil {
			t.Fatal(err)
		}
		if got := log()[0].Path; got != "/api/v1/projects/p1/vv/coverage" {
			t.Errorf("path = %s, want /api/v1/projects/p1/vv/coverage", got)
		}

		var report struct {
			ProjectID string         `json:"project_id"`
			Summary   map[string]int `json:"summary"`
			Entries   []map[string]interface{}
		}
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatalf("bad JSON %q: %v", out, err)
		}
		if report.ProjectID != "p1" || report.Summary["pass"] != 1 || report.Summary["none"] != 1 {
			t.Errorf("summary lost: %s", out)
		}
		if len(report.Entries) != 2 {
			t.Fatalf("entries = %d, want 2", len(report.Entries))
		}
		if report.Entries[0]["title"] != "Fast boot" || report.Entries[0]["rollup"] != "pass" {
			t.Errorf("unexpected first entry: %v", report.Entries[0])
		}
		for _, key := range []string{"test_case_ids", "latest_results"} {
			if _, ok := report.Entries[0][key]; ok {
				t.Errorf("summary view must drop %q", key)
			}
		}
	})

	t.Run("detail passes the full report through", func(t *testing.T) {
		server, _ := captureServer(t, http.StatusOK, coverageReport)
		for _, detail := range []interface{}{true, "true"} {
			out, err := tool.Handler(NewClient(server.URL, "test-token"),
				map[string]interface{}{"project_id": "p1", "detail": detail})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, "latest_results") {
				t.Errorf("detail=%v dropped the full report: %s", detail, out)
			}
		}
	})

	t.Run("malformed response errors", func(t *testing.T) {
		server, _ := captureServer(t, http.StatusOK, `[1,2,3]`)
		_, err := tool.Handler(NewClient(server.URL, "test-token"), map[string]interface{}{"project_id": "p1"})
		if err == nil || !strings.Contains(err.Error(), "unexpected coverage response") {
			t.Fatalf("err = %v, want unexpected-response error", err)
		}
	})
}

func TestCloseTestRun(t *testing.T) {
	tool := toolByName(t, "close_test_run")

	t.Run("completed", func(t *testing.T) {
		server, log := captureServer(t, http.StatusOK, `{"id":"tr1","status":"completed"}`)
		if _, err := tool.Handler(NewClient(server.URL, "test-token"),
			map[string]interface{}{"run_id": "tr1", "status": "Completed"}); err != nil {
			t.Fatal(err)
		}
		req := log()[0]
		if req.Method != "PUT" || req.Path != "/api/v1/test-runs/tr1" {
			t.Errorf("request = %s %s, want PUT /api/v1/test-runs/tr1", req.Method, req.Path)
		}
		if req.Body["status"] != "completed" {
			t.Errorf("status = %v, want completed", req.Body["status"])
		}
	})

	t.Run("other statuses never reach the API", func(t *testing.T) {
		server, log := captureServer(t, http.StatusOK, `{}`)
		_, err := tool.Handler(NewClient(server.URL, "test-token"),
			map[string]interface{}{"run_id": "tr1", "status": "in-progress"})
		if err == nil || !strings.Contains(err.Error(), "completed or aborted") {
			t.Fatalf("err = %v, want a rejected-status error", err)
		}
		if len(log()) != 0 {
			t.Error("invalid status still hit the API")
		}
	})
}

func TestGetVVGaps(t *testing.T) {
	gaps := `{"requirements_without_method":["r2"],"requirements_without_test_case":["r2"],` +
		`"requirements_failing":[],"orphan_test_cases":[],"needs_without_requirement":[],"hazards_unmitigated":[]}`
	server, log := captureServer(t, http.StatusOK, gaps)
	out, err := toolByName(t, "get_vv_gaps").Handler(
		NewClient(server.URL, "test-token"), map[string]interface{}{"project_id": "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if out != gaps {
		t.Errorf("output = %q, want the gap report passed through", out)
	}
	if got := log()[0].Path; got != "/api/v1/projects/p1/vv/gaps" {
		t.Errorf("path = %s, want /api/v1/projects/p1/vv/gaps", got)
	}
}

// TestMaintenanceLoopIsCovered guards the tools the requirements-maintenance
// loop runs on: an agent that cannot capture a baseline or read coverage
// falls back on raw API calls, which is what the tool surface exists to avoid.
func TestMaintenanceLoopIsCovered(t *testing.T) {
	have := map[string]bool{}
	for _, tool := range Tools() {
		have[tool.Name] = true
	}
	for _, name := range []string{
		"get_project_map", "get_artifact", "create_artifact", "update_artifact",
		"create_link", "delete_link", "create_test_run", "record_test_result",
		"close_test_run", "get_vv_coverage", "get_vv_gaps", "create_baseline", "list_baselines",
	} {
		if !have[name] {
			t.Errorf("maintenance tool %q missing from the tool table", name)
		}
	}
}
