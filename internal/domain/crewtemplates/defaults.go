package crewtemplates

import "github.com/openv/requirements-platform/internal/domain/teams"

// Built-in crew preset keys.
const (
	founderDevTeamKey   = "founders-dev-team"
	requirementsPairKey = "requirements-vv-pair"
)

// CrewTemplate is a named, built-in crew preset. Crew is the same portable
// document a user would get from GET /crews/{id}/export, so a preset is
// materialized through the ordinary import path.
type CrewTemplate struct {
	Key         string       `json:"key"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	IsBuiltin   bool         `json:"is_builtin"`
	Crew        PortableCrew `json:"crew"`
}

// CrewTemplateSummary is the metadata view without the full graph.
type CrewTemplateSummary struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsBuiltin   bool   `json:"is_builtin"`
	NodeCount   int    `json:"node_count"`
	EdgeCount   int    `json:"edge_count"`
}

// Summary derives the metadata view of a template.
func (t *CrewTemplate) Summary() CrewTemplateSummary {
	return CrewTemplateSummary{
		Key:         t.Key,
		Name:        t.Name,
		Description: t.Description,
		IsBuiltin:   t.IsBuiltin,
		NodeCount:   len(t.Crew.Nodes),
		EdgeCount:   len(t.Crew.Edges),
	}
}

// BuiltinCrewTemplates returns the bundled crew presets. Their agent slugs
// match the per-org seeded agents (see internal/seeds), so importing a preset
// into any seeded org resolves cleanly.
func BuiltinCrewTemplates() []*CrewTemplate {
	return []*CrewTemplate{
		foundersDevTeamTemplate(),
		requirementsPairTemplate(),
	}
}

// FindBuiltinCrewTemplate returns the preset with the given key, or nil.
func FindBuiltinCrewTemplate(key string) *CrewTemplate {
	for _, t := range BuiltinCrewTemplates() {
		if t.Key == key {
			return t
		}
	}
	return nil
}

// foundersDevTeamTemplate mirrors the seeded "Founder's Dev Team": a Chief of
// Staff orchestrator delegating to three specialists, with a reviewer watching
// the developer.
func foundersDevTeamTemplate() *CrewTemplate {
	return &CrewTemplate{
		Key:         founderDevTeamKey,
		Name:        "Founder's Dev Team",
		Description: "An orchestrator delegating to a requirements analyst, a V&V engineer, and a developer, with a reviewer watching the developer.",
		IsBuiltin:   true,
		Crew: PortableCrew{
			Kind:         Kind,
			Version:      Version,
			Name:         "Founder's Dev Team",
			Description:  "Default multi-agent team: an orchestrator delegating to specialists, with a reviewer watching the developer.",
			EntryNodeKey: "Chief of Staff",
			Nodes: []PortableNode{
				{Key: "Chief of Staff", AgentSlug: "chief-of-staff", Label: "Chief of Staff", Department: "Leadership"},
				{Key: "Requirements Analyst", AgentSlug: "requirements-analyst", Label: "Requirements Analyst", Department: "Product"},
				{Key: "V&V Engineer", AgentSlug: "vv-engineer", Label: "V&V Engineer", Department: "Quality"},
				{Key: "Developer", AgentSlug: "developer", Label: "Developer", Department: "Engineering"},
				{Key: "Reviewer", AgentSlug: "reviewer", Label: "Reviewer", Department: "Engineering"},
			},
			Edges: []PortableEdge{
				{FromKey: "Chief of Staff", ToKey: "Requirements Analyst", EdgeType: teams.EdgeDelegates},
				{FromKey: "Chief of Staff", ToKey: "V&V Engineer", EdgeType: teams.EdgeDelegates},
				{FromKey: "Chief of Staff", ToKey: "Developer", EdgeType: teams.EdgeDelegates},
				{FromKey: "Developer", ToKey: "Reviewer", EdgeType: teams.EdgeReviews},
			},
		},
	}
}

// requirementsPairTemplate is a minimal two-agent crew: a requirements analyst
// hands finished drafts off to a V&V engineer. Demonstrates a crew with no
// orchestrator.
func requirementsPairTemplate() *CrewTemplate {
	return &CrewTemplate{
		Key:         requirementsPairKey,
		Name:        "Requirements & V&V Pair",
		Description: "A requirements analyst who hands drafts off to a V&V engineer for test coverage.",
		IsBuiltin:   true,
		Crew: PortableCrew{
			Kind:         Kind,
			Version:      Version,
			Name:         "Requirements & V&V Pair",
			Description:  "A requirements analyst who hands drafts off to a V&V engineer for test coverage.",
			EntryNodeKey: "Requirements Analyst",
			Nodes: []PortableNode{
				{Key: "Requirements Analyst", AgentSlug: "requirements-analyst", Label: "Requirements Analyst", Department: "Product"},
				{Key: "V&V Engineer", AgentSlug: "vv-engineer", Label: "V&V Engineer", Department: "Quality"},
			},
			Edges: []PortableEdge{
				{FromKey: "Requirements Analyst", ToKey: "V&V Engineer", EdgeType: teams.EdgeHandsOff},
			},
		},
	}
}
