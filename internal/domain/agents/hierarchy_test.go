package agents

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The agent hierarchy (issue 733): reports_to, group and sort_order travel
// in the file, the registry row carries the parent resolved to an agent of
// the same workspace, and no arrangement may make an agent report to itself.

const hierOrg = "11111111-1111-4111-8111-111111111111"

func hierService(t *testing.T) (*FileService, *memRepo, string) {
	t.Helper()
	dir := t.TempDir()
	repo := newMemRepo()
	svc, err := NewFileService(dir, repo)
	if err != nil {
		t.Fatal(err)
	}
	return svc, repo, dir
}

func hierDef(slug, reportsTo, group string, order int) *Definition {
	return &Definition{Slug: slug, Name: strings.ToUpper(slug), Provider: "claude-code",
		AllowedTools: []string{"mcp__openv__*"}, SystemPrompt: "Work.",
		ReportsTo: reportsTo, GroupName: group, SortOrder: order}
}

func mustSave(t *testing.T, svc *FileService, def *Definition) *Agent {
	t.Helper()
	a, err := svc.SaveDefinition(hierOrg, def)
	if err != nil {
		t.Fatalf("SaveDefinition(%s): %v", def.Slug, err)
	}
	return a
}

func parentSlug(t *testing.T, svc *FileService, slug string) string {
	t.Helper()
	a, err := svc.GetBySlug(hierOrg, slug)
	if err != nil {
		t.Fatal(err)
	}
	if a.ParentAgentID == nil {
		return ""
	}
	p, err := svc.Get(*a.ParentAgentID)
	if err != nil {
		t.Fatal(err)
	}
	return p.Slug
}

func TestTheHierarchyTravelsInTheFile(t *testing.T) {
	svc, _, dir := hierService(t)
	lead := mustSave(t, svc, hierDef("lead", "", "", 0))
	child := mustSave(t, svc, hierDef("analyst", "lead", "Requirements", 2))
	if child.ParentAgentID == nil || *child.ParentAgentID != lead.ID || child.ReportsTo != "lead" ||
		child.GroupName != "Requirements" || child.SortOrder != 2 {
		t.Fatalf("row = parent %v, reports_to %q, group %q, order %d", child.ParentAgentID, child.ReportsTo,
			child.GroupName, child.SortOrder)
	}
	raw, err := os.ReadFile(filepath.Join(dir, hierOrg, "analyst.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"reports_to: lead", "group: Requirements", "sort_order: 2"} {
		if !strings.Contains(string(raw), line) {
			t.Errorf("the file lacks %q:\n%s", line, raw)
		}
	}
	def, err := ParseFile(string(raw))
	if err != nil || def.ReportsTo != "lead" || def.GroupName != "Requirements" || def.SortOrder != 2 {
		t.Fatalf("parsed back %+v, %v", def, err)
	}
	// A file with no place says nothing about one.
	raw, _ = os.ReadFile(filepath.Join(dir, hierOrg, "lead.md"))
	if strings.Contains(string(raw), "reports_to") || strings.Contains(string(raw), "group:") {
		t.Errorf("a top agent's file names a place:\n%s", raw)
	}
}

func TestASaveRefusesABadPlace(t *testing.T) {
	svc, _, _ := hierService(t)
	mustSave(t, svc, hierDef("lead", "", "", 0))
	mustSave(t, svc, hierDef("analyst", "lead", "", 0))
	for _, tc := range []struct {
		name string
		def  *Definition
		want error
	}{
		{"itself", hierDef("lead", "lead", "", 0), ErrHierarchyCycle},
		{"its own report", hierDef("lead", "analyst", "", 0), ErrHierarchyCycle},
		{"an agent the workspace does not have", hierDef("analyst", "ghost", "", 0), ErrUnknownParent},
	} {
		if _, err := svc.SaveDefinition(hierOrg, tc.def); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	if _, err := svc.SaveDefinition(hierOrg, hierDef("analyst", "", strings.Repeat("g", MaxGroupNameLength+1), 0)); err == nil {
		t.Error("a group name over the limit was saved")
	}
	if parentSlug(t, svc, "lead") != "" || parentSlug(t, svc, "analyst") != "lead" {
		t.Error("a refused save changed the hierarchy")
	}
}

func TestArrangeChecksTheWholeArrangement(t *testing.T) {
	svc, _, _ := hierService(t)
	mustSave(t, svc, hierDef("a", "", "", 0))
	mustSave(t, svc, hierDef("b", "a", "", 0))
	mustSave(t, svc, hierDef("c", "a", "", 1))

	// An exchange: b goes to the top, a under b. Half-way it is a cycle;
	// whole, it is not.
	list, err := svc.Arrange(hierOrg, []Placement{
		{Slug: "b", ReportsTo: "", GroupName: "Leads"},
		{Slug: "a", ReportsTo: "b", GroupName: "Leads", SortOrder: 3},
	})
	if err != nil {
		t.Fatalf("Arrange: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("Arrange answered %d agents, want the org's 3", len(list))
	}
	if parentSlug(t, svc, "a") != "b" || parentSlug(t, svc, "b") != "" || parentSlug(t, svc, "c") != "a" {
		t.Errorf("after the exchange: a under %q, b under %q, c under %q", parentSlug(t, svc, "a"),
			parentSlug(t, svc, "b"), parentSlug(t, svc, "c"))
	}
	a, _ := svc.GetBySlug(hierOrg, "a")
	if a.GroupName != "Leads" || a.SortOrder != 3 {
		t.Errorf("a: group %q, order %d", a.GroupName, a.SortOrder)
	}
	raw, _ := svc.RawFile(hierOrg, "a")
	if !strings.Contains(raw, "reports_to: b") || !strings.Contains(raw, "Work.") {
		t.Errorf("a's file after Arrange:\n%s", raw)
	}

	for _, tc := range []struct {
		name string
		p    []Placement
		want error
	}{
		{"a cycle", []Placement{{Slug: "b", ReportsTo: "c"}}, ErrHierarchyCycle},
		{"an unknown parent", []Placement{{Slug: "c", ReportsTo: "ghost"}}, ErrUnknownParent},
		{"an unknown agent", []Placement{{Slug: "ghost"}}, ErrNotFound},
	} {
		if _, err := svc.Arrange(hierOrg, tc.p); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	if parentSlug(t, svc, "b") != "" || parentSlug(t, svc, "c") != "a" {
		t.Error("a refused arrangement changed the hierarchy")
	}
}

func TestASyncLinksParentsWhateverTheFileOrder(t *testing.T) {
	svc, repo, dir := hierService(t)
	orgDir := filepath.Join(dir, hierOrg)
	if err := os.MkdirAll(orgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(def *Definition) {
		content, err := SerializeFile(def)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(orgDir, def.Slug+".md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// "a-child" sorts before "z-lead", so it is synced before its parent;
	// x and y report to each other, as only a hand edit could make them.
	write(hierDef("a-child", "z-lead", "", 0))
	write(hierDef("z-lead", "", "", 0))
	write(hierDef("x", "y", "", 0))
	write(hierDef("y", "x", "", 0))
	if err := svc.SyncFromDisk(hierOrg); err != nil {
		t.Fatalf("SyncFromDisk: %v", err)
	}
	if got := parentSlug(t, svc, "a-child"); got != "z-lead" {
		t.Errorf("a-child reports to %q, want z-lead", got)
	}
	parents := map[string]string{}
	for _, a := range repo.rows {
		parents[a.Slug] = a.ReportsTo
	}
	if _, cycle := HierarchyCycle(parents); cycle {
		t.Errorf("the registry holds a cycle: %v", parents)
	}
	if parentSlug(t, svc, "x") != "" && parentSlug(t, svc, "y") != "" {
		t.Error("neither of a cycle's agents was left at the top")
	}
}

func TestAParentCreatedAfterItsReportsLinksThem(t *testing.T) {
	svc, _, dir := hierService(t)
	orgDir := filepath.Join(dir, hierOrg)
	if err := os.MkdirAll(orgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content, _ := SerializeFile(hierDef("child", "later", "", 0))
	if err := os.WriteFile(filepath.Join(orgDir, "child.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.SyncFromDisk(hierOrg); err != nil {
		t.Fatal(err)
	}
	if parentSlug(t, svc, "child") != "" {
		t.Fatal("child has a parent before there is one")
	}
	mustSave(t, svc, hierDef("later", "", "", 0))
	if got := parentSlug(t, svc, "child"); got != "later" {
		t.Errorf("child reports to %q once its parent exists, want later", got)
	}
}

func TestHierarchyCycle(t *testing.T) {
	for _, tc := range []struct {
		parents map[string]string
		cycle   bool
	}{
		{map[string]string{"a": "", "b": "a", "c": "b"}, false},
		{map[string]string{"a": "b", "b": "a"}, true},
		{map[string]string{"a": "", "b": "c", "c": "d", "d": "b"}, true},
		{map[string]string{"a": "ghost"}, false},
	} {
		if _, got := HierarchyCycle(tc.parents); got != tc.cycle {
			t.Errorf("HierarchyCycle(%v) = %v, want %v", tc.parents, got, tc.cycle)
		}
	}
}
