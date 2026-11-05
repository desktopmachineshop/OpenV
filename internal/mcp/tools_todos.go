// The MCP tools over to-dos: one paragraph to tick off, kept off the board
// (issue #731). Tools, in tools.go, concatenates each area's constructor in
// the table's order.

package mcp

import "strings"

// todoTools returns the tools that list, add and tick off a project's
// to-dos, in Tools() order.
func todoTools() []Tool {
	return []Tool{
		{
			Name:        "list_todos",
			ReadOnly:    true,
			Description: "List a project's to-dos: one-paragraph jobs to tick off, separate from the kanban board. Open ones come first, soonest due first, then the done ones. Set include_done to false to leave the done ones out.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id":   str("Project ID"),
				"include_done": boolean("Include to-dos already ticked off (default true)"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/projects/"+strArg(args, "project_id")+"/todos", nil, nil)
				if err != nil {
					return out, err
				}
				if _, set := args["include_done"]; !set || boolArg(args, "include_done") {
					return out, nil
				}
				list, err := decodeList(out)
				if err != nil {
					return "", err
				}
				open := make([]map[string]interface{}, 0, len(list))
				for _, t := range list {
					if done, _ := t["done"].(bool); !done {
						open = append(open, t)
					}
				}
				return toJSON(open)
			},
		},
		{
			Name:        "create_todo",
			Description: "Add a to-do to a project: one paragraph of text, at most 1000 characters, for a small job someone just does and ticks off. Optionally assign it to a project member and give it a due date (YYYY-MM-DD). Larger work with discussion belongs on the board as a work item.",
			InputSchema: schema([]string{"project_id", "text"}, map[string]interface{}{
				"project_id":  str("Project ID"),
				"text":        str("The to-do: one paragraph"),
				"assignee_id": str("Optional user ID of the project member who has it"),
				"due_date":    str("Optional due date, YYYY-MM-DD"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				body := map[string]interface{}{"text": strArg(args, "text")}
				if a := strings.TrimSpace(strArg(args, "assignee_id")); a != "" {
					body["assignee_id"] = a
				}
				if d := strings.TrimSpace(strArg(args, "due_date")); d != "" {
					if len(d) == len("2006-01-02") {
						d += "T00:00:00Z"
					}
					body["due_date"] = d
				}
				out, _, err := c.request("POST", "/api/v1/projects/"+strArg(args, "project_id")+"/todos", nil, body)
				return out, err
			},
		},
		{
			Name:        "tick_todo",
			Description: "Tick a to-do off as done, or set done to false to open it again.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id":   str("To-do ID"),
				"done": boolean("true to tick it off (the default), false to open it again"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				done := true
				if _, set := args["done"]; set {
					done = boolArg(args, "done")
				}
				out, _, err := c.request("POST", "/api/v1/todos/"+strArg(args, "id")+"/done", nil, map[string]interface{}{"done": done})
				return out, err
			},
		},
	}
}
