package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stdioSession drives the JSON-RPC loop over in-memory pipes, reading
// responses line-by-line so the tests pin the real framing: one JSON object
// per newline-terminated line.
type stdioSession struct {
	t    *testing.T
	in   *io.PipeWriter
	out  *bufio.Reader
	done chan error
}

type rpcErrMsg struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcMsg struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      json.RawMessage        `json:"id"`
	Result  map[string]interface{} `json:"result"`
	Error   *rpcErrMsg             `json:"error"`
}

// startSession runs serve in a goroutine against pipe-backed stdin/stdout.
func startSession(t *testing.T, client *Client, tools []Tool) *stdioSession {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := &stdioSession{
		t:    t,
		in:   inW,
		out:  bufio.NewReader(outR),
		done: make(chan error, 1),
	}
	go func() {
		s.done <- serve(inR, outW, client, tools)
	}()
	t.Cleanup(func() {
		_ = inW.Close()
		_ = outR.Close() // unblock any in-flight tool goroutine's write
	})
	return s
}

// sendRaw writes one raw line to the server's stdin.
func (s *stdioSession) sendRaw(raw string) {
	s.t.Helper()
	if _, err := io.WriteString(s.in, raw); err != nil {
		s.t.Fatalf("write %q: %v", raw, err)
	}
}

// send writes one JSON-RPC request line with a string id.
func (s *stdioSession) send(id string, method string, params interface{}) {
	s.t.Helper()
	msg := map[string]interface{}{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	buf, err := json.Marshal(msg)
	if err != nil {
		s.t.Fatal(err)
	}
	s.sendRaw(string(buf) + "\n")
}

// recv reads exactly one newline-terminated line and decodes it.
func (s *stdioSession) recv() rpcMsg {
	s.t.Helper()
	type lineResult struct {
		line string
		err  error
	}
	ch := make(chan lineResult, 1)
	go func() {
		line, err := s.out.ReadString('\n')
		ch <- lineResult{line, err}
	}()
	var line string
	select {
	case r := <-ch:
		if r.err != nil {
			s.t.Fatalf("read response line: %v", r.err)
		}
		line = r.line
	case <-time.After(10 * time.Second):
		s.t.Fatal("timed out waiting for a response line")
	}
	if !strings.HasSuffix(line, "\n") {
		s.t.Fatalf("response not newline-terminated: %q", line)
	}
	var msg rpcMsg
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		s.t.Fatalf("response line is not one JSON object %q: %v", line, err)
	}
	if msg.JSONRPC != "2.0" {
		s.t.Fatalf("response jsonrpc = %q, want 2.0 (line %q)", msg.JSONRPC, line)
	}
	return msg
}

// shutdown closes stdin and asserts serve returns cleanly.
func (s *stdioSession) shutdown() {
	s.t.Helper()
	_ = s.in.Close()
	select {
	case err := <-s.done:
		if err != nil {
			s.t.Fatalf("serve returned %v, want nil on EOF", err)
		}
	case <-time.After(10 * time.Second):
		s.t.Fatal("serve did not return after stdin EOF")
	}
}

func idString(t *testing.T, id json.RawMessage) string {
	t.Helper()
	return string(id)
}

// callResult unpacks the MCP tool-call result envelope.
func callResult(t *testing.T, msg rpcMsg) (text string, isError bool) {
	t.Helper()
	if msg.Error != nil {
		t.Fatalf("rpc error %d %q, want a result", msg.Error.Code, msg.Error.Message)
	}
	content, ok := msg.Result["content"].([]interface{})
	if !ok || len(content) != 1 {
		t.Fatalf("result content = %#v, want exactly one block", msg.Result["content"])
	}
	block, _ := content[0].(map[string]interface{})
	if block["type"] != "text" {
		t.Fatalf("content block type = %v, want text", block["type"])
	}
	text, _ = block["text"].(string)
	isError, _ = msg.Result["isError"].(bool)
	return text, isError
}

// echoTools returns a minimal tool table for loop tests: an echo tool that
// round-trips its arguments and a fail tool that returns an error.
func echoTools() []Tool {
	return []Tool{
		{
			Name:        "echo",
			Description: "echo arguments back as JSON",
			InputSchema: schema(nil, map[string]interface{}{}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				if args == nil {
					return "", errors.New("args must never be nil")
				}
				return toJSON(args)
			},
		},
		{
			Name:        "fail",
			Description: "always fails",
			InputSchema: schema(nil, map[string]interface{}{}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				if partial, _ := args["partial"].(string); partial != "" {
					return partial, errors.New("boom")
				}
				return "", errors.New("boom")
			},
		},
	}
}

func TestServeStdioHandshakeToolsListAndShutdown(t *testing.T) {
	s := startSession(t, nil, Tools())

	// initialize -> protocol handshake.
	s.send("1", "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]interface{}{"name": "test", "version": "0"},
	})
	resp := s.recv()
	if idString(t, resp.ID) != `"1"` {
		t.Fatalf("initialize response id = %s, want \"1\"", resp.ID)
	}
	if resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}
	if got := resp.Result["protocolVersion"]; got != "2024-11-05" {
		t.Errorf("protocolVersion = %v, want 2024-11-05", got)
	}
	info, _ := resp.Result["serverInfo"].(map[string]interface{})
	if info["name"] != "openv-mcp" {
		t.Errorf("serverInfo = %v, want name openv-mcp", info)
	}
	caps, _ := resp.Result["capabilities"].(map[string]interface{})
	if _, ok := caps["tools"]; !ok {
		t.Errorf("capabilities = %v, want a tools capability", caps)
	}

	// The initialized notification produces no response; blank lines are
	// skipped. The next response must belong to the ping.
	s.sendRaw(`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n")
	s.sendRaw("\n  \n")
	s.send("2", "ping", nil)
	resp = s.recv()
	if idString(t, resp.ID) != `"2"` || resp.Error != nil {
		t.Fatalf("ping response = id %s err %+v, want id \"2\" and no error", resp.ID, resp.Error)
	}

	// tools/list mirrors the tool table.
	s.send("3", "tools/list", nil)
	resp = s.recv()
	if idString(t, resp.ID) != `"3"` || resp.Error != nil {
		t.Fatalf("tools/list response = id %s err %+v", resp.ID, resp.Error)
	}
	list, _ := resp.Result["tools"].([]interface{})
	want := Tools()
	if len(list) != len(want) {
		t.Fatalf("tools/list returned %d tools, want %d", len(list), len(want))
	}
	for i, raw := range list {
		entry, _ := raw.(map[string]interface{})
		if entry["name"] != want[i].Name {
			t.Errorf("tool %d name = %v, want %s", i, entry["name"], want[i].Name)
		}
		if desc, _ := entry["description"].(string); desc == "" {
			t.Errorf("tool %v has no description", entry["name"])
		}
		schema, _ := entry["inputSchema"].(map[string]interface{})
		if schema["type"] != "object" {
			t.Errorf("tool %v inputSchema = %v, want an object schema", entry["name"], schema)
		}
	}

	s.shutdown()
}

func TestServeStdioToolsCall(t *testing.T) {
	s := startSession(t, nil, echoTools())

	t.Run("happy path round-trips arguments", func(t *testing.T) {
		s.send("10", "tools/call", map[string]interface{}{
			"name":      "echo",
			"arguments": map[string]interface{}{"key": "value", "n": 4},
		})
		resp := s.recv()
		if idString(t, resp.ID) != `"10"` {
			t.Fatalf("id = %s, want \"10\"", resp.ID)
		}
		text, isError := callResult(t, resp)
		if isError {
			t.Fatalf("isError = true for a successful call (text %q)", text)
		}
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(text), &args); err != nil {
			t.Fatalf("echoed text %q: %v", text, err)
		}
		if args["key"] != "value" || args["n"] != float64(4) {
			t.Errorf("echoed args = %v", args)
		}
	})

	t.Run("missing arguments become an empty map", func(t *testing.T) {
		s.send("11", "tools/call", map[string]interface{}{"name": "echo"})
		resp := s.recv()
		text, isError := callResult(t, resp)
		if isError {
			t.Fatalf("isError = true, text %q", text)
		}
		if text != "{}" {
			t.Errorf("echo with nil arguments = %q, want {}", text)
		}
	})

	t.Run("handler error sets isError with the message", func(t *testing.T) {
		s.send("12", "tools/call", map[string]interface{}{"name": "fail"})
		resp := s.recv()
		text, isError := callResult(t, resp)
		if !isError || text != "boom" {
			t.Errorf("failed call = isError %v text %q, want true/boom", isError, text)
		}
	})

	t.Run("handler error keeps partial output after the message", func(t *testing.T) {
		s.send("13", "tools/call", map[string]interface{}{
			"name":      "fail",
			"arguments": map[string]interface{}{"partial": "API said no"},
		})
		resp := s.recv()
		text, isError := callResult(t, resp)
		if !isError || text != "boom\nAPI said no" {
			t.Errorf("failed call = isError %v text %q, want error then partial output", isError, text)
		}
	})

	t.Run("unknown tool is a -32602 rpc error", func(t *testing.T) {
		s.send("14", "tools/call", map[string]interface{}{"name": "nope"})
		resp := s.recv()
		if resp.Error == nil || resp.Error.Code != -32602 {
			t.Fatalf("error = %+v, want code -32602", resp.Error)
		}
		if !strings.Contains(resp.Error.Message, "unknown tool nope") {
			t.Errorf("message = %q, want it to name the unknown tool", resp.Error.Message)
		}
	})

	t.Run("non-object params are a -32602 rpc error", func(t *testing.T) {
		s.sendRaw(`{"jsonrpc":"2.0","id":"15","method":"tools/call","params":[1,2]}` + "\n")
		resp := s.recv()
		if resp.Error == nil || resp.Error.Code != -32602 || resp.Error.Message != "invalid params" {
			t.Fatalf("error = %+v, want -32602 invalid params", resp.Error)
		}
		if idString(t, resp.ID) != `"15"` {
			t.Errorf("id = %s, want \"15\"", resp.ID)
		}
	})

	s.shutdown()
}

func TestServeStdioMalformedJSONKeepsServing(t *testing.T) {
	s := startSession(t, nil, echoTools())

	s.sendRaw("this is not json\n")
	resp := s.recv()
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Fatalf("error = %+v, want -32700 parse error", resp.Error)
	}
	if idString(t, resp.ID) != "null" {
		t.Errorf("parse-error id = %s, want null", resp.ID)
	}

	// A truncated JSON object is a parse error too.
	s.sendRaw(`{"jsonrpc":"2.0","id":1,"method":"ping"` + "\n")
	resp = s.recv()
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Fatalf("error = %+v, want -32700 for truncated JSON", resp.Error)
	}

	// The loop survives and keeps answering.
	s.send("2", "ping", nil)
	resp = s.recv()
	if idString(t, resp.ID) != `"2"` || resp.Error != nil {
		t.Fatalf("post-garbage ping = id %s err %+v", resp.ID, resp.Error)
	}

	s.shutdown()
}

func TestServeStdioUnknownMethod(t *testing.T) {
	s := startSession(t, nil, echoTools())

	// With an id: method-not-found error.
	s.send("7", "resources/list", nil)
	resp := s.recv()
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("error = %+v, want -32601", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "resources/list") {
		t.Errorf("message = %q, want it to name the method", resp.Error.Message)
	}

	// Without an id (notification) or with id null: silence. The next
	// response line must belong to the ping that follows.
	s.sendRaw(`{"jsonrpc":"2.0","method":"notifications/cancelled"}` + "\n")
	s.sendRaw(`{"jsonrpc":"2.0","id":null,"method":"notifications/cancelled"}` + "\n")
	s.send("8", "ping", nil)
	resp = s.recv()
	if idString(t, resp.ID) != `"8"` || resp.Error != nil {
		t.Fatalf("response after unknown notifications = id %s err %+v, want the ping's", resp.ID, resp.Error)
	}

	s.shutdown()
}

// TestServeStdioOversizedLine drives a request line larger than the 1MB
// bufio buffer through the loop: ReadString must accumulate the whole line
// and the response must round-trip the payload intact.
func TestServeStdioOversizedLine(t *testing.T) {
	s := startSession(t, nil, echoTools())

	big := strings.Repeat("x", 2*1024*1024)
	req, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": "big", "method": "tools/call",
		"params": map[string]interface{}{
			"name":      "echo",
			"arguments": map[string]interface{}{"blob": big},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.sendRaw(string(req) + "\n")

	resp := s.recv()
	if idString(t, resp.ID) != `"big"` {
		t.Fatalf("id = %s, want \"big\"", resp.ID)
	}
	text, isError := callResult(t, resp)
	if isError {
		t.Fatalf("isError = true: %s", text[:min(200, len(text))])
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(text), &args); err != nil {
		t.Fatal(err)
	}
	if args["blob"] != big {
		t.Errorf("blob corrupted: len %d, want %d", len(args["blob"]), len(big))
	}

	s.shutdown()
}

// TestServeStdioSlowToolDoesNotBlockLoop pins the design decision that
// tools/call handlers run off the read loop: a ping issued after a blocked
// tool call must be answered first.
func TestServeStdioSlowToolDoesNotBlockLoop(t *testing.T) {
	release := make(chan struct{})
	tools := []Tool{{
		Name:        "block",
		InputSchema: schema(nil, map[string]interface{}{}),
		Handler: func(c *Client, args map[string]interface{}) (string, error) {
			<-release
			return "released", nil
		},
	}}
	s := startSession(t, nil, tools)

	s.send("slow", "tools/call", map[string]interface{}{"name": "block"})
	s.send("fast", "ping", nil)

	resp := s.recv()
	if idString(t, resp.ID) != `"fast"` {
		t.Fatalf("first response id = %s, want the ping (\"fast\") while the tool is blocked", resp.ID)
	}

	close(release)
	resp = s.recv()
	if idString(t, resp.ID) != `"slow"` {
		t.Fatalf("second response id = %s, want \"slow\"", resp.ID)
	}
	text, isError := callResult(t, resp)
	if isError || text != "released" {
		t.Errorf("blocked tool result = %q isError %v", text, isError)
	}

	s.shutdown()
}

// TestServeStdioFinalRequestWithoutNewline: a request in the last line
// before EOF, with no trailing newline, is still served before the clean
// shutdown.
func TestServeStdioFinalRequestWithoutNewline(t *testing.T) {
	s := startSession(t, nil, echoTools())

	s.sendRaw(`{"jsonrpc":"2.0","id":"last","method":"ping"}`) // no \n
	_ = s.in.Close()

	resp := s.recv()
	if idString(t, resp.ID) != `"last"` || resp.Error != nil {
		t.Fatalf("final response = id %s err %+v, want the un-terminated ping's", resp.ID, resp.Error)
	}
	select {
	case err := <-s.done:
		if err != nil {
			t.Fatalf("serve returned %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not return after EOF")
	}
}

// TestServeStdioRealToolOverHTTP runs the full stack: JSON-RPC line in,
// authenticated HTTP call out, API payload back through the content block.
func TestServeStdioRealToolOverHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/projects" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer run-token" {
			t.Errorf("auth header = %q", got)
		}
		fmt.Fprint(w, `[{"id":"p1","name":"Demo"}]`)
	}))
	defer server.Close()

	s := startSession(t, NewClient(server.URL, "run-token"), Tools())

	s.send("1", "tools/call", map[string]interface{}{"name": "list_projects"})
	resp := s.recv()
	text, isError := callResult(t, resp)
	if isError {
		t.Fatalf("isError = true: %s", text)
	}
	if text != `[{"id":"p1","name":"Demo"}]` {
		t.Errorf("text = %q, want the API payload passed through", text)
	}

	s.shutdown()
}
