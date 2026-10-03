// The MCP tools that read, search, create and update artifacts. Tools, in
// tools.go, concatenates each area's constructor in the table's order.

package mcp

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// artifactReadTools returns the tools that list a project's artifacts and
// read one, in Tools() order.
func artifactReadTools() []Tool {
	return []Tool{
		{
			Name:        "list_artifacts",
			ReadOnly:    true,
			Description: "List artifacts in a project, optionally filtered by type and by owner (the \"owner\" attribute: a person, team or supplier).",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"type":       str("Optional artifact type filter"),
				"owner":      str("Optional owner filter: only artifacts whose owner attribute equals this"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				q := url.Values{"project_id": {strArg(args, "project_id")}}
				if t := strArg(args, "type"); t != "" {
					q.Set("type", t)
				}
				if o := strArg(args, "owner"); o != "" {
					q.Set("owner", o)
				}
				out, _, err := c.request("GET", "/api/v1/artifacts", q, nil)
				return out, err
			},
		},
		{
			Name:        "get_artifact",
			ReadOnly:    true,
			Description: "Get a single artifact by ID, including its body. Accepts a stable ref (e.g. \"REQ-12\") instead of a UUID when project_id is also given.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id":         str("Artifact ID (UUID), or a stable ref like REQ-12 (requires project_id)"),
				"project_id": str("Project ID; required only when id is a stable ref"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				id, err := resolveArtifactID(c, strArg(args, "project_id"), strArg(args, "id"))
				if err != nil {
					return "", err
				}
				out, _, err := c.request("GET", "/api/v1/artifacts/"+id, nil, nil)
				return out, err
			},
		},
	}
}

// artifactOverviewTools returns the tools that read a project's artifacts
// as a map or a tree, or one artifact with its context, in Tools() order.
func artifactOverviewTools() []Tool {
	return []Tool{
		{
			Name:        "get_project_map",
			ReadOnly:    true,
			Description: "The project's AI map: every artifact as one outline line — stable ref, title, status, hierarchy, and inline link annotations, no bodies. Roughly 10x fewer tokens than list_artifacts; use it to orient, then pull bodies for specific artifacts with get_artifact or get_context. Pass baseline_id for the map as of a baseline/release.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id":  str("Project ID"),
				"baseline_id": str("Optional baseline ID to render the map from that snapshot"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				var q url.Values
				if b := strArg(args, "baseline_id"); b != "" {
					q = url.Values{"baseline_id": {b}}
				}
				out, _, err := c.request("GET", "/api/v1/projects/"+strArg(args, "project_id")+"/ai-map", q, nil)
				return out, err
			},
		},
		{
			Name:        "get_context",
			ReadOnly:    true,
			Description: "One-call context bundle for an artifact: full body, ancestor path, children, and every linked artifact with a short excerpt — addressed by stable ref (REQ-12) or UUID. Replaces a get_artifact + list_links_for_artifact + N more get_artifact round trip.",
			InputSchema: schema([]string{"project_id", "id"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"id":         str("Artifact stable ref (e.g. REQ-12) or UUID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				return buildContextBundle(c, strArg(args, "project_id"), strArg(args, "id"))
			},
		},
		{
			Name:        "get_project_tree",
			ReadOnly:    true,
			Description: "Get the project's artifact tree: id, type, title, parent_id, sort_order per artifact (no bodies).",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				q := url.Values{"project_id": {strArg(args, "project_id")}}
				out, _, err := c.request("GET", "/api/v1/artifacts", q, nil)
				if err != nil {
					return out, err
				}
				list, err := decodeList(out)
				if err != nil {
					return "", err
				}
				tree := make([]map[string]interface{}, 0, len(list))
				for _, a := range list {
					tree = append(tree, map[string]interface{}{
						"id":         a["id"],
						"ref":        a["ref"],
						"type":       a["type"],
						"title":      a["title"],
						"parent_id":  a["parent_id"],
						"sort_order": a["sort_order"],
					})
				}
				return toJSON(tree)
			},
		},
	}
}

// artifactSearchTools returns the artifact search tool.
func artifactSearchTools() []Tool {
	return []Tool{
		{
			Name:        "search_artifacts",
			ReadOnly:    true,
			Description: "Case-insensitive substring search over artifact refs, titles and bodies in a project. A query that is a ref (\"REQ-30\", case-insensitive) returns that artifact first.",
			InputSchema: schema([]string{"project_id", "query"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"query":      str("Substring to search for, or a ref such as REQ-30"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				q := url.Values{"project_id": {strArg(args, "project_id")}}
				out, _, err := c.request("GET", "/api/v1/artifacts", q, nil)
				if err != nil {
					return out, err
				}
				list, err := decodeList(out)
				if err != nil {
					return "", err
				}
				raw := strings.TrimSpace(strArg(args, "query"))
				needle := strings.ToLower(raw)
				// Refs are how artifacts are addressed in prose and in every
				// tool that takes one, so a model searching "REQ-30" means
				// that artifact. Match refs as well as text, and put an exact
				// ref first: "REQ-3" must not be buried under "REQ-30".
				exactRef := artifacts.NormalizeRef(raw)
				matches := []map[string]interface{}{}
				for _, a := range list {
					title, _ := a["title"].(string)
					body, _ := a["body"].(string)
					ref, _ := a["ref"].(string)
					if !strings.Contains(strings.ToLower(ref), needle) &&
						!strings.Contains(strings.ToLower(title), needle) &&
						!strings.Contains(strings.ToLower(body), needle) {
						continue
					}
					hit := map[string]interface{}{
						"id":    a["id"],
						"ref":   a["ref"],
						"type":  a["type"],
						"title": a["title"],
					}
					if exactRef != "" && ref == exactRef {
						matches = append([]map[string]interface{}{hit}, matches...)
						continue
					}
					matches = append(matches, hit)
				}
				return toJSON(matches)
			},
		},
	}
}

// artifactCreateTools returns the artifact create tool.
func artifactCreateTools() []Tool {
	return []Tool{
		{
			Name:        "create_artifact",
			Description: "Create an artifact. A 202 response means the change was diverted to a proposal pending human review. In proposal mode you may pass a `ref`: a temporary token you choose (e.g. \"t1\") that names this not-yet-created artifact so a create_link in the SAME run can point at it via from_id/to_id. The token resolves to the real artifact id when the artifact proposal is approved.",
			InputSchema: schema([]string{"project_id", "type", "title"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"type":       str("Artifact type"),
				"title":      str("Title"),
				"body":       str("Optional markdown body"),
				"parent_id":  str("Optional parent artifact ID"),
				"attributes": obj("Optional attributes object"),
				"ref":        str("Optional temporary reference token (proposal mode only) to name this artifact for a sibling create_link in the same run"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				body := map[string]interface{}{
					"project_id": strArg(args, "project_id"),
					"type":       strArg(args, "type"),
					"title":      strArg(args, "title"),
					"body":       strArg(args, "body"),
				}
				if p := strArg(args, "parent_id"); p != "" {
					body["parent_id"] = p
				}
				if attrs, ok := args["attributes"].(map[string]interface{}); ok {
					body["attributes"] = attrs
				}
				ref := strArg(args, "ref")
				if ref != "" {
					body["ref"] = ref
				}
				out, status, err := c.request("POST", "/api/v1/artifacts", nil, body)
				if err != nil {
					return out, err
				}
				if status == http.StatusAccepted {
					msg := "Proposal created (pending human review, not yet applied): " + out
					if ref != "" {
						msg += "\nReference this artifact from a create_link in this run by passing from_id or to_id = \"" + ref + "\"."
					}
					return msg, nil
				}
				return out, nil
			},
		},
	}
}

// artifactUpdateTools returns the artifact update tool.
func artifactUpdateTools() []Tool {
	return []Tool{
		{
			Name:        "update_artifact",
			Description: "Update an artifact. Omitted optional fields are left unchanged. A 202 response means the change was diverted to a proposal pending human review.",
			InputSchema: schema([]string{"id"}, map[string]interface{}{
				"id":         str("Artifact ID"),
				"type":       str("Optional artifact type (omit to keep current)"),
				"title":      str("Optional title (omit to keep current)"),
				"body":       str("Optional markdown body (omit to keep current; pass \"\" to clear)"),
				"parent_id":  str("Optional new parent artifact ID (omit to keep current; pass \"\" to move to the top level)"),
				"attributes": obj("Optional attributes object (omit to keep current)"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				// Only fields the caller actually passed reach the payload:
				// the API treats an absent type/title/body (nil after
				// decode) as "no change", the same contract attributes
				// gained in issue #125. Sending "" for an omitted arg used
				// to wipe the artifact's body/title (issue #170).
				body := map[string]interface{}{}
				for _, key := range []string{"type", "title", "body"} {
					if _, present := args[key]; present {
						body[key] = strArg(args, key)
					}
				}
				// parent_id is presence-aware server-side (issue #172):
				// absent = keep the current parent, JSON null = move to the
				// top level, an ID = reparent. Map the tool's "" (and an
				// explicit null arg) to JSON null.
				if _, present := args["parent_id"]; present {
					if p := strArg(args, "parent_id"); p != "" {
						body["parent_id"] = p
					} else {
						body["parent_id"] = nil
					}
				}
				if attrs, ok := args["attributes"].(map[string]interface{}); ok {
					body["attributes"] = attrs
				}
				out, status, err := c.request("PUT", "/api/v1/artifacts/"+strArg(args, "id"), nil, body)
				if err != nil {
					return out, err
				}
				if status == http.StatusAccepted {
					return "Proposal created (pending human review, not yet applied): " + out, nil
				}
				return out, nil
			},
		},
	}
}
