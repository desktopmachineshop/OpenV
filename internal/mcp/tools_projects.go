// The MCP tools over the projects an agent can see. Tools, in tools.go,
// concatenates each area's constructor in the table's order.

package mcp

// projectTools returns the project tools, in Tools() order.
func projectTools() []Tool {
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
	}
}
