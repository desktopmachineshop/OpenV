package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// toolsGoldenPath pins the MCP tool surface (invariant I11). Agent
// definitions name these tools in their allowlists (REQ-91), vendor CLIs call
// them by name with arguments shaped by these schemas, and the seeded
// interviewer's allowlist is stored output of ReadOnlyToolNames
// (internal/seeds), so every field below is observable by something already
// deployed (REQ-143).
var toolsGoldenPath = filepath.Join("testdata", "tools.json")

// toolsGolden is the shape of testdata/tools.json.
type toolsGolden struct {
	// Tools is Tools() in table order, which is also the order tools/list
	// serves them in.
	Tools []toolGolden `json:"tools"`
	// ReadOnlyToolNames is ReadOnlyToolNames() verbatim: the seeded
	// interviewer is granted this list, so its order is pinned with its
	// members.
	ReadOnlyToolNames []string `json:"read_only_tool_names"`
}

type toolGolden struct {
	Name        string          `json:"name"`
	ReadOnly    bool            `json:"read_only"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	// Calls are the REST requests the handler makes, in order, each as its
	// line in internal/api/testdata/routes.txt.
	Calls []string `json:"calls"`
	// Requests are the same requests exactly as sent: method, path with the
	// ids the recording drove and the query, and the JSON body when there is
	// one. They pin which id went where, the query parameters and the body
	// fields, which a route line cannot show.
	Requests []string `json:"requests"`
}

// TestMCPToolsGolden pins every tool in Tools() order: its name, read-only
// flag, description, input schema and the REST routes its handler calls, with
// each request's query and body, plus the order of ReadOnlyToolNames. Each tool runs against a stub API with
// arguments that reach every call it makes, and every call has to be a route
// in the API's inventory. A renamed or reordered tool, a changed schema or
// description, a moved read-only flag or a retargeted path fails here.
func TestMCPToolsGolden(t *testing.T) {
	t.Parallel() // delegate_to_agent sleeps 5s before its first status poll
	doc := buildToolsGolden(t)
	// The recording also settles what TestReadOnlyToolsExcludeWriters can
	// only infer from names: a tool granted to least-privilege agents
	// (REQ-91) issues nothing but GETs.
	for _, tool := range doc.Tools {
		for _, call := range tool.Calls {
			if tool.ReadOnly && !strings.HasPrefix(call, http.MethodGet+" ") {
				t.Errorf("read-only tool %q calls %s", tool.Name, call)
			}
		}
	}
	checkGolden(t, toolsGoldenPath, encodeGoldenCompact(t, doc), "TestMCPToolsGolden")
}

// Ids the recording drives the tools with. A path segment equal to one of
// them is an id, so it has to sit where the route template has a parameter.
const (
	goldenProjectID       = "11111111-1111-4111-8111-111111111111"
	goldenArtifactID      = "22222222-2222-4222-8222-222222222222"
	goldenOtherArtifactID = "33333333-3333-4333-8333-333333333333"
	goldenLinkID          = "44444444-4444-4444-8444-444444444444"
	goldenBaselineID      = "55555555-5555-4555-8555-555555555555"
	goldenTestRunID       = "66666666-6666-4666-8666-666666666666"
	goldenTestCaseID      = "77777777-7777-4777-8777-777777777777"
	goldenWorkItemID      = "88888888-8888-4888-8888-888888888888"
	goldenDelegateRunID   = "99999999-9999-4999-8999-999999999999"
	goldenAttachmentID    = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	goldenAgentID         = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

// goldenArtifactRef is goldenArtifactID's stable ref. A ref is not an id:
// every tool resolves it to a UUID before it reaches a path, so a path segment
// carrying it matches no route and fails the recording.
const goldenArtifactRef = "REQ-7"

func toolGoldenIDs() map[string]bool {
	ids := map[string]bool{}
	for _, id := range []string{
		goldenProjectID, goldenArtifactID, goldenOtherArtifactID,
		goldenLinkID, goldenBaselineID, goldenTestRunID, goldenTestCaseID,
		goldenWorkItemID, goldenDelegateRunID, goldenAttachmentID, goldenAgentID,
	} {
		ids[id] = true
	}
	return ids
}

// toolGoldenArgs drives each tool through every REST call it makes: every
// optional argument is set, and a tool that resolves stable refs gets a ref,
// so the lookup call is recorded too.
func toolGoldenArgs() map[string]map[string]interface{} {
	return map[string]map[string]interface{}{
		"list_projects":           {},
		"list_artifacts":          {"project_id": goldenProjectID, "type": "requirement", "owner": "QA"},
		"get_artifact":            {"id": goldenArtifactRef, "project_id": goldenProjectID},
		"get_project_map":         {"project_id": goldenProjectID, "baseline_id": goldenBaselineID},
		"get_context":             {"project_id": goldenProjectID, "id": goldenArtifactRef},
		"get_project_tree":        {"project_id": goldenProjectID},
		"search_artifacts":        {"project_id": goldenProjectID, "query": goldenArtifactRef},
		"create_artifact":         {"project_id": goldenProjectID, "type": "requirement", "title": "Seal integrity", "body": "Holds 6 bar.", "parent_id": goldenArtifactID, "attributes": map[string]interface{}{"priority": "must"}, "ref": "t1"},
		"update_artifact":         {"id": goldenArtifactID, "type": "requirement", "title": "Seal integrity", "body": "", "parent_id": "", "attributes": map[string]interface{}{"priority": "should"}},
		"create_link":             {"from_id": goldenArtifactID, "to_id": goldenOtherArtifactID, "type": "verifies"},
		"delete_link":             {"id": goldenLinkID},
		"confirm_link":            {"id": goldenLinkID},
		"list_links_for_artifact": {"artifact_id": goldenArtifactID, "project_id": goldenProjectID},
		"add_comment":             {"artifact_id": goldenArtifactID, "message": "Checked on the rig."},
		"list_baselines":          {"project_id": goldenProjectID},
		"get_baseline":            {"id": goldenBaselineID},
		"create_baseline":         {"project_id": goldenProjectID, "name": "B1"},
		"start_project_review":    {"project_id": goldenProjectID, "types": []interface{}{"requirement"}},
		"create_test_run":         {"project_id": goldenProjectID, "name": "Bench run", "description": "Rig 2"},
		"record_test_result":      {"run_id": goldenTestRunID, "test_case_id": goldenTestCaseID, "status": "pass", "notes": "ok", "evidence": []interface{}{goldenAttachmentID}},
		"close_test_run":          {"run_id": goldenTestRunID, "status": "completed"},
		"get_quality_rules":       {"project_id": goldenProjectID},
		"get_quality_findings":    {"artifact_id": goldenArtifactRef, "project_id": goldenProjectID},
		"get_vv_coverage":         {"project_id": goldenProjectID, "detail": true},
		"get_vv_gaps":             {"project_id": goldenProjectID},
		"list_work_items":         {"project_id": goldenProjectID, "column": "todo", "assignee_id": goldenAgentID},
		"get_work_item":           {"id": goldenWorkItemID},
		"get_work_item_history":   {"id": goldenWorkItemID},
		"update_work_item":        {"id": goldenWorkItemID, "comment": "Started."},
		"record_candidate_need":   {"project_id": goldenProjectID, "need": "Operators see seal wear early", "rationale": "Downtime", "quote": "We only find out when it leaks."},
		"delegate_to_agent":       {"role_label": "builder", "prompt": "Draft the test cases."},
	}
}

// toolStubBodies answers the routes whose reply a handler parses. Every other
// route answers {}, which the handlers that pass a body through accept.
func toolStubBodies() map[string]string {
	return map[string]string{
		"GET /api/v1/artifacts": `[{"id":"` + goldenArtifactID + `","ref":"` + goldenArtifactRef +
			`","type":"requirement","title":"Seal integrity","body":"Holds 6 bar.","parent_id":null,"sort_order":1}]`,
		"GET /api/v1/links": `[{"id":"` + goldenLinkID + `","from_id":"` + goldenArtifactID +
			`","to_id":"` + goldenOtherArtifactID + `","type":"verifies"}]`,
		"GET /api/v1/projects/{id}/work-items":    `[]`,
		"GET /api/v1/work-items/{id}":             `{"id":"` + goldenWorkItemID + `","activity":[]}`,
		"GET /api/v1/projects/{id}/quality-rules": `{"effective":{},"summary":"shall"}`,
		"POST /api/v1/agent-runs/delegate":        `{"run_id":"` + goldenDelegateRunID + `","status":"queued"}`,
		"GET /api/v1/agent-runs/delegate/{id}":    `{"run_id":"` + goldenDelegateRunID + `","status":"succeeded","final_text":"done"}`,
	}
}

// toolStubAPI is the OpenV API the recording runs against. Each tool
// authenticates with its own token, so the stub files every request under
// the tool that made it, as the inventory line it resolves to and as sent.
type toolStubAPI struct {
	routes []string
	ids    map[string]bool
	bodies map[string]string

	mu     sync.Mutex
	calls  map[string][]string // tool name -> calls in order
	reqs   map[string][]string // tool name -> requests as sent, in order
	misses map[string][]string // tool name -> requests no inventory route matches
}

const toolTokenPrefix = "golden-"

func (s *toolStubAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tool := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "+toolTokenPrefix)
	route, ok := matchRoute(s.routes, r.Method, r.URL.Path, func(seg string) bool { return s.ids[seg] })
	sent := r.Method + " " + r.URL.RequestURI()
	if body, _ := io.ReadAll(r.Body); len(body) > 0 {
		sent += " " + string(body)
	}
	s.mu.Lock()
	s.reqs[tool] = append(s.reqs[tool], sent)
	if ok {
		s.calls[tool] = append(s.calls[tool], route)
	} else {
		unresolved := r.Method + " " + r.URL.Path
		s.calls[tool] = append(s.calls[tool], unresolved)
		s.misses[tool] = append(s.misses[tool], unresolved)
	}
	s.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"no such route"}`)
		return
	}
	body, found := s.bodies[route]
	if !found {
		body = "{}"
	}
	_, _ = io.WriteString(w, body)
}

// buildToolsGolden runs every tool against the stub API and assembles the
// golden document.
func buildToolsGolden(t *testing.T) toolsGolden {
	t.Helper()
	api := &toolStubAPI{
		routes: loadRouteInventory(t),
		ids:    toolGoldenIDs(),
		bodies: toolStubBodies(),
		calls:  map[string][]string{},
		reqs:   map[string][]string{},
		misses: map[string][]string{},
	}
	srv := httptest.NewServer(api)
	defer srv.Close()

	tools := Tools()
	args := toolGoldenArgs()
	errs := make([]error, len(tools))
	var wg sync.WaitGroup
	for i, tool := range tools {
		toolArgs, ok := args[tool.Name]
		if !ok {
			t.Errorf("tool %q has no recording arguments: add it to toolGoldenArgs", tool.Name)
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = tool.Handler(NewClient(srv.URL, toolTokenPrefix+tool.Name), toolArgs)
		}()
	}
	wg.Wait()

	doc := toolsGolden{ReadOnlyToolNames: ReadOnlyToolNames()}
	seen := map[string]bool{}
	for i, tool := range tools {
		seen[tool.Name] = true
		if errs[i] != nil {
			t.Errorf("tool %q failed against the stub API, so its calls may be incomplete: %v", tool.Name, errs[i])
		}
		for _, miss := range api.misses[tool.Name] {
			t.Errorf("tool %q calls %s, which is not a route in %s", tool.Name, miss, routeInventoryShown)
		}
		calls, reqs := api.calls[tool.Name], api.reqs[tool.Name]
		if calls == nil {
			calls, reqs = []string{}, []string{}
		}
		doc.Tools = append(doc.Tools, toolGolden{
			Name:        tool.Name,
			ReadOnly:    ReadOnly(tool.Name),
			Description: tool.Description,
			InputSchema: schemaJSON(t, tool.InputSchema),
			Calls:       calls,
			Requests:    reqs,
		})
	}
	for name := range args {
		if !seen[name] {
			t.Errorf("toolGoldenArgs names %q, which is not in Tools()", name)
		}
	}
	return doc
}

// schemaJSON renders an input schema as the tools/list response carries it
// (object keys sorted), without HTML escaping.
func schemaJSON(t *testing.T, schema map[string]interface{}) json.RawMessage {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(schema); err != nil {
		t.Fatalf("encode input schema: %v", err)
	}
	return json.RawMessage(bytes.TrimSpace(buf.Bytes()))
}

// checkToolsListAgainstGolden compares a tools/list result served from the
// whole table with testdata/tools.json: every tool's name, description and
// input schema, in order. Under UPDATE_GOLDEN=1 the file may be rewritten by
// TestMCPToolsGolden in the same run, so the table is compared with itself
// instead.
func checkToolsListAgainstGolden(t *testing.T, served []interface{}) {
	t.Helper()
	var want []map[string]interface{}
	if updatingGoldens() {
		for _, tool := range Tools() {
			want = append(want, map[string]interface{}{"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema})
		}
	} else {
		shown := goldenPkgDir + "/" + filepath.ToSlash(toolsGoldenPath)
		raw, err := os.ReadFile(toolsGoldenPath)
		if err != nil {
			t.Fatalf("read golden %s: %v\nCreate it with:\n  %s", shown, err, regenerateCommand("TestMCPToolsGolden"))
		}
		var doc toolsGolden
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parse golden %s: %v", shown, err)
		}
		for _, tool := range doc.Tools {
			want = append(want, map[string]interface{}{"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema})
		}
	}
	wantJSON, gotJSON := string(encodeGolden(t, want)), string(encodeGolden(t, served))
	if wantJSON != gotJSON {
		t.Errorf("tools/list does not serve the tool table pinned in %s/%s:\n%s\n"+
			"A refactor never changes it. If this is a deliberate behavior change, regenerate it with:\n  %s",
			goldenPkgDir, filepath.ToSlash(toolsGoldenPath), goldenDiff(wantJSON, gotJSON), regenerateCommand("TestMCPToolsGolden"))
	}
}

// TestMCPToolsGoldenArgsCoverSchemas keeps the recording honest in both
// directions: an argument the recording sets must be one the tool's schema
// declares, so a renamed property cannot leave the recording driving a path
// the tool no longer takes; and every property the schema declares must be
// set, so a new optional argument cannot add a call the recording never makes.
func TestMCPToolsGoldenArgsCoverSchemas(t *testing.T) {
	args := toolGoldenArgs()
	for _, tool := range Tools() {
		props, _ := tool.InputSchema["properties"].(map[string]interface{})
		var unknown []string
		for key := range args[tool.Name] {
			if _, ok := props[key]; !ok {
				unknown = append(unknown, key)
			}
		}
		sort.Strings(unknown)
		if len(unknown) > 0 {
			t.Errorf("toolGoldenArgs sets %v for %q, which its input schema does not declare", unknown, tool.Name)
		}
		var unset []string
		for key := range props {
			if _, ok := args[tool.Name][key]; !ok {
				unset = append(unset, key)
			}
		}
		sort.Strings(unset)
		if len(unset) > 0 {
			t.Errorf("toolGoldenArgs leaves %v unset for %q: set every property its input schema declares, so the recording reaches every call", unset, tool.Name)
		}
	}
}
