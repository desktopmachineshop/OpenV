// The MCP tools over trace links between artifacts. Tools, in tools.go,
// concatenates each area's constructor in the table's order.

package mcp

import (
	"net/http"
	"net/url"
)

// linkTools returns the trace link tools, in Tools() order.
func linkTools() []Tool {
	return []Tool{
		{
			Name:        "create_link",
			Description: "Create a typed link between two artifacts. In proposal mode from_id and/or to_id may be a temporary ref token that a create_artifact in the SAME run assigned via its `ref` argument, letting you link to an artifact whose own proposal is not yet approved; the token resolves to the real id when both proposals are approved (the artifact one first). A 202 response means the link was diverted to a proposal pending human review.",
			InputSchema: schema([]string{"from_id", "to_id", "type"}, map[string]interface{}{
				"from_id": str("Source artifact ID, or a ref token from a create_artifact in this run"),
				"to_id":   str("Target artifact ID, or a ref token from a create_artifact in this run"),
				"type":    str("Link type"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, status, err := c.request("POST", "/api/v1/links", nil, map[string]interface{}{
					"from_id": strArg(args, "from_id"),
					"to_id":   strArg(args, "to_id"),
					"type":    strArg(args, "type"),
				})
				if err != nil {
					return out, err
				}
				if status == http.StatusAccepted {
					return "Proposal created (pending human review, not yet applied): " + out, nil
				}
				return out, nil
			},
		},
		{
			Name:        "delete_link",
			Description: "Delete a link by ID.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id": str("Link ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("DELETE", "/api/v1/links/"+strArg(args, "id"), nil, nil)
				if err != nil {
					return out, err
				}
				if out == "" {
					out = "link deleted"
				}
				return out, nil
			},
		},
		{
			Name:        "confirm_link",
			Description: "Clear the suspect flag on one link, vouching that the trace still holds after an artifact at one of its ends changed. Re-read both ends before calling it: the flag exists to make someone look, and clearing it is the assertion that they did. Idempotent — confirming a link that is not suspect changes nothing. list_links_for_artifact reports `suspect` per link, which is how you find the ones waiting. Proposal-mode agent runs are refused (403) rather than diverted to a proposal: this is a human sign-off, and routing it through a proposal would defeat the review the flag is there to trigger.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id": str("Link ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("PUT", "/api/v1/links/"+strArg(args, "id")+"/confirm", nil, nil)
				return out, err
			},
		},
		{
			Name:        "list_links_for_artifact",
			ReadOnly:    true,
			Description: "List all links touching one artifact.",
			InputSchema: schema([]string{"artifact_id", "project_id"}, map[string]interface{}{
				"artifact_id": str("Artifact ID"),
				"project_id":  str("Project ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				q := url.Values{"project_id": {strArg(args, "project_id")}}
				out, _, err := c.request("GET", "/api/v1/links", q, nil)
				if err != nil {
					return out, err
				}
				list, err := decodeList(out)
				if err != nil {
					return "", err
				}
				id := strArg(args, "artifact_id")
				matches := []map[string]interface{}{}
				for _, l := range list {
					if l["from_id"] == id || l["to_id"] == id {
						matches = append(matches, l)
					}
				}
				return toJSON(matches)
			},
		},
	}
}
