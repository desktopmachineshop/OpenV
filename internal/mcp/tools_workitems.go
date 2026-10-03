// The MCP tools over kanban work items. Tools, in tools.go, concatenates
// each area's constructor in the table's order.

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// workItemListTools returns the tool that lists a project's work items.
func workItemListTools() []Tool {
	return []Tool{
		{
			Name:        "list_work_items",
			ReadOnly:    true,
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
	}
}

// workItemTools returns the tools that read and update one work item, in
// Tools() order.
func workItemTools() []Tool {
	return []Tool{
		{
			Name:        "get_work_item",
			ReadOnly:    true,
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
			ReadOnly:    true,
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
	}
}
