// The MCP tools over test runs and their results. Tools, in tools.go,
// concatenates each area's constructor in the table's order.

package mcp

import (
	"fmt"
	"strings"
)

// testRunTools returns the test run tools, in Tools() order.
func testRunTools() []Tool {
	return []Tool{
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
				"evidence":     strList("Optional evidence attachment IDs"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("POST", "/api/v1/test-runs/"+strArg(args, "run_id")+"/results", nil, map[string]interface{}{
					"test_case_id": strArg(args, "test_case_id"),
					"status":       strArg(args, "status"),
					"notes":        strArg(args, "notes"),
					"evidence":     strListArg(args, "evidence"),
				})
				return out, err
			},
		},
		{
			Name:        "close_test_run",
			Description: "Close a test run once its results are recorded: status \"completed\", or \"aborted\" for a run that was abandoned. Only an in-progress run can be closed.",
			InputSchema: schema([]string{"run_id", "status"}, map[string]interface{}{
				"run_id": str("Test run ID"),
				"status": str("completed or aborted"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				status := strings.ToLower(strings.TrimSpace(strArg(args, "status")))
				if status != "completed" && status != "aborted" {
					return "", fmt.Errorf("status %q: a run closes as completed or aborted", strArg(args, "status"))
				}
				out, _, err := c.request("PUT", "/api/v1/test-runs/"+strArg(args, "run_id"), nil, map[string]interface{}{
					"status": status,
				})
				return out, err
			},
		},
	}
}
