// The MCP tools over a project's target releases: the list and its edits,
// an artifact's release range, and each release's progress. They follow
// vvTools in Tools().

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// releaseTools returns the target release tools, in Tools() order.
func releaseTools() []Tool {
	return []Tool{
		{
			Name:        "list_releases",
			ReadOnly:    true,
			Description: "A project's target releases (such as MVP, V1.0) in order, each with its id, name, target date and status (planned, in_progress or released). A requirement introduced in a release is in every later one, until the last release it targets, if it has one.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/projects/"+strArg(args, "project_id")+"/releases", nil, nil)
				return out, err
			},
		},
		{
			Name:        "create_release",
			Description: "Add a target release to the end of a project's release order.",
			InputSchema: schema([]string{"project_id", "name"}, map[string]interface{}{
				"project_id":  str("Project ID"),
				"name":        str("Release name, unique in the project (for example MVP or V1.0)"),
				"description": str("Optional description"),
				"target_date": str("Optional planned date, YYYY-MM-DD"),
				"status":      str("planned (default), in_progress or released"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				body := map[string]interface{}{"name": strArg(args, "name"), "description": strArg(args, "description"),
					"status": strArg(args, "status")}
				if d := strArg(args, "target_date"); d != "" {
					body["target_date"] = d
				}
				out, _, err := c.request("POST", "/api/v1/projects/"+strArg(args, "project_id")+"/releases", nil, body)
				return out, err
			},
		},
		{
			Name:        "update_release",
			Description: "Change a target release: its name, description, target date (an empty string clears it) or status. Omitted fields are kept.",
			InputSchema: schema([]string{"project_id", "release_id"}, map[string]interface{}{
				"project_id":  str("Project ID"),
				"release_id":  str("Release ID"),
				"name":        str("New name"),
				"description": str("New description"),
				"target_date": str("New planned date, YYYY-MM-DD, or an empty string to clear it"),
				"status":      str("planned, in_progress or released"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				body := map[string]interface{}{}
				for _, k := range []string{"name", "description", "target_date", "status"} {
					if v, ok := args[k].(string); ok {
						body[k] = v
					}
				}
				out, _, err := c.request("PUT", "/api/v1/projects/"+strArg(args, "project_id")+"/releases/"+
					strArg(args, "release_id"), nil, body)
				return out, err
			},
		},
		{
			Name:        "get_release_progress",
			ReadOnly:    true,
			Description: "Progress against target releases. Without release_id: every release's counts (requirements in it, introduced in it, approved, verified, ready, and by V&V rollup) and the requirements no release targets. With release_id: that release's counts plus one line per requirement (ref, title, review status, rollup, release range); pass detail=true to add each requirement's test cases and latest results and the day-by-day burn-up.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"release_id": str("Release ID; omit for the overview of every release"),
				"detail":     boolean("With release_id: return the full progress, test results and burn-up included"),
			}),
			Handler: getReleaseProgress,
		},
		{
			Name:        "set_release_target",
			Description: "Set the target releases of an artifact: the release it is introduced in (release) and, optionally, the last release it is in (until); it is in every release between them, or every later one when until is empty. Releases are named by id or name. clear=true removes the artifact's own targeting, so it inherits its parent's again. The artifact's other attributes are kept.",
			InputSchema: schema([]string{"artifact_id"}, map[string]interface{}{
				"artifact_id": str("Artifact ID"),
				"release":     str("First release, by id or name"),
				"until":       str("Optional last release, by id or name"),
				"clear":       boolean("Remove the artifact's own targeting instead"),
			}),
			Handler: setReleaseTarget,
		},
	}
}

// setReleaseTarget writes an artifact's targeting attributes, keeping its
// other attributes: the API replaces the attributes map whole.
func setReleaseTarget(c *Client, args map[string]interface{}) (string, error) {
	id := strArg(args, "artifact_id")
	raw, _, err := c.request("GET", "/api/v1/artifacts/"+id, nil, nil)
	if err != nil {
		return raw, err
	}
	var art struct {
		ProjectID  string                 `json:"project_id"`
		Attributes map[string]interface{} `json:"attributes"`
	}
	if err := json.Unmarshal([]byte(raw), &art); err != nil {
		return "", fmt.Errorf("unexpected artifact response: %v", err)
	}
	attrs := art.Attributes
	if attrs == nil {
		attrs = map[string]interface{}{}
	}
	delete(attrs, "target_release")
	delete(attrs, "target_release_until")
	if !boolArg(args, "clear") {
		list, _, err := c.request("GET", "/api/v1/projects/"+art.ProjectID+"/releases", nil, nil)
		if err != nil {
			return list, err
		}
		releases, err := decodeList(list)
		if err != nil {
			return "", err
		}
		resolve := func(v string) (string, error) {
			for _, r := range releases {
				if r["id"] == v || strings.EqualFold(fmt.Sprint(r["name"]), v) {
					return fmt.Sprint(r["id"]), nil
				}
			}
			return "", fmt.Errorf("no release %q in the project; list_releases names them", v)
		}
		from, err := resolve(strArg(args, "release"))
		if err != nil {
			return "", err
		}
		attrs["target_release"] = from
		if u := strArg(args, "until"); u != "" {
			until, err := resolve(u)
			if err != nil {
				return "", err
			}
			attrs["target_release_until"] = until
		}
	}
	out, _, err := c.request("PUT", "/api/v1/artifacts/"+id, nil, map[string]interface{}{"attributes": attrs})
	return out, err
}

// getReleaseProgress reads the overview, or one release's progress, lean
// unless detail is set.
func getReleaseProgress(c *Client, args map[string]interface{}) (string, error) {
	base := "/api/v1/projects/" + strArg(args, "project_id") + "/vv/releases"
	id := strArg(args, "release_id")
	if id == "" {
		out, _, err := c.request("GET", base, nil, nil)
		return out, err
	}
	out, _, err := c.request("GET", base+"/"+id, nil, nil)
	if err != nil || boolArg(args, "detail") {
		return out, err
	}
	// Lean by default (REQ-20), as get_vv_coverage is.
	var p struct {
		Release map[string]interface{} `json:"release"`
		Summary map[string]interface{} `json:"summary"`
		Entries []struct {
			RequirementID string                 `json:"requirement_id"`
			Ref           string                 `json:"ref"`
			Title         string                 `json:"title"`
			Status        string                 `json:"status"`
			Rollup        string                 `json:"rollup"`
			Range         map[string]interface{} `json:"range"`
			Introduced    bool                   `json:"introduced"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		return "", fmt.Errorf("unexpected release progress response: %v", err)
	}
	return toJSON(p)
}
