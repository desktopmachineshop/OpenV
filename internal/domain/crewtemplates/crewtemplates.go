// Package crewtemplates serializes a crew (team) graph into a portable,
// org-independent JSON document and resolves it back into a concrete crew when
// imported into another organization.
//
// Unlike teams.CloneTeam — which copies a crew's nodes/edges under fresh IDs
// but stays in the source org because it reuses the same agent IDs — a portable
// crew references its agents BY SLUG. Agents are seeded per-org with stable
// slugs (see internal/seeds), so an importing org resolves each slug to its own
// agent, making crews shareable across organizations.
package crewtemplates

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/teams"
)

// Kind/version tag the document so importers can recognize and evolve it.
const (
	Kind    = "openv.crew"
	Version = "1.0"
)

// PortableCrew is the org-independent serialization of a crew graph. Nodes
// reference agents by slug; edges reference nodes by their local key. Only
// agent nodes are portable — human nodes point at org-specific users and are
// dropped on export (see Serialize).
type PortableCrew struct {
	Kind         string         `json:"kind"`
	Version      string         `json:"version"`
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	EntryNodeKey string         `json:"entry_node_key,omitempty"`
	Nodes        []PortableNode `json:"nodes"`
	Edges        []PortableEdge `json:"edges"`
	ExportedAt   time.Time      `json:"exported_at,omitempty"`
}

// PortableNode is one agent placed on the crew. Key is a document-local
// identifier (edges reference it); AgentSlug resolves to the importing org's
// agent registry.
type PortableNode struct {
	Key        string                 `json:"key"`
	AgentSlug  string                 `json:"agent_slug"`
	Label      string                 `json:"label"`
	Department string                 `json:"department,omitempty"`
	Position   map[string]interface{} `json:"position,omitempty"`
}

// PortableEdge is a typed relation between two nodes, addressed by their keys.
type PortableEdge struct {
	FromKey  string                 `json:"from"`
	ToKey    string                 `json:"to"`
	EdgeType string                 `json:"edge_type"`
	Config   map[string]interface{} `json:"config,omitempty"`
}

// AgentDirectory resolves agents for export (by global id) and import (by slug
// within the target org). agents.Service satisfies this interface.
type AgentDirectory interface {
	Get(id string) (*agents.Agent, error)
	GetBySlug(orgID, slug string) (*agents.Agent, error)
}

// CrewWriter is the subset of teams.Service used to materialize an imported
// crew. teams.Service satisfies this interface.
type CrewWriter interface {
	CreateTeam(orgID, name, description string, projectID *string) (*teams.Team, error)
	AddNode(teamID string, spec teams.NodeSpec) (*teams.Node, error)
	AddEdge(teamID, fromNodeID, toNodeID, edgeType string, config map[string]interface{}) (*teams.Edge, error)
	UpdateTeam(id string, name, description, entryNodeID *string) (*teams.Team, error)
}

// keyFor returns a document-unique key derived from a human-readable base,
// disambiguating collisions with a numeric suffix.
func keyFor(base string, used map[string]bool) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "node"
	}
	k := base
	for i := 2; used[k]; i++ {
		k = fmt.Sprintf("%s-%d", base, i)
	}
	used[k] = true
	return k
}

// Serialize converts a crew graph into a portable document. Human nodes and any
// edges touching them are omitted, since human identities are not portable
// across orgs. Agent nodes whose agent can no longer be resolved (e.g. a
// deleted agent) are likewise dropped along with their edges.
func Serialize(graph *teams.TeamGraph, dir AgentDirectory) (*PortableCrew, error) {
	if graph == nil || graph.Team == nil {
		return nil, errors.New("crew graph is required")
	}

	out := &PortableCrew{
		Kind:        Kind,
		Version:     Version,
		Name:        graph.Team.Name,
		Description: graph.Team.Description,
		Nodes:       []PortableNode{},
		Edges:       []PortableEdge{},
		ExportedAt:  time.Now().UTC(),
	}

	used := map[string]bool{}
	nodeKey := make(map[string]string, len(graph.Nodes)) // node id -> portable key
	for _, n := range graph.Nodes {
		if n.IsHuman() || n.AgentID == "" {
			continue
		}
		agent, err := dir.Get(n.AgentID)
		if err != nil {
			return nil, fmt.Errorf("resolve agent %s: %w", n.AgentID, err)
		}
		if agent == nil || agent.Slug == "" {
			continue // agent gone; the node isn't portable
		}
		key := keyFor(n.Label, used)
		nodeKey[n.ID] = key
		out.Nodes = append(out.Nodes, PortableNode{
			Key:        key,
			AgentSlug:  agent.Slug,
			Label:      n.Label,
			Department: n.Department,
			Position:   n.Position,
		})
	}

	for _, e := range graph.Edges {
		from, okFrom := nodeKey[e.FromNodeID]
		to, okTo := nodeKey[e.ToNodeID]
		if !okFrom || !okTo {
			continue // edge references a dropped (human/unresolved) node
		}
		out.Edges = append(out.Edges, PortableEdge{
			FromKey:  from,
			ToKey:    to,
			EdgeType: e.EdgeType,
			Config:   e.Config,
		})
	}

	if graph.Team.EntryNodeID != nil {
		if key, ok := nodeKey[*graph.Team.EntryNodeID]; ok {
			out.EntryNodeKey = key
		}
	}
	return out, nil
}

// ImportResult reports the crew created by Import plus any non-fatal warnings
// (e.g. nodes skipped because their agent slug is absent in the target org).
type ImportResult struct {
	Team     *teams.Team `json:"team"`
	Warnings []string    `json:"warnings,omitempty"`
}

// Import resolves a portable crew into a concrete crew in orgID.
//
// Missing-agent behavior: a node whose agent_slug does not exist in the target
// org is SKIPPED with a warning (rather than failing the whole import), and any
// edge touching a skipped node is dropped with its own warning. This mirrors
// how file-based project templates log-and-continue, and keeps a shared preset
// importable even when the target org has customized or removed some agents.
// The created crew and the collected warnings are returned so the caller can
// surface them to the user.
func Import(p *PortableCrew, orgID string, projectID *string, dir AgentDirectory, crew CrewWriter) (*ImportResult, error) {
	if p == nil {
		return nil, errors.New("crew document is required")
	}
	if p.Kind != "" && p.Kind != Kind {
		return nil, fmt.Errorf("unrecognized crew document kind %q", p.Kind)
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return nil, errors.New("crew name is required")
	}
	if orgID == "" {
		return nil, errors.New("organization id is required")
	}

	team, err := crew.CreateTeam(orgID, name, p.Description, projectID)
	if err != nil {
		return nil, err
	}

	res := &ImportResult{Team: team}
	keyToNodeID := make(map[string]string, len(p.Nodes))
	for _, pn := range p.Nodes {
		agent, err := dir.GetBySlug(orgID, pn.AgentSlug)
		if err != nil {
			return nil, fmt.Errorf("resolve agent slug %q: %w", pn.AgentSlug, err)
		}
		if agent == nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"agent %q was not found in this workspace; skipped node %q", pn.AgentSlug, pn.Label))
			continue
		}
		label := strings.TrimSpace(pn.Label)
		if label == "" {
			label = agent.Name
		}
		if label == "" {
			label = agent.Slug
		}
		node, err := crew.AddNode(team.ID, teams.NodeSpec{
			NodeType:   teams.NodeAgent,
			AgentID:    agent.ID,
			Label:      label,
			Department: pn.Department,
			Position:   pn.Position,
		})
		if err != nil {
			return nil, fmt.Errorf("add node %q: %w", label, err)
		}
		keyToNodeID[pn.Key] = node.ID
	}

	for _, pe := range p.Edges {
		fromID, okFrom := keyToNodeID[pe.FromKey]
		toID, okTo := keyToNodeID[pe.ToKey]
		if !okFrom || !okTo {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"edge %s %q->%q was dropped because an endpoint node was not imported",
				pe.EdgeType, pe.FromKey, pe.ToKey))
			continue
		}
		if _, err := crew.AddEdge(team.ID, fromID, toID, pe.EdgeType, pe.Config); err != nil {
			// A structurally invalid edge shouldn't abort the whole import;
			// keep the crew and report the rejected connection.
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"edge %s %q->%q was rejected: %v", pe.EdgeType, pe.FromKey, pe.ToKey, err))
		}
	}

	if p.EntryNodeKey != "" {
		if entryID, ok := keyToNodeID[p.EntryNodeKey]; ok {
			if _, err := crew.UpdateTeam(team.ID, nil, nil, &entryID); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("could not set entry node: %v", err))
			}
		}
	}

	return res, nil
}
