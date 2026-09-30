package mcp

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// This file backs the AI context surface's read tools: stable-ref
// resolution for get_artifact, and the get_context bundle that packs an
// artifact's body, ancestry, children, and linked neighbors into one
// compact reply. The extra API round trips happen here, between the MCP
// host and the API — the model pays tokens only for the assembled result.

// resolveArtifactID maps a stable ref (REQ-12) to the artifact's UUID by
// scanning the project's artifact list; anything that does not parse as a
// ref (UUIDs, proposal temp tokens) passes through unchanged. Refs need
// projectID because they are only unique within a project.
func resolveArtifactID(c *Client, projectID, idOrRef string) (string, error) {
	if _, _, ok := artifacts.ParseRef(idOrRef); !ok {
		return idOrRef, nil
	}
	if projectID == "" {
		return "", fmt.Errorf("%q looks like a stable ref; pass project_id to resolve it", idOrRef)
	}
	list, err := fetchProjectArtifacts(c, projectID)
	if err != nil {
		return "", err
	}
	for _, a := range list {
		if ref, _ := a["ref"].(string); ref == idOrRef {
			id, _ := a["id"].(string)
			return id, nil
		}
	}
	return "", fmt.Errorf("no artifact with ref %q in project %s", idOrRef, projectID)
}

func fetchProjectArtifacts(c *Client, projectID string) ([]map[string]interface{}, error) {
	out, _, err := c.request("GET", "/api/v1/artifacts", url.Values{"project_id": {projectID}}, nil)
	if err != nil {
		return nil, err
	}
	return decodeList(out)
}

// buildContextBundle assembles the get_context reply. One artifact list and
// one link list feed everything: the target with full body, the ancestor
// path, children, and both link directions with neighbor excerpts.
func buildContextBundle(c *Client, projectID, idOrRef string) (string, error) {
	if projectID == "" {
		return "", fmt.Errorf("project_id is required")
	}
	arts, err := fetchProjectArtifacts(c, projectID)
	if err != nil {
		return "", err
	}
	linksOut, _, err := c.request("GET", "/api/v1/links", url.Values{"project_id": {projectID}}, nil)
	if err != nil {
		return "", err
	}
	lks, err := decodeList(linksOut)
	if err != nil {
		return "", err
	}

	byID := make(map[string]map[string]interface{}, len(arts))
	for _, a := range arts {
		if id, _ := a["id"].(string); id != "" {
			byID[id] = a
		}
	}

	// Locate the target by ref or UUID.
	var target map[string]interface{}
	for _, a := range arts {
		ref, _ := a["ref"].(string)
		id, _ := a["id"].(string)
		if (ref != "" && ref == idOrRef) || id == idOrRef {
			target = a
			break
		}
	}
	if target == nil {
		return "", fmt.Errorf("no artifact %q in project %s", idOrRef, projectID)
	}
	targetID, _ := target["id"].(string)

	label := func(a map[string]interface{}) string {
		ref, _ := a["ref"].(string)
		if ref == "" {
			if id, _ := a["id"].(string); len(id) >= 8 {
				ref = "#" + id[:8]
			}
		}
		title, _ := a["title"].(string)
		return ref + " " + title
	}
	excerpt := func(a map[string]interface{}) string {
		body, _ := a["body"].(string)
		body = strings.Join(strings.Fields(body), " ")
		if len(body) > 240 {
			body = body[:240] + "…"
		}
		return body
	}

	var b strings.Builder
	typ, _ := target["type"].(string)
	status, _ := target["status"].(string)
	fmt.Fprintf(&b, "%s (%s", label(target), typ)
	if status != "" {
		fmt.Fprintf(&b, ", %s", status)
	}
	b.WriteString(")\n")

	// Ancestor path, root first.
	var path []string
	cur := target
	for range arts { // bounded walk; a parent cycle cannot loop forever
		parentID, _ := cur["parent_id"].(string)
		if parentID == "" {
			break
		}
		parent, ok := byID[parentID]
		if !ok {
			break
		}
		path = append([]string{label(parent)}, path...)
		cur = parent
	}
	if len(path) > 0 {
		fmt.Fprintf(&b, "Path: %s\n", strings.Join(path, " > "))
	}

	body, _ := target["body"].(string)
	if strings.TrimSpace(body) != "" {
		fmt.Fprintf(&b, "\n%s\n", strings.TrimSpace(body))
	}

	var children []string
	for _, a := range arts {
		if pid, _ := a["parent_id"].(string); pid == targetID {
			children = append(children, label(a))
		}
	}
	if len(children) > 0 {
		sort.Strings(children)
		fmt.Fprintf(&b, "\nChildren: %s\n", strings.Join(children, "; "))
	}

	// Links with neighbor excerpts, deterministic order.
	type edge struct{ line, excerpt string }
	var edges []edge
	for _, l := range lks {
		fromID, _ := l["from_id"].(string)
		toID, _ := l["to_id"].(string)
		linkType, _ := l["type"].(string)
		suspect, _ := l["suspect"].(bool)
		mark := ""
		if suspect {
			mark = " (suspect)"
		}
		if fromID == targetID {
			if n, ok := byID[toID]; ok {
				edges = append(edges, edge{fmt.Sprintf("→ %s %s%s", linkType, label(n), mark), excerpt(n)})
			}
		} else if toID == targetID {
			if n, ok := byID[fromID]; ok {
				edges = append(edges, edge{fmt.Sprintf("← %s %s%s", linkType, label(n), mark), excerpt(n)})
			}
		}
	}
	if len(edges) > 0 {
		sort.Slice(edges, func(i, j int) bool { return edges[i].line < edges[j].line })
		b.WriteString("\nLinks (→ out, ← in):\n")
		for _, e := range edges {
			b.WriteString(e.line)
			b.WriteByte('\n')
			if e.excerpt != "" {
				fmt.Fprintf(&b, "  %s\n", e.excerpt)
			}
		}
	}

	return b.String(), nil
}
