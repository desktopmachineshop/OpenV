package seeds

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agents"
)

const hierOrg = "22222222-2222-4222-8222-222222222222"

// placeOf names the slug an agent reports to and its group.
func placeOf(t *testing.T, svc agents.Service, slug string) (string, string) {
	t.Helper()
	a, err := svc.GetBySlug(hierOrg, slug)
	if err != nil {
		t.Fatalf("%s: %v", slug, err)
	}
	return a.ReportsTo, a.GroupName
}

// Seeded agents arrive already arranged (issue 733): the Chief of Staff at
// the top, and each specialist in its group under its lead.
func TestSeededAgentsArriveArranged(t *testing.T) {
	svc, err := agents.NewFileService(t.TempDir(), newMemAgentRepo(), agents.WithSeedAllowedTools(SeedAllowedTools))
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureOrgDefaults(hierOrg, svc); err != nil {
		t.Fatalf("EnsureOrgDefaults: %v", err)
	}
	want := map[string][2]string{
		"chief-of-staff":       {"", ""},
		"requirements-analyst": {"chief-of-staff", "Requirements"},
		agents.InterviewerSlug: {"requirements-analyst", "Requirements"},
		"vv-engineer":          {"chief-of-staff", "Verification"},
		TestCaseAuthorSlug:     {"vv-engineer", "Verification"},
		"requirements-copilot": {"vv-engineer", "Verification"},
		"developer":            {"chief-of-staff", "Engineering"},
		"reviewer":             {"developer", "Engineering"},
	}
	for slug, w := range want {
		if rt, g := placeOf(t, svc, slug); rt != w[0] || g != w[1] {
			t.Errorf("%s: reports to %q in %q, want %q in %q", slug, rt, g, w[0], w[1])
		}
	}
}

// A workspace seeded before the hierarchy takes the seed's arrangement for
// agents nobody has placed, and keeps a place a person chose.
func TestSeededAgentsWithAPlaceKeepIt(t *testing.T) {
	svc, err := agents.NewFileService(t.TempDir(), newMemAgentRepo(), agents.WithSeedAllowedTools(SeedAllowedTools))
	if err != nil {
		t.Fatal(err)
	}
	// As a workspace seeded before the hierarchy: every seed, none placed.
	for _, seed := range defaultAgents() {
		def := seed.def
		def.ReportsTo, def.GroupName, def.SortOrder = "", "", 0
		if _, err := svc.SaveDefinition(hierOrg, &def); err != nil {
			t.Fatal(err)
		}
	}
	// Someone has placed the developer already.
	dev, _ := svc.GetBySlug(hierOrg, "developer")
	def := definitionOf(dev)
	def.GroupName = "Build"
	if _, err := svc.SaveDefinition(hierOrg, &def); err != nil {
		t.Fatal(err)
	}
	if err := EnsureOrgDefaults(hierOrg, svc); err != nil {
		t.Fatalf("EnsureOrgDefaults: %v", err)
	}
	if rt, g := placeOf(t, svc, "developer"); rt != "" || g != "Build" {
		t.Errorf("developer: reports to %q in %q, want its own place (top of Build)", rt, g)
	}
	if rt, g := placeOf(t, svc, "reviewer"); rt != "developer" || g != "Engineering" {
		t.Errorf("reviewer: reports to %q in %q, want the seed's place", rt, g)
	}
}
