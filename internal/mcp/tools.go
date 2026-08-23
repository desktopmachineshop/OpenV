// Package mcp implements a stdio MCP server exposing OpenV tools to agent
// CLIs. The JSON-RPC 2.0 loop is hand-rolled (MCP spec 2024-11-05) to keep
// the binary dependency-free.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Client calls the OpenV API as the agent (run-token auth).
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient creates an API client for the MCP tools.
func NewClient(baseURL, runToken string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   runToken,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// request performs an API call and returns the response body and status.
func (c *Client) request(method, path string, query url.Values, body interface{}) (string, int, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return "", 0, err
		}
		reader = bytes.NewReader(buf)
	}
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(method, u, reader)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}
	text := strings.TrimSpace(string(data))
	if resp.StatusCode >= 400 {
		return text, resp.StatusCode, fmt.Errorf("API %d: %s", resp.StatusCode, text)
	}
	return text, resp.StatusCode, nil
}

// Tool is one MCP tool: schema plus handler. The table is exported so other
// hosts (e.g. an API loop) can reuse it.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     func(c *Client, args map[string]interface{}) (string, error)
}

func schema(required []string, props map[string]interface{}) map[string]interface{} {
	if required == nil {
		required = []string{}
	}
	return map[string]interface{}{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

func str(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc}
}

func obj(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "object", "description": desc}
}

func strArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}

// decodeList parses a JSON array response into generic maps.
func decodeList(raw string) ([]map[string]interface{}, error) {
	var list []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, fmt.Errorf("unexpected list response: %v", err)
	}
	return list, nil
}

func toJSON(v interface{}) (string, error) {
	buf, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

// Tools returns the OpenV tool table.
func Tools() []Tool {
	return []Tool{
		{
			Name:        "list_projects",
			Description: "List all projects visible to this agent.",
			InputSchema: schema(nil, map[string]interface{}{}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/projects", nil, nil)
				return out, err
			},
		},
		{
			Name:        "list_artifacts",
			Description: "List artifacts in a project, optionally filtered by type.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"type":       str("Optional artifact type filter"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				q := url.Values{"project_id": {strArg(args, "project_id")}}
				if t := strArg(args, "type"); t != "" {
					q.Set("type", t)
				}
				out, _, err := c.request("GET", "/api/v1/artifacts", q, nil)
				return out, err
			},
		},
		{
			Name:        "get_artifact",
			Description: "Get a single artifact by ID, including its body.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id": str("Artifact ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/artifacts/"+strArg(args, "id"), nil, nil)
				return out, err
			},
		},
		{
			Name:        "get_project_tree",
			Description: "Get the project's artifact tree: id, type, title, parent_id, sort_order per artifact (no bodies).",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				q := url.Values{"project_id": {strArg(args, "project_id")}}
				out, _, err := c.request("GET", "/api/v1/artifacts", q, nil)
				if err != nil {
					return out, err
				}
				list, err := decodeList(out)
				if err != nil {
					return "", err
				}
				tree := make([]map[string]interface{}, 0, len(list))
				for _, a := range list {
					tree = append(tree, map[string]interface{}{
						"id":         a["id"],
						"type":       a["type"],
						"title":      a["title"],
						"parent_id":  a["parent_id"],
						"sort_order": a["sort_order"],
					})
				}
				return toJSON(tree)
			},
		},
		{
			Name:        "search_artifacts",
			Description: "Case-insensitive substring search over artifact titles and bodies in a project.",
			InputSchema: schema([]string{"project_id", "query"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"query":      str("Substring to search for"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				q := url.Values{"project_id": {strArg(args, "project_id")}}
				out, _, err := c.request("GET", "/api/v1/artifacts", q, nil)
				if err != nil {
					return out, err
				}
				list, err := decodeList(out)
				if err != nil {
					return "", err
				}
				needle := strings.ToLower(strArg(args, "query"))
				matches := []map[string]interface{}{}
				for _, a := range list {
					title, _ := a["title"].(string)
					body, _ := a["body"].(string)
					if strings.Contains(strings.ToLower(title), needle) ||
						strings.Contains(strings.ToLower(body), needle) {
						matches = append(matches, map[string]interface{}{
							"id":    a["id"],
							"type":  a["type"],
							"title": a["title"],
						})
					}
				}
				return toJSON(matches)
			},
		},
		{
			Name:        "create_artifact",
			Description: "Create an artifact. A 202 response means the change was diverted to a proposal pending human review.",
			InputSchema: schema([]string{"project_id", "type", "title"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"type":       str("Artifact type"),
				"title":      str("Title"),
				"body":       str("Optional markdown body"),
				"parent_id":  str("Optional parent artifact ID"),
				"attributes": obj("Optional attributes object"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				body := map[string]interface{}{
					"project_id": strArg(args, "project_id"),
					"type":       strArg(args, "type"),
					"title":      strArg(args, "title"),
					"body":       strArg(args, "body"),
				}
				if p := strArg(args, "parent_id"); p != "" {
					body["parent_id"] = p
				}
				if attrs, ok := args["attributes"].(map[string]interface{}); ok {
					body["attributes"] = attrs
				}
				out, status, err := c.request("POST", "/api/v1/artifacts", nil, body)
				if err != nil {
					return out, err
				}
				if status == http.StatusAccepted {
					return "Proposal created (pending human review, not yet applied): " + out, nil
				}
				return out, nil
			},
		},
		{
			Name:        "update_artifact",
			Description: "Update an artifact. A 202 response means the change was diverted to a proposal pending human review.",
			InputSchema: schema([]string{"id", "type", "title"}, map[string]interface{}{
				"id":         str("Artifact ID"),
				"type":       str("Artifact type"),
				"title":      str("Title"),
				"body":       str("Optional markdown body"),
				"attributes": obj("Optional attributes object"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				body := map[string]interface{}{
					"type":  strArg(args, "type"),
					"title": strArg(args, "title"),
					"body":  strArg(args, "body"),
				}
				if attrs, ok := args["attributes"].(map[string]interface{}); ok {
					body["attributes"] = attrs
				}
				out, status, err := c.request("PUT", "/api/v1/artifacts/"+strArg(args, "id"), nil, body)
				if err != nil {
					return out, err
				}
				if status == http.StatusAccepted {
					return "Proposal created (pending human review, not yet applied): " + out, nil
				}
				return out, nil
			},
		},
		{
			Name:        "create_link",
			Description: "Create a typed link between two artifacts.",
			InputSchema: schema([]string{"from_id", "to_id", "type"}, map[string]interface{}{
				"from_id": str("Source artifact ID"),
				"to_id":   str("Target artifact ID"),
				"type":    str("Link type"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("POST", "/api/v1/links", nil, map[string]interface{}{
					"from_id": strArg(args, "from_id"),
					"to_id":   strArg(args, "to_id"),
					"type":    strArg(args, "type"),
				})
				return out, err
			},
		},
		{
			Name:        "delete_link",
			Description: "Delete a link by ID.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id": str("Link ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("DELETE", "/api/v1/links/"+strArg(args, "id"), nil, nil)
				if err != nil {
					return out, err
				}
				if out == "" {
					out = "link deleted"
				}
				return out, nil
			},
		},
		{
			Name:        "list_links_for_artifact",
			Description: "List all links touching one artifact.",
			InputSchema: schema([]string{"artifact_id", "project_id"}, map[string]interface{}{
				"artifact_id": str("Artifact ID"),
				"project_id":  str("Project ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				q := url.Values{"project_id": {strArg(args, "project_id")}}
				out, _, err := c.request("GET", "/api/v1/links", q, nil)
				if err != nil {
					return out, err
				}
				list, err := decodeList(out)
				if err != nil {
					return "", err
				}
				id := strArg(args, "artifact_id")
				matches := []map[string]interface{}{}
				for _, l := range list {
					if l["from_id"] == id || l["to_id"] == id {
						matches = append(matches, l)
					}
				}
				return toJSON(matches)
			},
		},
		{
			Name:        "add_comment",
			Description: "Add a chatter comment to an artifact.",
			InputSchema: schema([]string{"artifact_id", "message"}, map[string]interface{}{
				"artifact_id": str("Artifact ID"),
				"message":     str("Comment text"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("POST", "/api/v1/chatter", nil, map[string]interface{}{
					"artifact_id": strArg(args, "artifact_id"),
					"message":     strArg(args, "message"),
				})
				return out, err
			},
		},
		{
			Name:        "list_baselines",
			Description: "List a project's baselines.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/projects/"+strArg(args, "project_id")+"/baselines", nil, nil)
				return out, err
			},
		},
		{
			Name:        "get_baseline",
			Description: "Get a baseline by ID.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id": str("Baseline ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/baselines/"+strArg(args, "id"), nil, nil)
				return out, err
			},
		},
		{
			Name:        "create_test_run",
			Description: "Create a test run in a project.",
			InputSchema: schema([]string{"project_id", "name"}, map[string]interface{}{
				"project_id":  str("Project ID"),
				"name":        str("Test run name"),
				"description": str("Optional description"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("POST", "/api/v1/projects/"+strArg(args, "project_id")+"/test-runs", nil, map[string]interface{}{
					"name":        strArg(args, "name"),
					"description": strArg(args, "description"),
				})
				return out, err
			},
		},
		{
			Name:        "record_test_result",
			Description: "Record one test case result in a test run.",
			InputSchema: schema([]string{"run_id", "test_case_id", "status"}, map[string]interface{}{
				"run_id":       str("Test run ID"),
				"test_case_id": str("Test case artifact ID"),
				"status":       str("Result status (e.g. pass, fail, blocked)"),
				"notes":        str("Optional notes"),
				"evidence":     str("Optional evidence"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("POST", "/api/v1/test-runs/"+strArg(args, "run_id")+"/results", nil, map[string]interface{}{
					"test_case_id": strArg(args, "test_case_id"),
					"status":       strArg(args, "status"),
					"notes":        strArg(args, "notes"),
					"evidence":     strArg(args, "evidence"),
				})
				return out, err
			},
		},
		{
			Name:        "list_work_items",
			Description: "List a project's kanban board cards sorted by column and position (no descriptions or activity — use get_work_item for a card's detail). Optionally filter by board column and/or assignee ID.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id":  str("Project ID"),
				"column":      str("Optional board column filter: backlog, todo, in-progress, review or done"),
				"assignee_id": str("Optional assignee ID filter (agent, user or team ID)"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				column := strings.ToLower(strings.TrimSpace(strArg(args, "column")))
				// Mirrors the board columns in internal/domain/workitems; kept
				// local so the stdio binary stays dependency-free.
				switch column {
				case "", "backlog", "todo", "in-progress", "review", "done":
				default:
					return "", fmt.Errorf("unknown column %q: valid columns are backlog, todo, in-progress, review, done", column)
				}
				out, _, err := c.request("GET", "/api/v1/projects/"+strArg(args, "project_id")+"/work-items", nil, nil)
				if err != nil {
					return out, err
				}
				list, err := decodeList(out)
				if err != nil {
					return "", err
				}
				assignee := strArg(args, "assignee_id")
				cards := make([]map[string]interface{}, 0, len(list))
				for _, item := range list {
					if column != "" && item["column"] != column {
						continue
					}
					if assignee != "" && item["assignee_id"] != assignee {
						continue
					}
					cards = append(cards, map[string]interface{}{
						"id":            item["id"],
						"title":         item["title"],
						"column":        item["column"],
						"sort_order":    item["sort_order"],
						"assignee_type": item["assignee_type"],
						"assignee_id":   item["assignee_id"],
						"agent_run_id":  item["agent_run_id"],
						"artifact_ids":  item["artifact_ids"],
						"due_date":      item["due_date"],
					})
				}
				return toJSON(cards)
			},
		},
		{
			Name:        "get_work_item",
			Description: "Get a kanban work item with its activity.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id": str("Work item ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/work-items/"+strArg(args, "id"), nil, nil)
				return out, err
			},
		},
		{
			Name:        "get_work_item_history",
			Description: "Get just the activity history of a work item.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id": str("Work item ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/work-items/"+strArg(args, "id"), nil, nil)
				if err != nil {
					return out, err
				}
				var payload map[string]interface{}
				if err := json.Unmarshal([]byte(out), &payload); err != nil {
					return "", fmt.Errorf("unexpected work item response: %v", err)
				}
				return toJSON(payload["activity"])
			},
		},
		{
			Name:        "update_work_item",
			Description: "Add a progress comment to a work item.",
			InputSchema: schema([]string{"id", "comment"}, map[string]interface{}{
				"id":      str("Work item ID"),
				"comment": str("Comment text"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("POST", "/api/v1/work-items/"+strArg(args, "id")+"/comments", nil, map[string]interface{}{
					"comment": strArg(args, "comment"),
				})
				return out, err
			},
		},
		{
			Name:        "record_candidate_need",
			Description: "Record a candidate user need discovered during an interview. Stored as a draft user-need artifact for later review.",
			InputSchema: schema([]string{"project_id", "need"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"need":       str("Short statement of the user need"),
				"rationale":  str("Optional rationale"),
				"quote":      str("Optional supporting quote from the interviewee"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				var parts []string
				if r := strArg(args, "rationale"); r != "" {
					parts = append(parts, "Rationale: "+r)
				}
				if q := strArg(args, "quote"); q != "" {
					parts = append(parts, "Quote: \""+q+"\"")
				}
				out, status, err := c.request("POST", "/api/v1/artifacts", nil, map[string]interface{}{
					"project_id": strArg(args, "project_id"),
					"type":       "user-need",
					"title":      strArg(args, "need"),
					"body":       strings.Join(parts, "\n\n"),
				})
				if err != nil {
					return out, err
				}
				if status == http.StatusAccepted {
					return "Candidate need recorded as a proposal (pending review): " + out, nil
				}
				return out, nil
			},
		},
		{
			Name:        "delegate_to_agent",
			Description: "Delegate a task to one of this agent's team delegates by role label, then wait for the result (polls up to 30 minutes).",
			InputSchema: schema([]string{"role_label", "prompt"}, map[string]interface{}{
				"role_label": str("Delegate's role label in the team"),
				"prompt":     str("Task prompt for the delegate"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("POST", "/api/v1/agent-runs/delegate", nil, map[string]interface{}{
					"role_label": strArg(args, "role_label"),
					"prompt":     strArg(args, "prompt"),
				})
				if err != nil {
					return out, err
				}
				var launched struct {
					RunID  string `json:"run_id"`
					Status string `json:"status"`
				}
				if err := json.Unmarshal([]byte(out), &launched); err != nil || launched.RunID == "" {
					return "", errors.New("unexpected delegate response: " + out)
				}

				deadline := time.Now().Add(30 * time.Minute)
				for time.Now().Before(deadline) {
					time.Sleep(5 * time.Second)
					body, _, err := c.request("GET", "/api/v1/agent-runs/delegate/"+launched.RunID, nil, nil)
					if err != nil {
						return body, err
					}
					var st struct {
						RunID     string `json:"run_id"`
						Status    string `json:"status"`
						FinalText string `json:"final_text"`
						Error     string `json:"error"`
					}
					if err := json.Unmarshal([]byte(body), &st); err != nil {
						return "", errors.New("unexpected delegate status response: " + body)
					}
					switch st.Status {
					case "succeeded", "awaiting_approval":
						text := st.FinalText
						if text == "" {
							text = "(delegate finished with no final text)"
						}
						if st.Status == "awaiting_approval" {
							text += "\n\n(Note: the delegate's changes are proposals awaiting human approval.)"
						}
						return text, nil
					case "failed", "cancelled", "timed_out":
						detail := st.Error
						if detail == "" {
							detail = "no error detail"
						}
						return "", fmt.Errorf("delegated run %s %s: %s", st.RunID, st.Status, detail)
					}
				}
				return "", errors.New("delegated run " + launched.RunID + " did not finish within 30 minutes")
			},
		},
	}
}

// --- JSON-RPC 2.0 stdio server ---

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

// ServeStdio runs the MCP server over stdin/stdout until EOF.
func ServeStdio(client *Client, tools []Tool) error {
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
	out := json.NewEncoder(os.Stdout)
	respond := func(resp rpcResponse) {
		resp.JSONRPC = "2.0"
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = out.Encode(resp)
	}

	reader := bufio.NewReaderSize(os.Stdin, 1024*1024)
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
