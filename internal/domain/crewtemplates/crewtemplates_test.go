package crewtemplates

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/teams"
)

// --- in-memory teams repository (mirrors the teams package test double) ---

type memRepo struct {
	teams map[string]*teams.Team
	nodes map[string]*teams.Node
	edges map[string]*teams.Edge
}

func newMemRepo() *memRepo {
	return &memRepo{
		teams: map[string]*teams.Team{},
		nodes: map[string]*teams.Node{},
		edges: map[string]*teams.Edge{},
	}
}

func (r *memRepo) SaveTeam(t *teams.Team) error   { r.teams[t.ID] = t; return nil }
func (r *memRepo) UpdateTeam(t *teams.Team) error { r.teams[t.ID] = t; return nil }
func (r *memRepo) FindTeamByID(id string) (*teams.Team, error) {
	return r.teams[id], nil
}
func (r *memRepo) ListTeams(orgID, projectID string) ([]*teams.Team, error) {
	var out []*teams.Team
	for _, t := range r.teams {
		if t.OrgID == orgID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (r *memRepo) DeleteTeam(id string) error { delete(r.teams, id); return nil }
func (r *memRepo) MarkDefault(teamID string) error {
	if t, ok := r.teams[teamID]; ok {
		t.IsDefault = true
	}
	return nil
}
func (r *memRepo) FindDefaultTeam(orgID string) (*teams.Team, error) {
	for _, t := range r.teams {
		if t.OrgID == orgID && t.IsDefault {
			return t, nil
		}
	}
	return nil, nil
}
func (r *memRepo) SaveNode(n *teams.Node) error   { r.nodes[n.ID] = n; return nil }
func (r *memRepo) UpdateNode(n *teams.Node) error { r.nodes[n.ID] = n; return nil }
func (r *memRepo) FindNodeByID(id string) (*teams.Node, error) {
	return r.nodes[id], nil
}
func (r *memRepo) ListNodesByTeam(teamID string) ([]*teams.Node, error) {
	var out []*teams.Node
	for _, n := range r.nodes {
		if n.TeamID == teamID {
			out = append(out, n)
		}
	}
	return out, nil
}
func (r *memRepo) DeleteNode(id string) error { delete(r.nodes, id); return nil }
func (r *memRepo) SaveEdge(e *teams.Edge) error   { r.edges[e.ID] = e; return nil }
func (r *memRepo) UpdateEdge(e *teams.Edge) error { r.edges[e.ID] = e; return nil }
func (r *memRepo) FindEdgeByID(id string) (*teams.Edge, error) {
	return r.edges[id], nil
}
func (r *memRepo) ListEdgesByTeam(teamID string) ([]*teams.Edge, error) {
	var out []*teams.Edge
	for _, e := range r.edges {
		if e.TeamID == teamID {
			out = append(out, e)
		}
	}
	return out, nil
}
func (r *memRepo) DeleteEdge(id string) error { delete(r.edges, id); return nil }

// --- fake agent directory ---

// fakeDir resolves agents by global id and by (org, slug). Each org has its own
// id space so a slug maps to different ids across orgs — the whole point of
// slug-based portability.
type fakeDir struct {
	byID   map[string]*agents.Agent            // global id -> agent
	bySlug map[string]map[string]*agents.Agent // org -> slug -> agent
}

func newFakeDir() *fakeDir {
	return &fakeDir{byID: map[string]*agents.Agent{}, bySlug: map[string]map[string]*agents.Agent{}}
}

func (d *fakeDir) add(orgID, id, slug, name string) *agents.Agent {
	a := &agents.Agent{ID: id, OrgID: orgID, Slug: slug, Name: name}
	d.byID[id] = a
	if d.bySlug[orgID] == nil {
		d.bySlug[orgID] = map[string]*agents.Agent{}
	}
	d.bySlug[orgID][slug] = a
	return a
}

func (d *fakeDir) Get(id string) (*agents.Agent, error) { return d.byID[id], nil }
func (d *fakeDir) GetBySlug(orgID, slug string) (*agents.Agent, error) {
	if m := d.bySlug[orgID]; m != nil {
		return m[slug], nil
	}
	return nil, nil
}

// buildSourceCrew creates a crew in orgA with a chief delegating to a developer
// and returns its graph.
func buildSourceCrew(t *testing.T, svc *teams.DefaultService, dir *fakeDir, orgA string) *teams.TeamGraph {
	t.Helper()
	chief := dir.add(orgA, "a-chief", "chief-of-staff", "Chief of Staff")
	dev := dir.add(orgA, "a-dev", "developer", "Developer")

	team, err := svc.CreateTeam(orgA, "Dev Crew", "desc", nil)
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	chiefNode, err := svc.AddNode(team.ID, teams.NodeSpec{NodeType: teams.NodeAgent, AgentID: chief.ID, Label: "Chief", Department: "Leadership"})
	if err != nil {
		t.Fatalf("AddNode chief: %v", err)
	}
	devNode, err := svc.AddNode(team.ID, teams.NodeSpec{NodeType: teams.NodeAgent, AgentID: dev.ID, Label: "Dev", Department: "Engineering"})
	if err != nil {
		t.Fatalf("AddNode dev: %v", err)
	}
	if _, err := svc.AddEdge(team.ID, chiefNode.ID, devNode.ID, teams.EdgeDelegates, nil); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	name := team.Name
	if _, err := svc.UpdateTeam(team.ID, &name, nil, &chiefNode.ID); err != nil {
		t.Fatalf("UpdateTeam entry: %v", err)
	}
	graph, err := svc.GetTeam(team.ID)
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	return graph
}

func TestSerializeImportRoundTripAcrossOrgs(t *testing.T) {
	const orgA, orgB = "org-a", "org-b"
	svc := teams.NewDefaultService(newMemRepo())
	dir := newFakeDir()

	graph := buildSourceCrew(t, svc, dir, orgA)

	portable, err := Serialize(graph, dir)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if len(portable.Nodes) != 2 || len(portable.Edges) != 1 {
		t.Fatalf("portable has %d nodes / %d edges, want 2/1", len(portable.Nodes), len(portable.Edges))
	}
	// Nodes must reference agents by slug, not by id.
	slugs := map[string]bool{}
	for _, n := range portable.Nodes {
		slugs[n.AgentSlug] = true
		if n.AgentSlug == "" {
			t.Fatalf("node %q missing agent slug", n.Label)
		}
	}
	if !slugs["chief-of-staff"] || !slugs["developer"] {
		t.Fatalf("expected chief-of-staff and developer slugs, got %v", slugs)
	}
	if portable.EntryNodeKey == "" {
		t.Fatalf("entry node key not serialized")
	}

	// Target org B has the same slugs under DIFFERENT ids.
	bChief := dir.add(orgB, "b-chief", "chief-of-staff", "Chief of Staff")
	bDev := dir.add(orgB, "b-dev", "developer", "Developer")

	res, err := Import(portable, orgB, nil, dir, svc)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", res.Warnings)
	}
	if res.Team.OrgID != orgB {
		t.Fatalf("imported crew org = %q, want %q", res.Team.OrgID, orgB)
	}

	imported, err := svc.GetTeam(res.Team.ID)
	if err != nil {
		t.Fatalf("GetTeam(imported): %v", err)
	}
	if len(imported.Nodes) != 2 || len(imported.Edges) != 1 {
		t.Fatalf("imported has %d nodes / %d edges, want 2/1", len(imported.Nodes), len(imported.Edges))
	}
	gotIDs := map[string]bool{}
	for _, n := range imported.Nodes {
		gotIDs[n.AgentID] = true
	}
	if !gotIDs[bChief.ID] || !gotIDs[bDev.ID] {
		t.Fatalf("imported nodes reference %v, want org-B agent ids %s/%s", gotIDs, bChief.ID, bDev.ID)
	}
	// And crucially NOT the source org's ids.
	if gotIDs["a-chief"] || gotIDs["a-dev"] {
		t.Fatalf("imported crew leaked source-org agent ids: %v", gotIDs)
	}
	if imported.Team.EntryNodeID == nil {
		t.Fatalf("entry node not resolved on import")
	}
}

func TestImportMissingSlugSkipsNodeAndEdges(t *testing.T) {
	const orgB = "org-b"
	svc := teams.NewDefaultService(newMemRepo())
	dir := newFakeDir()
	// Only the developer slug exists in org B; the chief is missing.
	dir.add(orgB, "b-dev", "developer", "Developer")

	portable := &PortableCrew{
		Kind: Kind, Version: Version, Name: "Partial Crew",
		EntryNodeKey: "chief",
		Nodes: []PortableNode{
			{Key: "chief", AgentSlug: "chief-of-staff", Label: "Chief"},
			{Key: "dev", AgentSlug: "developer", Label: "Dev"},
		},
		Edges: []PortableEdge{
			{FromKey: "chief", ToKey: "dev", EdgeType: teams.EdgeDelegates},
		},
	}

	res, err := Import(portable, orgB, nil, dir, svc)
	if err != nil {
		t.Fatalf("Import should not fail on a missing slug: %v", err)
	}
	if len(res.Warnings) < 2 {
		t.Fatalf("expected warnings for the skipped node and its edge, got %v", res.Warnings)
	}
	imported, err := svc.GetTeam(res.Team.ID)
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if len(imported.Nodes) != 1 {
		t.Fatalf("imported %d nodes, want 1 (missing chief skipped)", len(imported.Nodes))
	}
	if len(imported.Edges) != 0 {
		t.Fatalf("imported %d edges, want 0 (edge to skipped node dropped)", len(imported.Edges))
	}
	if imported.Nodes[0].AgentID != "b-dev" {
		t.Fatalf("surviving node references %q, want b-dev", imported.Nodes[0].AgentID)
	}
}

func TestImportRejectsWrongKind(t *testing.T) {
	svc := teams.NewDefaultService(newMemRepo())
	dir := newFakeDir()
	_, err := Import(&PortableCrew{Kind: "something-else", Name: "X"}, "org-b", nil, dir, svc)
	if err == nil {
		t.Fatal("expected error for unrecognized document kind")
	}
}

func TestImportRequiresName(t *testing.T) {
	svc := teams.NewDefaultService(newMemRepo())
	dir := newFakeDir()
	if _, err := Import(&PortableCrew{Kind: Kind, Name: "  "}, "org-b", nil, dir, svc); err == nil {
		t.Fatal("expected error for empty crew name")
	}
}

func TestSerializeDropsHumanNodes(t *testing.T) {
	const orgA = "org-a"
	svc := teams.NewDefaultService(newMemRepo())
	dir := newFakeDir()
	agent := dir.add(orgA, "a-dev", "developer", "Developer")

	team, err := svc.CreateTeam(orgA, "Mixed", "", nil)
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	agentNode, err := svc.AddNode(team.ID, teams.NodeSpec{NodeType: teams.NodeAgent, AgentID: agent.ID, Label: "Dev"})
	if err != nil {
		t.Fatalf("AddNode agent: %v", err)
	}
	humanNode, err := svc.AddNode(team.ID, teams.NodeSpec{NodeType: teams.NodeHuman, UserID: "user-1", Label: "Dave"})
	if err != nil {
		t.Fatalf("AddNode human: %v", err)
	}
	// A hands-off edge from the agent to the human should be dropped with it.
	if _, err := svc.AddEdge(team.ID, agentNode.ID, humanNode.ID, teams.EdgeHandsOff, nil); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	graph, err := svc.GetTeam(team.ID)
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}

	portable, err := Serialize(graph, dir)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if len(portable.Nodes) != 1 {
		t.Fatalf("serialized %d nodes, want 1 (human dropped)", len(portable.Nodes))
	}
	if len(portable.Edges) != 0 {
		t.Fatalf("serialized %d edges, want 0 (edge to human dropped)", len(portable.Edges))
	}
}

func TestBuiltinCrewTemplatesShape(t *testing.T) {
	list := BuiltinCrewTemplates()
	if len(list) == 0 {
		t.Fatal("expected at least one built-in crew template")
	}
	founder := FindBuiltinCrewTemplate("founders-dev-team")
	if founder == nil {
		t.Fatal("founders-dev-team preset missing")
	}
	// Every edge must reference declared node keys, and the entry key must exist.
	for _, tmpl := range list {
		if tmpl.Key == "" || tmpl.Name == "" {
			t.Fatalf("template %+v missing key/name", tmpl.Summary())
		}
		keys := map[string]bool{}
		for _, n := range tmpl.Crew.Nodes {
			if n.AgentSlug == "" {
				t.Fatalf("%s: node %q has no agent slug", tmpl.Key, n.Label)
			}
			keys[n.Key] = true
		}
		for _, e := range tmpl.Crew.Edges {
			if !keys[e.FromKey] || !keys[e.ToKey] {
				t.Fatalf("%s: edge %s->%s references an unknown node key", tmpl.Key, e.FromKey, e.ToKey)
			}
		}
		if tmpl.Crew.EntryNodeKey != "" && !keys[tmpl.Crew.EntryNodeKey] {
			t.Fatalf("%s: entry key %q is not a declared node", tmpl.Key, tmpl.Crew.EntryNodeKey)
		}
	}
}

// TestBuiltinFounderTemplateImportsAgainstSeededSlugs proves a built-in preset
// resolves when its slugs are present, exercising the same path an org would
// use to instantiate it.
func TestBuiltinFounderTemplateImportsAgainstSeededSlugs(t *testing.T) {
	const org = "org-seeded"
	svc := teams.NewDefaultService(newMemRepo())
	dir := newFakeDir()
	for _, slug := range []string{"chief-of-staff", "requirements-analyst", "vv-engineer", "developer", "reviewer"} {
		dir.add(org, "id-"+slug, slug, slug)
	}
	tmpl := FindBuiltinCrewTemplate("founders-dev-team")
	crew := tmpl.Crew
	res, err := Import(&crew, org, nil, dir, svc)
	if err != nil {
		t.Fatalf("Import founder preset: %v", err)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("unexpected warnings importing preset: %v", res.Warnings)
	}
	imported, err := svc.GetTeam(res.Team.ID)
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if len(imported.Nodes) != 5 || len(imported.Edges) != 4 {
		t.Fatalf("preset imported %d nodes / %d edges, want 5/4", len(imported.Nodes), len(imported.Edges))
	}
	if imported.Team.EntryNodeID == nil {
		t.Fatal("preset entry node not set")
	}
}
