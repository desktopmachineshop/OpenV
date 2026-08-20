package teams

import "testing"

func mkNodes(ids ...string) []*Node {
	nodes := make([]*Node, 0, len(ids))
	for _, id := range ids {
		nodes = append(nodes, &Node{ID: id, TeamID: "t1", AgentID: "agent-" + id, Label: id})
	}
	return nodes
}

func mkEdge(from, to, edgeType string) *Edge {
	return &Edge{
		ID:         from + "->" + to,
		TeamID:     "t1",
		FromNodeID: from,
		ToNodeID:   to,
		EdgeType:   edgeType,
	}
}

func TestValidateGraph(t *testing.T) {
	tests := []struct {
		name    string
		nodes   []*Node
		edges   []*Edge
		wantErr bool
	}{
		{
			name:  "empty graph",
			nodes: nil,
			edges: nil,
		},
		{
			name:  "valid tree accepted",
			nodes: mkNodes("a", "b", "c", "d"),
			edges: []*Edge{
				mkEdge("a", "b", EdgeDelegates),
				mkEdge("a", "c", EdgeDelegates),
				mkEdge("b", "d", EdgeDelegates),
			},
		},
		{
			name:  "chain at max depth accepted",
			nodes: mkNodes("a", "b", "c", "d"),
			edges: []*Edge{
				mkEdge("a", "b", EdgeDelegates),
				mkEdge("b", "c", EdgeDelegates),
				mkEdge("c", "d", EdgeDelegates),
			},
		},
		{
			name:  "depth beyond max rejected",
			nodes: mkNodes("a", "b", "c", "d", "e"),
			edges: []*Edge{
				mkEdge("a", "b", EdgeDelegates),
				mkEdge("b", "c", EdgeDelegates),
				mkEdge("c", "d", EdgeDelegates),
				mkEdge("d", "e", EdgeDelegates),
			},
			wantErr: true,
		},
		{
			name:  "cycle rejected",
			nodes: mkNodes("a", "b", "c"),
			edges: []*Edge{
				mkEdge("a", "b", EdgeDelegates),
				mkEdge("b", "c", EdgeDelegates),
				mkEdge("c", "a", EdgeDelegates),
			},
			wantErr: true,
		},
		{
			name:  "two node cycle rejected",
			nodes: mkNodes("a", "b"),
			edges: []*Edge{
				mkEdge("a", "b", EdgeDelegates),
				mkEdge("b", "a", EdgeDelegates),
			},
			wantErr: true,
		},
		{
			name:  "multi-parent rejected",
			nodes: mkNodes("a", "b", "c"),
			edges: []*Edge{
				mkEdge("a", "c", EdgeDelegates),
				mkEdge("b", "c", EdgeDelegates),
			},
			wantErr: true,
		},
		{
			name:    "self-edge rejected",
			nodes:   mkNodes("a"),
			edges:   []*Edge{mkEdge("a", "a", EdgeDelegates)},
			wantErr: true,
		},
		{
			name:    "self-edge rejected on non-delegate types",
			nodes:   mkNodes("a"),
			edges:   []*Edge{mkEdge("a", "a", EdgeReviews)},
			wantErr: true,
		},
		{
			name:  "hands-off cycle allowed",
			nodes: mkNodes("a", "b"),
			edges: []*Edge{
				mkEdge("a", "b", EdgeHandsOff),
				mkEdge("b", "a", EdgeHandsOff),
			},
		},
		{
			name:  "reviews edges do not count toward delegation depth",
			nodes: mkNodes("a", "b", "c", "d", "e"),
			edges: []*Edge{
				mkEdge("a", "b", EdgeDelegates),
				mkEdge("b", "c", EdgeDelegates),
				mkEdge("c", "d", EdgeDelegates),
				mkEdge("d", "e", EdgeReviews),
			},
		},
		{
			name:    "edge referencing unknown node rejected",
			nodes:   mkNodes("a"),
			edges:   []*Edge{mkEdge("a", "ghost", EdgeDelegates)},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGraph(tt.nodes, tt.edges)
			if tt.wantErr && err == nil {
				t.Errorf("ValidateGraph() expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateGraph() unexpected error: %v", err)
			}
		})
	}
}

// --- fake repository for service-level tests ---

type fakeRepo struct {
	teams map[string]*Team
	nodes map[string]*Node
	edges map[string]*Edge
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		teams: map[string]*Team{},
		nodes: map[string]*Node{},
		edges: map[string]*Edge{},
	}
}

func (r *fakeRepo) SaveTeam(t *Team) error   { r.teams[t.ID] = t; return nil }
func (r *fakeRepo) UpdateTeam(t *Team) error { r.teams[t.ID] = t; return nil }
func (r *fakeRepo) FindTeamByID(id string) (*Team, error) {
	return r.teams[id], nil
}
func (r *fakeRepo) ListTeams(orgID, projectID string) ([]*Team, error) {
	var out []*Team
	for _, t := range r.teams {
		if t.OrgID == orgID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *fakeRepo) DeleteTeam(id string) error { delete(r.teams, id); return nil }
func (r *fakeRepo) MarkDefault(teamID string) error {
	if t, ok := r.teams[teamID]; ok {
		t.IsDefault = true
	}
	return nil
}

func (r *fakeRepo) FindDefaultTeam(orgID string) (*Team, error) {
	for _, t := range r.teams {
		if t.OrgID == orgID && t.IsDefault {
			return t, nil
		}
	}
	return nil, nil
}

func (r *fakeRepo) SaveNode(n *Node) error   { r.nodes[n.ID] = n; return nil }
func (r *fakeRepo) UpdateNode(n *Node) error { r.nodes[n.ID] = n; return nil }
func (r *fakeRepo) FindNodeByID(id string) (*Node, error) {
	return r.nodes[id], nil
}
func (r *fakeRepo) ListNodesByTeam(teamID string) ([]*Node, error) {
	var out []*Node
	for _, n := range r.nodes {
		if n.TeamID == teamID {
			out = append(out, n)
		}
	}
	return out, nil
}
func (r *fakeRepo) DeleteNode(id string) error { delete(r.nodes, id); return nil }

func (r *fakeRepo) SaveEdge(e *Edge) error   { r.edges[e.ID] = e; return nil }
func (r *fakeRepo) UpdateEdge(e *Edge) error { r.edges[e.ID] = e; return nil }
func (r *fakeRepo) FindEdgeByID(id string) (*Edge, error) {
	return r.edges[id], nil
}
func (r *fakeRepo) ListEdgesByTeam(teamID string) ([]*Edge, error) {
	var out []*Edge
	for _, e := range r.edges {
		if e.TeamID == teamID {
			out = append(out, e)
		}
	}
	return out, nil
}
func (r *fakeRepo) DeleteEdge(id string) error { delete(r.edges, id); return nil }

func newServiceWithTeam(t *testing.T) (*DefaultService, *fakeRepo, *Team) {
	t.Helper()
	repo := newFakeRepo()
	svc := NewDefaultService(repo)
	team, err := svc.CreateTeam("org-1", "Crew", "", nil)
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	return svc, repo, team
}

func TestAddNodeSpecValidation(t *testing.T) {
	tests := []struct {
		name    string
		spec    NodeSpec
		wantErr bool
	}{
		{name: "agent node ok", spec: NodeSpec{AgentID: "a1", Label: "Dev"}},
		{name: "explicit agent type ok", spec: NodeSpec{NodeType: NodeAgent, AgentID: "a1", Label: "Dev"}},
		{name: "agent without agent_id rejected", spec: NodeSpec{NodeType: NodeAgent, Label: "Dev"}, wantErr: true},
		{name: "agent with user_id rejected", spec: NodeSpec{NodeType: NodeAgent, AgentID: "a1", UserID: "u1", Label: "Dev"}, wantErr: true},
		{name: "human node ok", spec: NodeSpec{NodeType: NodeHuman, UserID: "u1", Label: "Dave"}},
		{name: "human without user_id rejected", spec: NodeSpec{NodeType: NodeHuman, Label: "Dave"}, wantErr: true},
		{name: "human with agent_id rejected", spec: NodeSpec{NodeType: NodeHuman, UserID: "u1", AgentID: "a1", Label: "Dave"}, wantErr: true},
		{name: "invalid node_type rejected", spec: NodeSpec{NodeType: "robot", AgentID: "a1", Label: "Dev"}, wantErr: true},
		{name: "missing label rejected", spec: NodeSpec{AgentID: "a1"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, team := newServiceWithTeam(t)
			node, err := svc.AddNode(team.ID, tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("AddNode() expected error, got node %+v", node)
				}
				return
			}
			if err != nil {
				t.Fatalf("AddNode() unexpected error: %v", err)
			}
			if node.NodeType == "" {
				t.Errorf("AddNode() did not default node_type")
			}
		})
	}
}

func TestAddNodeMemberValidator(t *testing.T) {
	svc, _, team := newServiceWithTeam(t)
	svc.SetMemberValidator(func(orgID, userID string) bool {
		return orgID == "org-1" && userID == "member"
	})
	if _, err := svc.AddNode(team.ID, NodeSpec{NodeType: NodeHuman, UserID: "member", Label: "Dave"}); err != nil {
		t.Fatalf("AddNode(member) unexpected error: %v", err)
	}
	if _, err := svc.AddNode(team.ID, NodeSpec{NodeType: NodeHuman, UserID: "outsider", Label: "Eve"}); err == nil {
		t.Fatal("AddNode(outsider) expected membership error, got nil")
	}
	// Agent nodes never hit the validator.
	if _, err := svc.AddNode(team.ID, NodeSpec{AgentID: "a1", Label: "Dev"}); err != nil {
		t.Fatalf("AddNode(agent) unexpected error: %v", err)
	}
}

func TestUpdateNodeCrossTypeRejected(t *testing.T) {
	svc, _, team := newServiceWithTeam(t)
	agentNode, err := svc.AddNode(team.ID, NodeSpec{AgentID: "a1", Label: "Dev"})
	if err != nil {
		t.Fatalf("AddNode(agent): %v", err)
	}
	humanNode, err := svc.AddNode(team.ID, NodeSpec{NodeType: NodeHuman, UserID: "u1", Label: "Dave"})
	if err != nil {
		t.Fatalf("AddNode(human): %v", err)
	}

	uid := "u2"
	if _, err := svc.UpdateNode(agentNode.ID, nil, nil, &uid, nil, nil); err == nil {
		t.Error("UpdateNode(agent node, user_id) expected error, got nil")
	}
	aid := "a2"
	if _, err := svc.UpdateNode(humanNode.ID, nil, &aid, nil, nil, nil); err == nil {
		t.Error("UpdateNode(human node, agent_id) expected error, got nil")
	}

	// Same-type identity changes still work.
	if _, err := svc.UpdateNode(agentNode.ID, nil, &aid, nil, nil, nil); err != nil {
		t.Errorf("UpdateNode(agent node, agent_id) unexpected error: %v", err)
	}
	updated, err := svc.UpdateNode(humanNode.ID, nil, nil, &uid, nil, nil)
	if err != nil {
		t.Fatalf("UpdateNode(human node, user_id) unexpected error: %v", err)
	}
	if updated.UserID == nil || *updated.UserID != "u2" {
		t.Errorf("UpdateNode(human node) user_id = %v, want u2", updated.UserID)
	}
}

func TestAddEdgeDelegatesToHumanRejected(t *testing.T) {
	svc, _, team := newServiceWithTeam(t)
	boss, _ := svc.AddNode(team.ID, NodeSpec{AgentID: "a1", Label: "Boss"})
	human, _ := svc.AddNode(team.ID, NodeSpec{NodeType: NodeHuman, UserID: "u1", Label: "Dave"})

	if _, err := svc.AddEdge(team.ID, boss.ID, human.ID, EdgeDelegates, nil); err == nil {
		t.Error("AddEdge(delegates-to human) expected error, got nil")
	}
	if _, err := svc.AddEdge(team.ID, boss.ID, human.ID, EdgeHandsOff, nil); err != nil {
		t.Errorf("AddEdge(hands-off-to human) unexpected error: %v", err)
	}
	if _, err := svc.AddEdge(team.ID, human.ID, boss.ID, EdgeReviews, nil); err != nil {
		t.Errorf("AddEdge(human reviews agent) unexpected error: %v", err)
	}
}

func TestResolveDelegatesFiltersHumans(t *testing.T) {
	svc, repo, team := newServiceWithTeam(t)
	boss, _ := svc.AddNode(team.ID, NodeSpec{AgentID: "a1", Label: "Boss"})
	worker, _ := svc.AddNode(team.ID, NodeSpec{AgentID: "a2", Label: "Worker"})
	human, _ := svc.AddNode(team.ID, NodeSpec{NodeType: NodeHuman, UserID: "u1", Label: "Dave"})

	if _, err := svc.AddEdge(team.ID, boss.ID, worker.ID, EdgeDelegates, nil); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	// Simulate a legacy/bypassed delegates edge to a human directly in the repo.
	if err := repo.SaveEdge(&Edge{ID: "legacy", TeamID: team.ID, FromNodeID: boss.ID, ToNodeID: human.ID, EdgeType: EdgeDelegates}); err != nil {
		t.Fatalf("SaveEdge: %v", err)
	}

	children, err := svc.ResolveDelegates(boss.ID)
	if err != nil {
		t.Fatalf("ResolveDelegates: %v", err)
	}
	if len(children) != 1 || children[0].ID != worker.ID {
		t.Errorf("ResolveDelegates() = %d children, want just the agent node", len(children))
	}
}

func TestUpdateTeamEntryNodeMustBeAgent(t *testing.T) {
	svc, _, team := newServiceWithTeam(t)
	agentNode, _ := svc.AddNode(team.ID, NodeSpec{AgentID: "a1", Label: "Dev"})
	humanNode, _ := svc.AddNode(team.ID, NodeSpec{NodeType: NodeHuman, UserID: "u1", Label: "Dave"})

	if _, err := svc.UpdateTeam(team.ID, nil, nil, &humanNode.ID); err == nil {
		t.Error("UpdateTeam(human entry) expected error, got nil")
	}
	if _, err := svc.UpdateTeam(team.ID, nil, nil, &agentNode.ID); err != nil {
		t.Errorf("UpdateTeam(agent entry) unexpected error: %v", err)
	}
}

func TestCloneTeamCopiesNodeIdentity(t *testing.T) {
	svc, _, team := newServiceWithTeam(t)
	if _, err := svc.AddNode(team.ID, NodeSpec{AgentID: "a1", Label: "Dev", Department: "Engineering"}); err != nil {
		t.Fatalf("AddNode(agent): %v", err)
	}
	if _, err := svc.AddNode(team.ID, NodeSpec{NodeType: NodeHuman, UserID: "u1", Label: "Dave", Department: "Ops"}); err != nil {
		t.Fatalf("AddNode(human): %v", err)
	}

	clone, err := svc.CloneTeam(team.ID, "Copy", nil)
	if err != nil {
		t.Fatalf("CloneTeam: %v", err)
	}
	graph, err := svc.GetTeam(clone.ID)
	if err != nil {
		t.Fatalf("GetTeam(clone): %v", err)
	}
	if len(graph.Nodes) != 2 {
		t.Fatalf("clone has %d nodes, want 2", len(graph.Nodes))
	}
	for _, n := range graph.Nodes {
		if n.Department == "" {
			t.Errorf("clone dropped department on node %q", n.Label)
		}
		switch n.NodeType {
		case NodeAgent:
			if n.AgentID != "a1" {
				t.Errorf("clone agent node has agent_id %q", n.AgentID)
			}
		case NodeHuman:
			if n.UserID == nil || *n.UserID != "u1" {
				t.Errorf("clone human node has user_id %v", n.UserID)
			}
		default:
			t.Errorf("clone node %q has node_type %q", n.Label, n.NodeType)
		}
	}
}
