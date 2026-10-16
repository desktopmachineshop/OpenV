package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// roundTrip answers every API call the tools make with a fixed body, and
// records the requests, so the HTTP transport is tested without a server.
type roundTrip struct {
	got  []*http.Request
	body string
}

func (rt *roundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.got = append(rt.got, r)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(rt.body)), Header: http.Header{}}, nil
}

func decode(t *testing.T, raw []byte) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return out
}

// REQ-44: a connected app is offered every tool but the run-only ones, and
// a read-only grant only the tools that change nothing.
func TestMemberToolsLeaveOutRunOnlyAndWritingTools(t *testing.T) {
	all := Tools()
	full := MemberTools(all, false)
	if len(full) != len(all)-1 {
		t.Fatalf("member tools = %d, want every tool but delegate_to_agent (%d)", len(full), len(all)-1)
	}
	for _, tool := range full {
		if tool.Name == "delegate_to_agent" {
			t.Fatal("delegate_to_agent offered to a member")
		}
	}
	for _, tool := range MemberTools(all, true) {
		if !tool.ReadOnly {
			t.Fatalf("read-only grant offered %s", tool.Name)
		}
	}
	if len(MemberTools(all, true)) != len(ReadOnlyToolNames()) {
		t.Fatalf("read-only grant offers %d tools, want %d", len(MemberTools(all, true)), len(ReadOnlyToolNames()))
	}
}

func TestHTTPNegotiatesTheProtocolVersion(t *testing.T) {
	c := NewClientWithTransport("http://openv", "", &roundTrip{})
	for asked, want := range map[string]string{
		"2025-11-25": "2025-11-25",
		"2025-06-18": "2025-06-18",
		"2025-03-26": "2025-03-26",
		"2024-11-05": "2024-11-05",
		"2099-01-01": "2025-11-25",
	} {
		out := decode(t, HandleHTTP(c, Tools(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"`+asked+`"}}`), "openv", "0.18.0"))
		result := out["result"].(map[string]interface{})
		if result["protocolVersion"] != want {
			t.Errorf("asked %s, got %v", asked, result["protocolVersion"])
		}
		if info := result["serverInfo"].(map[string]interface{}); info["name"] != "openv" || info["version"] != "0.18.0" {
			t.Errorf("serverInfo = %v", info)
		}
	}
}

func TestHTTPListsAndCallsTools(t *testing.T) {
	rt := &roundTrip{body: `[{"id":"p1","name":"Rover"}]`}
	c := NewClientWithTransport("http://openv", "", rt)
	tools := MemberTools(Tools(), true)

	out := decode(t, HandleHTTP(c, tools, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`), "openv", "x"))
	listed := out["result"].(map[string]interface{})["tools"].([]interface{})
	if len(listed) != len(tools) {
		t.Fatalf("listed %d tools, want %d", len(listed), len(tools))
	}

	out = decode(t, HandleHTTP(c, tools, []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}`), "openv", "x"))
	result := out["result"].(map[string]interface{})
	if result["isError"] != false || !strings.Contains(result["content"].([]interface{})[0].(map[string]interface{})["text"].(string), "Rover") {
		t.Fatalf("tools/call = %v", out)
	}
	if len(rt.got) != 1 || rt.got[0].Method != "GET" || rt.got[0].URL.Path != "/api/v1/projects" {
		t.Fatalf("API calls = %+v", rt.got)
	}

	// A tool the grant does not offer is unknown, not called.
	out = decode(t, HandleHTTP(c, tools, []byte(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create_artifact","arguments":{}}}`), "openv", "x"))
	if out["error"].(map[string]interface{})["code"].(float64) != -32602 || len(rt.got) != 1 {
		t.Fatalf("a tool outside the grant: %v (calls %d)", out, len(rt.got))
	}
}

func TestHTTPNotificationsBatchesAndErrors(t *testing.T) {
	c := NewClientWithTransport("http://openv", "", &roundTrip{})
	if got := HandleHTTP(c, Tools(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`), "openv", "x"); got != nil {
		t.Fatalf("a notification was answered: %s", got)
	}
	if got := HandleHTTP(c, Tools(), []byte(`{"jsonrpc":"2.0","id":9,"result":{}}`), "openv", "x"); got != nil {
		t.Fatalf("a client's response was answered: %s", got)
	}
	out := decode(t, HandleHTTP(c, Tools(), []byte(`not json`), "openv", "x"))
	if out["error"].(map[string]interface{})["code"].(float64) != -32700 {
		t.Fatalf("parse error = %v", out)
	}
	out = decode(t, HandleHTTP(c, Tools(), []byte(`{"jsonrpc":"2.0","id":5,"method":"resources/list"}`), "openv", "x"))
	if out["error"].(map[string]interface{})["code"].(float64) != -32601 {
		t.Fatalf("unknown method = %v", out)
	}

	var batch []map[string]interface{}
	if err := json.Unmarshal(HandleHTTP(c, Tools(), []byte(`[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":2,"method":"ping"}]`), "openv", "x"), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 2 || batch[0]["id"].(float64) != 1 || batch[1]["id"].(float64) != 2 {
		t.Fatalf("batch = %v", batch)
	}
	out = decode(t, HandleHTTP(c, Tools(), []byte(`[]`), "openv", "x"))
	if out["error"].(map[string]interface{})["code"].(float64) != -32600 {
		t.Fatalf("empty batch = %v", out)
	}
}
