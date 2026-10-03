// The MCP tool that comments on an artifact. Tools, in tools.go,
// concatenates each area's constructor in the table's order.

package mcp

// commentTools returns the comment tool.
func commentTools() []Tool {
	return []Tool{
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
	}
}
