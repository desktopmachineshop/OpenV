// --- JSON-RPC 2.0 over HTTP: the remote MCP endpoint (REQ-44) ---

package mcp

import (
	"bytes"
	"encoding/json"
)

// ProtocolVersions are the MCP revisions the HTTP endpoint speaks, newest
// first. initialize answers the client's own revision when it is one of
// these and the newest otherwise, which the client may then refuse.
var ProtocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// runOnly names the tools that act only inside an agent run: delegation
// asks the server for a child run of the caller's run, and a member has no
// run, so the server would refuse it every time.
var runOnly = map[string]bool{"delegate_to_agent": true}

// MemberTools is the tool table a member's connected app is offered: every
// tool but those that work only inside an agent run, and with readOnly only
// the tools that change nothing.
func MemberTools(tools []Tool, readOnly bool) []Tool {
	out := make([]Tool, 0, len(tools))
	for _, t := range tools {
		if runOnly[t.Name] || (readOnly && !t.ReadOnly) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// HandleHTTP answers the JSON-RPC body of one POST to the remote MCP
// endpoint: a single message or, as the 2025-03-26 revision allows, a batch.
// It returns the response body, or nil when the body held only
// notifications and responses, which the transport acknowledges with 202
// and no body. Tool calls run one after another, each to completion: there
// is no session and no stream for a long call to report on.
func HandleHTTP(client *Client, tools []Tool, body []byte, serverName, serverVersion string) []byte {
	byName, toolList := toolIndex(tools)
	handle := func(req rpcRequest) *rpcResponse {
		notification := len(req.ID) == 0 || string(req.ID) == "null"
		var resp rpcResponse
		switch req.Method {
		case "initialize":
			var params struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(req.Params, &params)
			version := ProtocolVersions[0]
			for _, v := range ProtocolVersions {
				if v == params.ProtocolVersion {
					version = v
				}
			}
			resp = rpcResponse{ID: req.ID, Result: map[string]interface{}{
				"protocolVersion": version,
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": serverName, "version": serverVersion},
			}}
		case "ping":
			resp = rpcResponse{ID: req.ID, Result: map[string]interface{}{}}
		case "tools/list":
			resp = rpcResponse{ID: req.ID, Result: map[string]interface{}{"tools": toolList}}
		case "tools/call":
			var params struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				resp = rpcResponse{ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid params"}}
				break
			}
			tool, ok := byName[params.Name]
			if !ok {
				resp = rpcResponse{ID: req.ID, Error: &rpcError{Code: -32602, Message: "unknown tool " + params.Name}}
				break
			}
			resp = rpcResponse{ID: req.ID, Result: callTool(client, tool, params.Arguments)}
		default:
			resp = rpcResponse{ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}}
		}
		if notification {
			// notifications/initialized and the like: nothing to answer.
			return nil
		}
		resp.JSONRPC = "2.0"
		return &resp
	}

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(trimmed, &batch); err != nil || len(batch) == 0 {
			return encode(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32600, Message: "invalid request"}})
		}
		answers := []rpcResponse{}
		for _, raw := range batch {
			if r := handleRaw(raw, handle); r != nil {
				answers = append(answers, *r)
			}
		}
		if len(answers) == 0 {
			return nil
		}
		return encode(answers)
	}
	if r := handleRaw(trimmed, handle); r != nil {
		return encode(*r)
	}
	return nil
}

// handleRaw decodes one message and answers it; a message with a method is
// a request or notification, one without is a client's response to the
// server, which this server never asks for and so ignores.
func handleRaw(raw []byte, handle func(rpcRequest) *rpcResponse) *rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return &rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}}
	}
	if req.Method == "" {
		return nil
	}
	return handle(req)
}

func encode(v interface{}) []byte {
	out, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`)
	}
	return out
}
