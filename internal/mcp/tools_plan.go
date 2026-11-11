// The MCP tools over the project plan, the Gantt over the board
// (internal/domain/plan). Tools, in tools.go, concatenates each area's
// constructor in the table's order.

package mcp

import "strings"

// planTools returns the tools that read a project's plan and place a task
// on it, in Tools() order.
func planTools() []Tool {
	return []Tool{
		{
			Name:     "get_plan",
			ReadOnly: true,
			Description: "Read a project's plan: every board task in outline order with its start and due dates, parent task, progress " +
				"(from its to-dos and sub-tasks), its relationships to the tasks that drive it (FS, SS or FF) and whether it is late; the milestones in date order, each target " +
				"release among them, with their completion criteria measured against the project's artifacts; and every metric a " +
				"criterion can name with its current value.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/projects/"+strArg(args, "project_id")+"/plan", nil, nil)
				return out, err
			},
		},
		{
			Name: "schedule_work_item",
			Description: "Place a board task on the plan: its start date and due date (YYYY-MM-DD), its parent task (making it a " +
				"sub-task) and its relationships to the tasks that drive it. A relationship is written \"<work item id>:<type>\", " +
				"where the type is FS (finish to start: this task starts after that one finishes), SS (start to start: it starts no " +
				"earlier than that one starts) or FF (finish to finish: it finishes no earlier than that one finishes); with no type it " +
				"is FS. A task may have several, so two tasks can together drive when it starts. Only the arguments you pass change; " +
				"pass an empty string to clear a date or the parent, and an empty relationships list to clear them. Answers the " +
				"task's row of the plan.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id":            str("Work item ID"),
				"start_date":    str("Start date, YYYY-MM-DD; an empty string clears it"),
				"due_date":      str("Due date, YYYY-MM-DD; an empty string clears it"),
				"parent_id":     str("Work item ID of the parent task; an empty string makes it a top-level task"),
				"relationships": strList("The tasks that drive this one, each \"<work item id>:FS\", \":SS\" or \":FF\"; replaces the task's relationships"),
				"depends_on":    strList("Earlier form of relationships: work item IDs, each finish to start unless it already has a type"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				body := map[string]interface{}{}
				for _, key := range []string{"start_date", "due_date", "parent_id"} {
					if _, set := args[key]; set {
						body[key] = strArg(args, key)
					}
				}
				if _, set := args["relationships"]; set {
					rels := []map[string]string{}
					for _, r := range strListArg(args, "relationships") {
						id, kind, _ := strings.Cut(r, ":")
						rels = append(rels, map[string]string{"task_id": strings.TrimSpace(id), "type": strings.TrimSpace(kind)})
					}
					body["relationships"] = rels
				}
				if _, set := args["depends_on"]; set {
					deps := strListArg(args, "depends_on")
					if deps == nil {
						deps = []string{}
					}
					body["depends_on"] = deps
				}
				out, _, err := c.request("PUT", "/api/v1/work-items/"+strArg(args, "id")+"/schedule", nil, body)
				return out, err
			},
		},
	}
}
