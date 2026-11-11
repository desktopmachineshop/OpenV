// The MCP tools over the project plan, the Gantt over the board
// (internal/domain/plan). Tools, in tools.go, concatenates each area's
// constructor in the table's order.

package mcp

import (
	"fmt"
	"strconv"
	"strings"
)

// planTools returns the tools that read a project's plan and place a task
// on it, in Tools() order.
func planTools() []Tool {
	return []Tool{
		{
			Name:     "get_plan",
			ReadOnly: true,
			Description: "Read a project's plan: every board task in outline order with its start and due dates, parent task, progress " +
				"(from its to-dos and sub-tasks), its duration and float in working days, its effort in days, whether it is on the critical path (the chain of tasks that decides the plan's end, which may still have float) and the day its end can slip to before it pushes the next task, its relationships to the tasks that drive it (FS, SS or FF) and whether it is late; the milestones in date order, each target " +
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
			Description: "Place a board task on the plan: its start date and due date (YYYY-MM-DD), its duration in working days and effort in days " +
				"(a task with sub-tasks takes all four from them and refuses them), " +
				"its parent task (making it a sub-task) and its relationships to the tasks that drive it. A relationship is written \"<work item id>:<type>\", " +
				"where the type is FS (finish to start: this task starts after that one finishes), SS (start to start: it starts no " +
				"earlier than that one starts) or FF (finish to finish: it finishes no earlier than that one finishes); with no type it " +
				"is FS. A task may have several, so two tasks can together drive when it starts. Only the arguments you pass change; " +
				"pass an empty string to clear a date or the parent, and an empty relationships list to clear them. Answers the " +
				"task's row of the plan.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id":            str("Work item ID"),
				"start_date":    str("Start date, YYYY-MM-DD; an empty string clears it"),
				"due_date":      str("Due date, YYYY-MM-DD; an empty string clears it"),
				"duration_days": str("How long the task runs, in whole working days counting its first and last; the task keeps it, so a start date sent alone moves the due date and a due date sent alone moves the start. \"0\" clears it"),
				"effort_days":   str("The work in the task, in person-days (may be a decimal); \"0\" clears it"),
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
				for _, key := range []string{"duration_days", "effort_days"} {
					if _, set := args[key]; !set {
						continue
					}
					n, err := strconv.ParseFloat(strings.TrimSpace(strArg(args, key)), 64)
					if err != nil {
						return "", fmt.Errorf("%s must be a number of days", key)
					}
					body[key] = n
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
		{
			Name: "set_plan_working_days",
			Description: "Set which days of the week a project's plan works, as ISO weekday numbers from \"1\" (Monday) to \"7\" " +
				"(Sunday). A task's duration and float count only working days, so a plan that works Monday to Friday (the " +
				"default) leaves weekends out. Answers the working days as stored.",
			InputSchema: schema([]string{"project_id", "working_days"}, map[string]interface{}{
				"project_id":   str("Project ID"),
				"working_days": strList("ISO weekday numbers, \"1\" (Monday) to \"7\" (Sunday); at least one"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				days := []int{}
				for _, d := range strListArg(args, "working_days") {
					n, err := strconv.Atoi(strings.TrimSpace(d))
					if err != nil {
						return "", fmt.Errorf("working_days are weekday numbers from 1 (Monday) to 7 (Sunday)")
					}
					days = append(days, n)
				}
				out, _, err := c.request("PUT", "/api/v1/projects/"+strArg(args, "project_id")+"/plan/settings", nil,
					map[string]interface{}{"working_days": days})
				return out, err
			},
		},
	}
}
