// The MCP tool an interviewer records a candidate user need with. Tools,
// in tools.go, concatenates each area's constructor in the table's order.

package mcp

import (
	"net/http"
	"strings"
)

// interviewTools returns the interview tool.
func interviewTools() []Tool {
	return []Tool{
		{
			Name:        "record_candidate_need",
			Description: "Record a candidate user need discovered during an interview. Stored as a draft user-need artifact for later review.",
			InputSchema: schema([]string{"project_id", "need"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"need":       str("Short statement of the user need"),
				"rationale":  str("Optional rationale"),
				"quote":      str("Optional supporting quote from the interviewee"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				var parts []string
				if r := strArg(args, "rationale"); r != "" {
					parts = append(parts, "Rationale: "+r)
				}
				if q := strArg(args, "quote"); q != "" {
					parts = append(parts, "Quote: \""+q+"\"")
				}
				out, status, err := c.request("POST", "/api/v1/artifacts", nil, map[string]interface{}{
					"project_id": strArg(args, "project_id"),
					"type":       "user-need",
					"title":      strArg(args, "need"),
					"body":       strings.Join(parts, "\n\n"),
				})
				if err != nil {
					return out, err
				}
				if status == http.StatusAccepted {
					return "Candidate need recorded as a proposal (pending review): " + out, nil
				}
				return out, nil
			},
		},
	}
}
