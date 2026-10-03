// --- JSON-RPC 2.0 stdio server ---

package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// ServeStdio runs the MCP server over stdin/stdout until EOF, serving only the
// tools EnvToolAllowlist admits. Both tools/list and tools/call see the same
// filtered table, so a tool left out is neither advertised nor callable.
func ServeStdio(client *Client, tools []Tool) error {
	return serve(os.Stdin, os.Stdout, client, EnvFilteredTools(tools))
}

// serve is the transport-agnostic JSON-RPC loop behind ServeStdio; split out
// so tests can drive it over an in-memory pipe.
func serve(in io.Reader, w io.Writer, client *Client, tools []Tool) error {
	byName := make(map[string]Tool, len(tools))
	toolList := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
		toolList = append(toolList, map[string]interface{}{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}

	var writeMu sync.Mutex
	out := json.NewEncoder(w)
	respond := func(resp rpcResponse) {
		resp.JSONRPC = "2.0"
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = out.Encode(resp)
	}

	reader := bufio.NewReaderSize(in, 1024*1024)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if strings.TrimSpace(line) == "" {
				if err == io.EOF {
					return nil
				}
				return err
			}
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var req rpcRequest
		if uerr := json.Unmarshal([]byte(trimmed), &req); uerr != nil {
			respond(rpcResponse{ID: nil, Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}

		switch req.Method {
		case "initialize":
			respond(rpcResponse{ID: req.ID, Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": "openv-mcp", "version": "0.1.0"},
			}})
		case "notifications/initialized", "initialized":
			// Notification: no response.
		case "ping":
			respond(rpcResponse{ID: req.ID, Result: map[string]interface{}{}})
		case "tools/list":
			respond(rpcResponse{ID: req.ID, Result: map[string]interface{}{"tools": toolList}})
		case "tools/call":
			var params struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			}
			if uerr := json.Unmarshal(req.Params, &params); uerr != nil {
				respond(rpcResponse{ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid params"}})
				continue
			}
			tool, ok := byName[params.Name]
			if !ok {
				respond(rpcResponse{ID: req.ID, Error: &rpcError{Code: -32602, Message: "unknown tool " + params.Name}})
				continue
			}
			// Long-running tools (delegation) must not block the read loop.
			go func(req rpcRequest, tool Tool, args map[string]interface{}) {
				if args == nil {
					args = map[string]interface{}{}
				}
				text, err := tool.Handler(client, args)
				isError := false
				if err != nil {
					isError = true
					if text != "" {
						text = err.Error() + "\n" + text
					} else {
						text = err.Error()
					}
				}
				respond(rpcResponse{ID: req.ID, Result: map[string]interface{}{
					"content": []map[string]interface{}{{"type": "text", "text": text}},
					"isError": isError,
				}})
			}(req, tool, params.Arguments)
		default:
			if len(req.ID) > 0 && string(req.ID) != "null" {
				respond(rpcResponse{ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}})
			}
		}

		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}
