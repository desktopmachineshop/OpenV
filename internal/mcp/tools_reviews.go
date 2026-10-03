// The MCP tool that starts a project review round. Tools, in tools.go,
// concatenates each area's constructor in the table's order.

package mcp

// reviewTools returns the review round tool.
func reviewTools() []Tool {
	return []Tool{
		{
			Name:        "start_project_review",
			Description: "Start a review round for a whole project: every artifact still in draft moves to in_review in one action. Re-runnable and safe to repeat — an approved artifact nobody has changed stays approved, while one whose content was edited since it was approved is already back in draft and so is pulled into review again. Covers every artifact type by default, headings and descriptions included; pass types to narrow it.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"types":      strList("Optional artifact types to review (default: every type in the catalog)"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("POST", "/api/v1/projects/"+strArg(args, "project_id")+"/review-round", nil, map[string]interface{}{
					"types": strListArg(args, "types"),
				})
				return out, err
			},
		},
	}
}
