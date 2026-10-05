// The MCP tools over baselines. Tools, in tools.go, concatenates each
// area's constructor in the table's order.

package mcp

import (
	"encoding/json"
	"fmt"
)

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
				return getBaseline(c, strArg(args, "id"))
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

// getBaseline answers get_baseline with the baseline's id, project, name and
// capture time. GET /baselines/{id} answers with the baseline's stored
// snapshot, which names its project but not the baseline (#379 bug 202), so
// the rest is read from that project's baseline list, the one list_baselines
// reads. The first read still decides: an unknown id, or one the caller may
// not read, is its 404.
func getBaseline(c *Client, id string) (string, error) {
	out, _, err := c.request("GET", "/api/v1/baselines/"+id, nil, nil)
	if err != nil {
		return out, err
	}
	var snapshot struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(out), &snapshot); err != nil {
		return "", fmt.Errorf("unexpected baseline response: %v", err)
	}
	if snapshot.ProjectID == "" {
		return "", fmt.Errorf("baseline %s: its snapshot names no project, so its name and capture time cannot be looked up", id)
	}
	out, _, err = c.request("GET", "/api/v1/projects/"+snapshot.ProjectID+"/baselines", nil, nil)
	if err != nil {
		return out, err
	}
	var list []json.RawMessage
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return "", fmt.Errorf("unexpected baseline list response: %v", err)
	}
	for _, entry := range list {
		var b struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(entry, &b) == nil && b.ID == id {
			return baselineSummary(string(entry))
		}
	}
	return "", fmt.Errorf("baseline %s is not in the baseline list of its project %s", id, snapshot.ProjectID)
}
