package teams

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Edge types.
const (
	EdgeDelegates = "delegates-to"
	EdgeHandsOff  = "hands-off-to"
	EdgeReviews   = "reviews"
)

// MaxDelegationDepth caps how deep a delegates-to chain may run from a root.
const MaxDelegationDepth = 3

// Node types: a crew node is either an AI agent or a human member.
const (
	NodeAgent = "agent"
	NodeHuman = "human"
)

// Team is a named graph of agents.
type Team struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	ProjectID   *string   `json:"project_id,omitempty"` // nil = global
	EntryNodeID *string   `json:"entry_node_id,omitempty"`
	IsDefault   bool      `json:"is_default"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Node places an agent or a human on a crew. Position stores per-layout
// coordinates, e.g. {"org":{"x":..,"y":..},"network":{...}}.
// AgentID is empty for human nodes; UserID is nil for agent nodes.
// UserName and UserAvatarURL are denormalized from the users table on read.
type Node struct {
	ID            string                 `json:"id"`
	TeamID        string                 `json:"team_id"`
	NodeType      string                 `json:"node_type"`
	AgentID       string                 `json:"agent_id"`
	UserID        *string                `json:"user_id"`
	UserName      string                 `json:"user_name,omitempty"`
	UserAvatarURL string                 `json:"user_avatar_url,omitempty"`
	Label         string                 `json:"label"`
	Department    string                 `json:"department"`
	Position      map[string]interface{} `json:"position"`
	CreatedAt     time.Time              `json:"created_at"`
}

// IsHuman reports whether the node represents a person. An empty NodeType is
// treated as an agent node (legacy rows predate the column).
func (n *Node) IsHuman() bool { return n.NodeType == NodeHuman }

// NodeSpec describes a node to add to a crew. NodeType defaults to NodeAgent.
type NodeSpec struct {
	NodeType   string
	AgentID    string
	UserID     string
	Label      string
	Department string
	Position   map[string]interface{}
}

// normalize defaults the node type and enforces the identity rules: agent
// nodes carry an agent_id and no user_id; human nodes the reverse.
func (spec *NodeSpec) normalize() error {
	if spec.NodeType == "" {
		spec.NodeType = NodeAgent
	}
	switch spec.NodeType {
	case NodeAgent:
		if spec.AgentID == "" {
			return errors.New("agent_id is required for agent nodes")
		}
		if spec.UserID != "" {
			return errors.New("user_id must be empty for agent nodes")
		}
	case NodeHuman:
		if spec.UserID == "" {
			return errors.New("user_id is required for human nodes")
		}
		if spec.AgentID != "" {
			return errors.New("agent_id must be empty for human nodes")
		}
	default:
		return fmt.Errorf("invalid node_type %q", spec.NodeType)
	}
	if spec.Label == "" {
		return errors.New("label is required")
	}
	return nil
}

// Edge is a typed relation between two team nodes.
type Edge struct {
	ID         string                 `json:"id"`
	TeamID     string                 `json:"team_id"`
	FromNodeID string                 `json:"from_node_id"`
	ToNodeID   string                 `json:"to_node_id"`
	EdgeType   string                 `json:"edge_type"`
	Config     map[string]interface{} `json:"config"`
	CreatedAt  time.Time              `json:"created_at"`
}

// TeamGraph is the full API payload for a team.
type TeamGraph struct {
	Team  *Team   `json:"team"`
	Nodes []*Node `json:"nodes"`
	Edges []*Edge `json:"edges"`
}

// Repository defines persistence operations for teams, nodes, and edges.
// Find methods return (nil, nil) when no row exists.
type Repository interface {
	SaveTeam(t *Team) error
	UpdateTeam(t *Team) error
	FindTeamByID(id string) (*Team, error)
	// ListTeams lists an org's teams; projectID "" lists all in the org,
	// otherwise the project's own plus global-in-org (project_id NULL) teams.
	ListTeams(orgID, projectID string) ([]*Team, error)
	DeleteTeam(id string) error
	FindDefaultTeam(orgID string) (*Team, error)
	MarkDefault(teamID string) error

	SaveNode(n *Node) error
	UpdateNode(n *Node) error
	FindNodeByID(id string) (*Node, error)
	ListNodesByTeam(teamID string) ([]*Node, error)
	DeleteNode(id string) error

	SaveEdge(e *Edge) error
	UpdateEdge(e *Edge) error
	FindEdgeByID(id string) (*Edge, error)
	ListEdgesByTeam(teamID string) ([]*Edge, error)
	DeleteEdge(id string) error
}

// Service defines team domain logic.
type Service interface {
	CreateTeam(orgID, name, description string, projectID *string) (*Team, error)
	GetTeam(id string) (*TeamGraph, error)
	ListTeams(orgID, projectID string) ([]*Team, error)
	UpdateTeam(id string, name, description, entryNodeID *string) (*Team, error)
	DeleteTeam(id string) error
	// MarkDefault flags a team as its org's seeded default.
	MarkDefault(teamID string) error

	AddNode(teamID string, spec NodeSpec) (*Node, error)
	GetNode(nodeID string) (*Node, error)
	UpdateNode(nodeID string, label, agentID, userID, department *string, position map[string]interface{}) (*Node, error)
	RemoveNode(nodeID string) error

	AddEdge(teamID, fromNodeID, toNodeID, edgeType string, config map[string]interface{}) (*Edge, error)
	GetEdge(edgeID string) (*Edge, error)
	UpdateEdge(edgeID string, config map[string]interface{}) (*Edge, error)
	RemoveEdge(edgeID string) error

	ValidateGraph(nodes []*Node, edges []*Edge) error
	ResolveDelegates(nodeID string) ([]*Node, error)
	SuccessorEdges(nodeID string, edgeType string) ([]*Edge, error)

	ExportTeam(id string) (*TeamGraph, error)
	CloneTeam(id, newName string, projectID *string) (*Team, error)
}

// DefaultService implements the Service interface.
type DefaultService struct {
	repo            Repository
	memberValidator func(orgID, userID string) bool
}

// NewDefaultService creates a new team service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// SetMemberValidator injects the org-membership check applied to human node
// identities. A nil validator skips the check.
func (s *DefaultService) SetMemberValidator(fn func(orgID, userID string) bool) {
	s.memberValidator = fn
}

// checkHumanMember verifies a human node's user belongs to the team's org.
func (s *DefaultService) checkHumanMember(orgID, userID string) error {
	if s.memberValidator == nil {
		return nil
	}
	if !s.memberValidator(orgID, userID) {
		return errors.New("user is not a member of this crew's workspace")
	}
	return nil
}

func validEdgeType(edgeType string) bool {
	return edgeType == EdgeDelegates || edgeType == EdgeHandsOff || edgeType == EdgeReviews
}

// ValidateGraph checks the structural rules of a team graph: no self-edges
// anywhere, and on the delegates-to subgraph: acyclic, at most one incoming
// delegates-to per node, and depth at most MaxDelegationDepth from any root.
func ValidateGraph(nodes []*Node, edges []*Edge) error {
	nodeSet := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		nodeSet[n.ID] = true
	}

	for _, e := range edges {
		if e.FromNodeID == e.ToNodeID {
			return fmt.Errorf("self-edge not allowed on node %s", e.FromNodeID)
		}
		if !nodeSet[e.FromNodeID] || !nodeSet[e.ToNodeID] {
			return fmt.Errorf("edge %s references a node outside the graph", e.ID)
		}
	}

	// Build the delegates-to subgraph.
	children := map[string][]string{}
	parent := map[string]string{}
	involved := map[string]bool{}
	for _, e := range edges {
		if e.EdgeType != EdgeDelegates {
			continue
		}
		if _, ok := parent[e.ToNodeID]; ok {
			return fmt.Errorf("node %s has more than one incoming delegates-to edge", e.ToNodeID)
		}
		parent[e.ToNodeID] = e.FromNodeID
		children[e.FromNodeID] = append(children[e.FromNodeID], e.ToNodeID)
		involved[e.FromNodeID] = true
		involved[e.ToNodeID] = true
	}

	// BFS from roots; single-parent means anything unreachable sits on a cycle.
	depth := map[string]int{}
	var queue []string
	for id := range involved {
		if _, hasParent := parent[id]; !hasParent {
			depth[id] = 0
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		if depth[id] > MaxDelegationDepth {
			return fmt.Errorf("delegation depth exceeds maximum of %d", MaxDelegationDepth)
		}
		for _, c := range children[id] {
			depth[c] = depth[id] + 1
			queue = append(queue, c)
		}
	}
	if visited < len(involved) {
		return errors.New("delegates-to graph contains a cycle")
	}
	return nil
}

// CreateTeam creates an empty team in an org.
func (s *DefaultService) CreateTeam(orgID, name, description string, projectID *string) (*Team, error) {
	if orgID == "" {
		return nil, errors.New("organization id is required")
	}
	if name == "" {
		return nil, errors.New("name is required")
	}
	now := time.Now()
	t := &Team{
		ID:          uuid.New().String(),
		OrgID:       orgID,
		Name:        name,
		Description: description,
		ProjectID:   projectID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.SaveTeam(t); err != nil {
		return nil, err
	}
	return t, nil
}

// GetTeam returns a team with its nodes and edges.
func (s *DefaultService) GetTeam(id string) (*TeamGraph, error) {
	t, err := s.repo.FindTeamByID(id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errors.New("team not found")
	}
	nodes, err := s.repo.ListNodesByTeam(id)
	if err != nil {
		return nil, err
	}
	edges, err := s.repo.ListEdgesByTeam(id)
	if err != nil {
		return nil, err
	}
	return &TeamGraph{Team: t, Nodes: nodes, Edges: edges}, nil
}

// ListTeams returns an org's teams, optionally narrowed to a project plus
// the org's global teams.
func (s *DefaultService) ListTeams(orgID, projectID string) ([]*Team, error) {
	return s.repo.ListTeams(orgID, projectID)
}

// UpdateTeam applies non-nil name/description/entry_node_id changes.
// An empty entryNodeID clears the entry node.
func (s *DefaultService) UpdateTeam(id string, name, description, entryNodeID *string) (*Team, error) {
	t, err := s.repo.FindTeamByID(id)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errors.New("team not found")
	}
	if name != nil {
		if *name == "" {
			return nil, errors.New("name is required")
		}
		t.Name = *name
	}
	if description != nil {
		t.Description = *description
	}
	if entryNodeID != nil {
		if *entryNodeID == "" {
			t.EntryNodeID = nil
		} else {
			n, err := s.repo.FindNodeByID(*entryNodeID)
			if err != nil {
				return nil, err
			}
			if n == nil || n.TeamID != id {
				return nil, errors.New("entry node does not belong to team")
			}
			if n.IsHuman() {
				return nil, errors.New("entry node must be an agent node")
			}
			v := *entryNodeID
			t.EntryNodeID = &v
		}
	}
	t.UpdatedAt = time.Now()
	if err := s.repo.UpdateTeam(t); err != nil {
		return nil, err
	}
	return t, nil
}

// DeleteTeam removes a team; nodes and edges cascade in the database.
func (s *DefaultService) DeleteTeam(id string) error {
	return s.repo.DeleteTeam(id)
}

// MarkDefault flags a team as its org's seeded default.
func (s *DefaultService) MarkDefault(teamID string) error {
	return s.repo.MarkDefault(teamID)
}

// AddNode places an agent or a human on a crew.
func (s *DefaultService) AddNode(teamID string, spec NodeSpec) (*Node, error) {
	if err := spec.normalize(); err != nil {
		return nil, err
	}
	t, err := s.repo.FindTeamByID(teamID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, errors.New("team not found")
	}
	var userID *string
	if spec.NodeType == NodeHuman {
		if err := s.checkHumanMember(t.OrgID, spec.UserID); err != nil {
			return nil, err
		}
		v := spec.UserID
		userID = &v
	}
	position := spec.Position
	if position == nil {
		position = map[string]interface{}{}
	}
	n := &Node{
		ID:         uuid.New().String(),
		TeamID:     teamID,
		NodeType:   spec.NodeType,
		AgentID:    spec.AgentID,
		UserID:     userID,
		Label:      spec.Label,
		Department: spec.Department,
		Position:   position,
		CreatedAt:  time.Now(),
	}
	if err := s.repo.SaveNode(n); err != nil {
		return nil, err
	}
	return n, nil
}

// UpdateNode applies non-nil label/agentID/department changes and a
// non-nil position.
// GetNode retrieves a team node by ID (nil, nil when none).
func (s *DefaultService) GetNode(nodeID string) (*Node, error) {
	return s.repo.FindNodeByID(nodeID)
}

func (s *DefaultService) UpdateNode(nodeID string, label, agentID, userID, department *string, position map[string]interface{}) (*Node, error) {
	n, err := s.repo.FindNodeByID(nodeID)
	if err != nil {
		return nil, err
	}
	if n == nil {
		return nil, errors.New("node not found")
	}
	if label != nil {
		if *label == "" {
			return nil, errors.New("label is required")
		}
		n.Label = *label
	}
	if agentID != nil {
		if n.IsHuman() {
			return nil, errors.New("cannot set agent_id on a human node")
		}
		if *agentID == "" {
			return nil, errors.New("agent_id is required")
		}
		n.AgentID = *agentID
	}
	if userID != nil {
		if !n.IsHuman() {
			return nil, errors.New("cannot set user_id on an agent node")
		}
		if *userID == "" {
			return nil, errors.New("user_id is required")
		}
		t, err := s.repo.FindTeamByID(n.TeamID)
		if err != nil {
			return nil, err
		}
		if t == nil {
			return nil, errors.New("team not found")
		}
		if err := s.checkHumanMember(t.OrgID, *userID); err != nil {
			return nil, err
		}
		v := *userID
		n.UserID = &v
	}
	if department != nil {
		n.Department = *department
	}
	if position != nil {
		n.Position = position
	}
	if err := s.repo.UpdateNode(n); err != nil {
		return nil, err
	}
	return n, nil
}

// RemoveNode deletes a node; its edges cascade in the database.
func (s *DefaultService) RemoveNode(nodeID string) error {
	return s.repo.DeleteNode(nodeID)
}

// AddEdge connects two nodes of a team, validating the resulting graph.
func (s *DefaultService) AddEdge(teamID, fromNodeID, toNodeID, edgeType string, config map[string]interface{}) (*Edge, error) {
	if !validEdgeType(edgeType) {
		return nil, fmt.Errorf("invalid edge type %q", edgeType)
	}
	if fromNodeID == toNodeID {
		return nil, errors.New("self-edges are not allowed")
	}

	from, err := s.repo.FindNodeByID(fromNodeID)
	if err != nil {
		return nil, err
	}
	if from == nil || from.TeamID != teamID {
		return nil, errors.New("from node does not belong to team")
	}
	to, err := s.repo.FindNodeByID(toNodeID)
	if err != nil {
		return nil, err
	}
	if to == nil || to.TeamID != teamID {
		return nil, errors.New("to node does not belong to team")
	}
	if edgeType == EdgeDelegates && to.IsHuman() {
		return nil, errors.New("people cannot be delegated to; use a hands-off edge")
	}

	if config == nil {
		config = map[string]interface{}{}
	}
	e := &Edge{
		ID:         uuid.New().String(),
		TeamID:     teamID,
		FromNodeID: fromNodeID,
		ToNodeID:   toNodeID,
		EdgeType:   edgeType,
		Config:     config,
		CreatedAt:  time.Now(),
	}

	nodes, err := s.repo.ListNodesByTeam(teamID)
	if err != nil {
		return nil, err
	}
	edges, err := s.repo.ListEdgesByTeam(teamID)
	if err != nil {
		return nil, err
	}
	if err := ValidateGraph(nodes, append(edges, e)); err != nil {
		return nil, err
	}

	if err := s.repo.SaveEdge(e); err != nil {
		return nil, err
	}
	return e, nil
}

// UpdateEdge replaces an edge's config.
// GetEdge retrieves a team edge by ID (nil, nil when none).
func (s *DefaultService) GetEdge(edgeID string) (*Edge, error) {
	return s.repo.FindEdgeByID(edgeID)
}

func (s *DefaultService) UpdateEdge(edgeID string, config map[string]interface{}) (*Edge, error) {
	e, err := s.repo.FindEdgeByID(edgeID)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errors.New("edge not found")
	}
	if config == nil {
		config = map[string]interface{}{}
	}
	e.Config = config
	if err := s.repo.UpdateEdge(e); err != nil {
		return nil, err
	}
	return e, nil
}

// RemoveEdge deletes an edge.
func (s *DefaultService) RemoveEdge(edgeID string) error {
	return s.repo.DeleteEdge(edgeID)
}

// ValidateGraph exposes the pure graph validation on the service.
func (s *DefaultService) ValidateGraph(nodes []*Node, edges []*Edge) error {
	return ValidateGraph(nodes, edges)
}

// ResolveDelegates returns the delegates-to children of a node.
func (s *DefaultService) ResolveDelegates(nodeID string) ([]*Node, error) {
	n, err := s.repo.FindNodeByID(nodeID)
	if err != nil {
		return nil, err
	}
	if n == nil {
		return nil, errors.New("node not found")
	}
	edges, err := s.repo.ListEdgesByTeam(n.TeamID)
	if err != nil {
		return nil, err
	}
	nodes, err := s.repo.ListNodesByTeam(n.TeamID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*Node, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	var result []*Node
	for _, e := range edges {
		if e.EdgeType == EdgeDelegates && e.FromNodeID == nodeID {
			// Only agent nodes are delegation targets; humans receive work
			// through hands-off/reviews cards instead.
			if child, ok := byID[e.ToNodeID]; ok && !child.IsHuman() {
				result = append(result, child)
			}
		}
	}
	return result, nil
}

// SuccessorEdges returns a node's outgoing edges of the given type.
func (s *DefaultService) SuccessorEdges(nodeID string, edgeType string) ([]*Edge, error) {
	n, err := s.repo.FindNodeByID(nodeID)
	if err != nil {
		return nil, err
	}
	if n == nil {
		return nil, errors.New("node not found")
	}
	edges, err := s.repo.ListEdgesByTeam(n.TeamID)
	if err != nil {
		return nil, err
	}
	var result []*Edge
	for _, e := range edges {
		if e.FromNodeID == nodeID && (edgeType == "" || e.EdgeType == edgeType) {
			result = append(result, e)
		}
	}
	return result, nil
}

// ExportTeam returns the full graph of a team for export.
func (s *DefaultService) ExportTeam(id string) (*TeamGraph, error) {
	return s.GetTeam(id)
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// CloneTeam deep-copies a team's nodes and edges under fresh IDs, preserving
// the entry node mapping.
func (s *DefaultService) CloneTeam(id, newName string, projectID *string) (*Team, error) {
	if newName == "" {
		return nil, errors.New("name is required")
	}
	graph, err := s.GetTeam(id)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	clone := &Team{
		ID:          uuid.New().String(),
		OrgID:       graph.Team.OrgID, // clones stay in the source team's org
		Name:        newName,
		Description: graph.Team.Description,
		ProjectID:   projectID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.SaveTeam(clone); err != nil {
		return nil, err
	}

	idMap := make(map[string]string, len(graph.Nodes))
	for _, n := range graph.Nodes {
		var userID *string
		if n.UserID != nil {
			v := *n.UserID
			userID = &v
		}
		newNode := &Node{
			ID:         uuid.New().String(),
			TeamID:     clone.ID,
			NodeType:   n.NodeType,
			AgentID:    n.AgentID,
			UserID:     userID,
			Label:      n.Label,
			Department: n.Department,
			Position:   copyMap(n.Position),
			CreatedAt:  now,
		}
		if err := s.repo.SaveNode(newNode); err != nil {
			return nil, err
		}
		idMap[n.ID] = newNode.ID
	}

	for _, e := range graph.Edges {
		newEdge := &Edge{
			ID:         uuid.New().String(),
			TeamID:     clone.ID,
			FromNodeID: idMap[e.FromNodeID],
			ToNodeID:   idMap[e.ToNodeID],
			EdgeType:   e.EdgeType,
			Config:     copyMap(e.Config),
			CreatedAt:  now,
		}
		if err := s.repo.SaveEdge(newEdge); err != nil {
			return nil, err
		}
	}

	if graph.Team.EntryNodeID != nil {
		if mapped, ok := idMap[*graph.Team.EntryNodeID]; ok {
			clone.EntryNodeID = &mapped
			if err := s.repo.UpdateTeam(clone); err != nil {
				return nil, err
			}
		}
	}
	return clone, nil
}
