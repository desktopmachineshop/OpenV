// The MCP tools over baselines. Tools, in tools.go, concatenates each
// area's constructor in the table's order.

package mcp

// baselineTools returns the baseline tools, in Tools() order.
func baselineTools() []Tool {
	return []Tool{
		{
			Name:        "list_baselines",
			ReadOnly:    true,
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
			ReadOnly:    true,
			Description: "Get a baseline's details by ID (name and capture time). The snapshot itself is not returned — read a baseline's content with get_project_map, passing baseline_id.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id": str("Baseline ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/baselines/"+strArg(args, "id"), nil, nil)
				if err != nil {
					return out, err
				}
				return baselineSummary(out)
			},
		},
		{
			Name:        "create_baseline",
			Description: "Capture a baseline: an immutable snapshot of the project's artifacts and links. Capture one after a coherent set of changes has landed, so later work can be compared against it.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"name":       str("Optional baseline name (the server names a dated one when omitted)"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("POST", "/api/v1/projects/"+strArg(args, "project_id")+"/baselines", nil, map[string]interface{}{
					"name": strArg(args, "name"),
				})
				if err != nil {
					return out, err
				}
				return baselineSummary(out)
			},
		},
	}
}
