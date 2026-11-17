package seeds

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agents"
)

func flatSvc(t *testing.T) agents.Service {
	t.Helper()
	svc, err := agents.NewFileService(t.TempDir(), newMemAgentRepo(), agents.WithSeedAllowedTools(SeedAllowedTools))
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func agentIn(t *testing.T, svc agents.Service, slug string) *agents.Agent {
	t.Helper()
	a, err := svc.GetBySlug(hierOrg, slug)
	if err != nil {
		t.Fatalf("%s: %v", slug, err)
	}
	return a
}

// A new workspace on the flat team gets the V&V Assistant at the top and the
// three auditors and the interviewer under it, and none of the earlier team.
func TestFlatTeamSeedsANewWorkspace(t *testing.T) {
	svc := flatSvc(t)
	if err := EnsureOrgDefaults(hierOrg, svc, Team{Flat: true}); err != nil {
		t.Fatal(err)
	}
	if a := agentIn(t, svc, AssistantSlug); a.ReportsTo != "" || a.Name != "V&V Assistant" {
		t.Errorf("assistant: reports to %q, name %q", a.ReportsTo, a.Name)
	}
	for _, slug := range []string{RequirementsAuditorSlug, TestAuditorSlug, LinkAuditorSlug, agents.InterviewerSlug} {
		if a := agentIn(t, svc, slug); a.ReportsTo != AssistantSlug || a.GroupName != "" {
			t.Errorf("%s: reports to %q in %q, want the assistant, no group", slug, a.ReportsTo, a.GroupName)
		}
	}
	for _, slug := range []string{RequirementsAuditorSlug, TestAuditorSlug, LinkAuditorSlug} {
		if a := agentIn(t, svc, slug); a.WriteMode != agents.WriteModeProposal {
			t.Errorf("%s writes %q; an auditor's changes must be proposals", slug, a.WriteMode)
		}
	}
	for _, slug := range RetiredSlugs {
		if _, err := svc.GetBySlug(hierOrg, slug); err == nil {
			t.Errorf("a new workspace was seeded the earlier team's %s", slug)
		}
	}
}

// A workspace on the earlier team moves to the flat one: untouched agents it
// replaces are archived, not deleted; the assistant and the interviewer move
// from their earlier places and the assistant takes its new prompt.
func TestFlatTeamRetiresTheEarlierTeam(t *testing.T) {
	svc := flatSvc(t)
	if err := EnsureOrgDefaults(hierOrg, svc); err != nil {
		t.Fatal(err)
	}
	if err := EnsureOrgDefaults(hierOrg, svc, Team{Flat: true}); err != nil {
		t.Fatal(err)
	}
	for _, slug := range RetiredSlugs {
		if a := agentIn(t, svc, slug); !a.Archived {
			t.Errorf("%s was not archived", slug)
		}
	}
	a := agentIn(t, svc, AssistantSlug)
	if a.ReportsTo != "" || a.GroupName != "" || a.Archived {
		t.Errorf("assistant: reports to %q in %q, archived %v; want the top, live", a.ReportsTo, a.GroupName, a.Archived)
	}
	if a.Description != flatDescription(AssistantSlug) {
		t.Errorf("an untouched assistant kept the earlier description %q", a.Description)
	}
	if rt := agentIn(t, svc, agents.InterviewerSlug).ReportsTo; rt != AssistantSlug {
		t.Errorf("interviewer reports to %q, want the assistant", rt)
	}
	for _, slug := range []string{RequirementsAuditorSlug, TestAuditorSlug, LinkAuditorSlug} {
		agentIn(t, svc, slug)
	}
	// Seeding again changes nothing more.
	if err := EnsureOrgDefaults(hierOrg, svc, Team{Flat: true}); err != nil {
		t.Fatal(err)
	}
}

// What a workspace made its own stays: a locked agent, an edited one, one an
// automation runs, and an arrangement a person chose.
func TestFlatTeamKeepsWhatTheWorkspaceMadeItsOwn(t *testing.T) {
	svc := flatSvc(t)
	if err := EnsureOrgDefaults(hierOrg, svc); err != nil {
		t.Fatal(err)
	}
	edit := func(slug string, change func(*agents.Definition)) *agents.Agent {
		def := definitionOf(agentIn(t, svc, slug))
		change(&def)
		a, err := svc.SaveDefinition(hierOrg, &def)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	edit("developer", func(d *agents.Definition) { d.Locked = true })
	edit("reviewer", func(d *agents.Definition) { d.SystemPrompt += "\nAlso check the changelog." })
	inUse := agentIn(t, svc, "vv-engineer").ID
	edit(agents.InterviewerSlug, func(d *agents.Definition) { d.GroupName = "Research" })

	team := Team{Flat: true, InUse: func(id string) bool { return id == inUse }}
	if err := EnsureOrgDefaults(hierOrg, svc, team); err != nil {
		t.Fatal(err)
	}
	for slug, want := range map[string]bool{"developer": false, "reviewer": false, "vv-engineer": false, "chief-of-staff": true} {
		if got := agentIn(t, svc, slug).Archived; got != want {
			t.Errorf("%s archived = %v, want %v", slug, got, want)
		}
	}
	if g := agentIn(t, svc, agents.InterviewerSlug).GroupName; g != "Research" {
		t.Errorf("a rearranged interviewer was moved: group %q", g)
	}
}

// Archived survives the agent's file: a sync keeps a retired agent retired.
func TestArchivedTravelsInTheFile(t *testing.T) {
	def, ok := seedNamed("chief-of-staff")
	if !ok {
		t.Fatal("no chief of staff seed")
	}
	def.Archived = true
	content, err := agents.SerializeFile(&def)
	if err != nil {
		t.Fatal(err)
	}
	back, err := agents.ParseFile(content)
	if err != nil || !back.Archived {
		t.Fatalf("parsed back archived = %v, %v; want true", back != nil && back.Archived, err)
	}
}
