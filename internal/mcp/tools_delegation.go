// The MCP tool that delegates a task to a team delegate. Tools, in
// tools.go, concatenates each area's constructor in the table's order.

package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// delegationTools returns the delegation tool.
func delegationTools() []Tool {
	return []Tool{
		{
			Name:        "delegate_to_agent",
			Description: "Delegate a task to one of this agent's team delegates by role label, then wait for the result (polls up to 30 minutes).",
			InputSchema: schema([]string{"role_label", "prompt"}, map[string]interface{}{
				"role_label": str("Delegate's role label in the team"),
				"prompt":     str("Task prompt for the delegate"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("POST", "/api/v1/agent-runs/delegate", nil, map[string]interface{}{
					"role_label": strArg(args, "role_label"),
					"prompt":     strArg(args, "prompt"),
				})
				if err != nil {
					return out, err
				}
				var launched struct {
					RunID  string `json:"run_id"`
					Status string `json:"status"`
				}
				if err := json.Unmarshal([]byte(out), &launched); err != nil || launched.RunID == "" {
					return "", errors.New("unexpected delegate response: " + out)
				}

				deadline := time.Now().Add(30 * time.Minute)
				for time.Now().Before(deadline) {
					time.Sleep(5 * time.Second)
					body, _, err := c.request("GET", "/api/v1/agent-runs/delegate/"+launched.RunID, nil, nil)
					if err != nil {
						return body, err
					}
					var st struct {
						RunID     string `json:"run_id"`
						Status    string `json:"status"`
						FinalText string `json:"final_text"`
						Error     string `json:"error"`
					}
					if err := json.Unmarshal([]byte(body), &st); err != nil {
						return "", errors.New("unexpected delegate status response: " + body)
					}
					switch st.Status {
					case "succeeded", "awaiting_approval":
						text := st.FinalText
						if text == "" {
							text = "(delegate finished with no final text)"
						}
						if st.Status == "awaiting_approval" {
							text += "\n\n(Note: the delegate's changes are proposals awaiting human approval.)"
						}
						return text, nil
					case "failed", "cancelled", "timed_out":
						detail := st.Error
						if detail == "" {
							detail = "no error detail"
						}
						return "", fmt.Errorf("delegated run %s %s: %s", st.RunID, st.Status, detail)
					}
				}
				return "", errors.New("delegated run " + launched.RunID + " did not finish within 30 minutes")
			},
		},
	}
}
