// The MCP tools over the project plan, the Gantt over the board
// (internal/domain/plan). Tools, in tools.go, concatenates each area's
// constructor in the table's order.

package mcp

// planTools returns the tools that read a project's plan and place a task
// on it, in Tools() order.
func planTools() []Tool {
	return []Tool{
		{
			Name:     "get_plan",
			ReadOnly: true,
			Description: "Read a project's plan: every board task in outline order with its start and due dates, parent task, progress " +
				"(from its to-dos and sub-tasks), what it depends on and whether it is late; the milestones in date order, each target " +
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
				"sub-task) and the tasks it depends on (finish-to-start). Only the arguments you pass change; pass an empty string to " +
				"clear a date or the parent, and an empty depends_on list to clear the dependencies. Answers the task's row of the plan.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id":         str("Work item ID"),
				"start_date": str("Start date, YYYY-MM-DD; an empty string clears it"),
				"due_date":   str("Due date, YYYY-MM-DD; an empty string clears it"),
				"parent_id":  str("Work item ID of the parent task; an empty string makes it a top-level task"),
				"depends_on": strList("Work item IDs this task cannot start before the end of"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				body := map[string]interface{}{}
				for _, key := range []string{"start_date", "due_date", "parent_id"} {
					if _, set := args[key]; set {
						body[key] = strArg(args, key)
					}
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
