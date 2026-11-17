// The MCP tools over the projects an agent can see. Tools, in tools.go,
// concatenates each area's constructor in the table's order.

package mcp

import (
	"encoding/json"
	"fmt"
)

// projectTools returns the project tools, in Tools() order.
func projectTools() []Tool {
	return []Tool{
		{
			Name:        "list_projects",
			ReadOnly:    true,
			Description: "List all projects visible to this agent.",
			InputSchema: schema(nil, map[string]interface{}{}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/projects", nil, nil)
				return out, err
			},
		},
		{
			Name: "create_project",
			Description: "Create a project in the workspace. Give parent_project_id to file it under another project of the same workspace, so its requirements can refine that project's and its verification rolls up there. " +
				"Works with a runner key and for a signed-in person; an agent run acts only inside its own project and is refused.",
			InputSchema: schema([]string{"name"}, map[string]interface{}{
				"name":              str("Project name"),
				"description":       str("Optional description: what the project defines"),
				"parent_project_id": str("Optional ID of the project this one belongs under"),
			}),
			Handler: createProject,
		},
	}
}

// createProject answers create_project. A project is created and then filed
// under its parent, as the app does, since POST /projects takes no parent.
// When the second step is refused the project exists all the same, so the
// answer says so and carries its id rather than reading as nothing made.
func createProject(c *Client, args map[string]interface{}) (string, error) {
	out, _, err := c.request("POST", "/api/v1/projects", nil, map[string]interface{}{
		"name":        strArg(args, "name"),
		"description": strArg(args, "description"),
	})
	parent := strArg(args, "parent_project_id")
	if err != nil || parent == "" {
		return out, err
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil || created.ID == "" {
		return out, fmt.Errorf("the project was created, but its id could not be read to file it under %s", parent)
	}
	filed, _, err := c.request("PUT", "/api/v1/projects/"+created.ID, nil, map[string]interface{}{
		"parent_project_id": parent,
	})
	if err != nil {
		return out, fmt.Errorf("project %s was created, but could not be filed under %s: %w", created.ID, parent, err)
	}
	return filed, nil
}
