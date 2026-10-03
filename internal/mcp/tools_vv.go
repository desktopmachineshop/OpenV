// The MCP tools over verification coverage and gaps. Tools, in tools.go,
// concatenates each area's constructor in the table's order.

package mcp

import (
	"encoding/json"
	"fmt"
)

// vvTools returns the verification coverage and gap tools, in Tools()
// order.
func vvTools() []Tool {
	return []Tool{
		{
			Name:        "get_vv_coverage",
			Description: "Verification coverage for a project: the rollup summary plus one line per requirement (verification method, status, rollup). Pass detail=true for the full report, which also carries each requirement's test cases and their latest results.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"detail":     boolean("Return the full report instead of the per-requirement summary lines"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/projects/"+strArg(args, "project_id")+"/vv/coverage", nil, nil)
				if err != nil {
					return out, err
				}
				if boolArg(args, "detail") {
					return out, nil
				}
				// Lean by default (REQ-20): the per-requirement test case IDs
				// and their result maps are the bulk of the report and are
				// rarely what the caller is after.
				var report struct {
					ProjectID string         `json:"project_id"`
					Summary   map[string]int `json:"summary"`
					Entries   []struct {
						RequirementID      string `json:"requirement_id"`
						Title              string `json:"title"`
						VerificationMethod string `json:"verification_method"`
						VerificationStatus string `json:"verification_status"`
						Rollup             string `json:"rollup"`
					} `json:"entries"`
				}
				if err := json.Unmarshal([]byte(out), &report); err != nil {
					return "", fmt.Errorf("unexpected coverage response: %v", err)
				}
				return toJSON(report)
			},
		},
		{
			Name:        "get_vv_gaps",
			Description: "Traceability and verification gaps in a project: requirements with no verification method, with no test case, unverified (requirements_unverified — method demonstration, analysis or inspection and not yet marked verified), or whose latest results fail, plus orphan test cases, user needs no requirement derives from, and unmitigated hazards. Each is a list of artifact IDs.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/projects/"+strArg(args, "project_id")+"/vv/gaps", nil, nil)
				return out, err
			},
		},
	}
}
