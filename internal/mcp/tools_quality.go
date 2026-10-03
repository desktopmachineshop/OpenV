// The MCP tools over requirement quality rules and findings. Tools, in
// tools.go, concatenates each area's constructor in the table's order.

package mcp

import (
	"encoding/json"
	"fmt"
)

// qualityTools returns the requirement quality tools, in Tools() order.
func qualityTools() []Tool {
	return []Tool{
		{
			Name:        "get_quality_rules",
			ReadOnly:    true,
			Description: "The requirement quality rules this project is judged against: its normative convention (\"shall\" or RFC 2119 must/should/may) and the severity of each lint rule, plus a one-sentence summary. Read this before drafting or rewording a requirement so the wording matches the project's house style.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/projects/"+strArg(args, "project_id")+"/quality-rules", nil, nil)
				if err != nil {
					return out, err
				}
				// The endpoint also carries the editor's catalog (every
				// convention, rule and label); an agent needs the resolved
				// rules and the sentence, not the picker's vocabulary.
				var payload struct {
					Effective map[string]interface{} `json:"effective"`
					Summary   string                 `json:"summary"`
				}
				if err := json.Unmarshal([]byte(out), &payload); err != nil {
					return "", fmt.Errorf("unexpected quality rules response: %v", err)
				}
				return toJSON(payload)
			},
		},
		{
			Name:        "get_quality_findings",
			ReadOnly:    true,
			Description: "Lint one requirement or user need against the project's quality rules: a 0-100 score, its band, and one finding per wording problem (weak words, vague quantifiers, placeholders, passive voice, off-convention keywords, over-long sentences, untestable phrasing). Advisory — findings never block a write.",
			InputSchema: schema([]string{"artifact_id"}, map[string]interface{}{
				"artifact_id": str("Artifact ID or stable ref (pass project_id to resolve a ref)"),
				"project_id":  str("Project ID, required when artifact_id is a stable ref"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				id, err := resolveArtifactID(c, strArg(args, "project_id"), strArg(args, "artifact_id"))
				if err != nil {
					return "", err
				}
				out, _, err := c.request("GET", "/api/v1/artifacts/"+id+"/quality", nil, nil)
				return out, err
			},
		},
	}
}
