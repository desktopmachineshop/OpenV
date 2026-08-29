package exports

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/projects"
)

// --- fakes for the import path. The artifact side uses the REAL
// artifacts.DefaultService over an in-memory repository so the test observes
// exactly what the import writes (and, crucially, whether it ever routes
// through the version-bumping Update path).

type memArtifactRepo struct {
	artifacts.Repository
	saved   []*artifacts.Artifact
	updates int
}

func (m *memArtifactRepo) Save(a *artifacts.Artifact) error {
	m.saved = append(m.saved, a)
	return nil
}

func (m *memArtifactRepo) FindByID(id string) (*artifacts.Artifact, error) {
	for _, a := range m.saved {
		if a.ID == id {
			copied := *a
			return &copied, nil
		}
	}
	return nil, artifacts.ErrNotFound
}

func (m *memArtifactRepo) Update(a *artifacts.Artifact) error {
	m.updates++
	for i, existing := range m.saved {
		if existing.ID == a.ID {
			m.saved[i] = a
			return nil
		}
	}
	return artifacts.ErrNotFound
}

func (m *memArtifactRepo) NextSortOrder(projectID string, parentID *string) (int, error) {
	return len(m.saved) + 1, nil
}

func (m *memArtifactRepo) byTitle(t *testing.T, title string) *artifacts.Artifact {
	t.Helper()
	for _, a := range m.saved {
		if a.Title == title {
			return a
		}
	}
	t.Fatalf("no saved artifact titled %q", title)
	return nil
}

type memLinkService struct {
	links.Service
	created []*links.Link
}

func (m *memLinkService) CreateLink(l *links.Link) error {
	m.created = append(m.created, l)
	return nil
}

type fakeProjectService struct {
	projects.Service
	created []*projects.Project
}

func (f *fakeProjectService) CreateProject(p *projects.Project) error {
	f.created = append(f.created, p)
	return nil
}

func newImportFixture() (*DefaultService, *memArtifactRepo, *memLinkService, *fakeProjectService) {
	repo := &memArtifactRepo{}
	linkSvc := &memLinkService{}
	projSvc := &fakeProjectService{}
	svc := NewService(
		artifacts.NewDefaultService(repo),
		linkSvc,
		&fakeAttachmentService{},
		nil, // projectRepo: only the export path reads it
		projSvc,
	)
	return svc, repo, linkSvc, projSvc
}

func roundTripPayload(t *testing.T) []byte {
	t.Helper()
	payload := &ProjectExport{
		Version:     "1.0",
		ProjectName: "Round Trip",
		ProjectDesc: "import round-trip fixture",
		Artifacts: []*artifacts.Artifact{
			{ID: "old-root", Type: "heading", Title: "Root", SortOrder: 1, Version: 4},
			{ID: "old-req", Type: "requirement", Title: "Req", Body: "shall", ParentID: strPtr("old-root"), SortOrder: 2, Version: 2},
			{ID: "old-tc", Type: "test-case", Title: "TC", ParentID: strPtr("old-req"), SortOrder: 3, Version: 1},
			{ID: "old-island", Type: "requirement", Title: "Island", SortOrder: 4, Version: 1},
		},
		Links: []*links.Link{
			{ID: "old-l1", FromID: "old-tc", ToID: "old-req", Type: "verifies"},
			// Dangling endpoint: must be skipped, exactly like before.
			{ID: "old-l2", FromID: "old-req", ToID: "not-in-payload", Type: "satisfies"},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshaling fixture: %v", err)
	}
	return data
}

// TestImportRoundTripVersionsStayOne locks in the issue-#124 fix: an import
// writes each artifact exactly once (version 1, no UpdateArtifact round
// trips) while still remapping parents, creating links, and populating
// links_snapshot attributes.
func TestImportRoundTripVersionsStayOne(t *testing.T) {
	svc, repo, linkSvc, projSvc := newImportFixture()

	projectID, err := svc.ImportProject(roundTripPayload(t), "org-1")
	if err != nil {
		t.Fatalf("ImportProject returned error: %v", err)
	}

	// Project created and owned by the org.
	if len(projSvc.created) != 1 {
		t.Fatalf("created %d projects, want 1", len(projSvc.created))
	}
	if projSvc.created[0].ID != projectID || projSvc.created[0].OrgID != "org-1" {
		t.Errorf("project id/org = %q/%q, want %q/org-1", projSvc.created[0].ID, projSvc.created[0].OrgID, projectID)
	}

	// Every artifact written exactly once, at version 1, in the new project.
	if len(repo.saved) != 4 {
		t.Fatalf("saved %d artifacts, want 4", len(repo.saved))
	}
	if repo.updates != 0 {
		t.Errorf("repo.Update called %d times, want 0 (imports must not bump versions)", repo.updates)
	}
	for _, a := range repo.saved {
		if a.Version != 1 {
			t.Errorf("artifact %q imported at version %d, want 1", a.Title, a.Version)
		}
		if a.ProjectID != projectID {
			t.Errorf("artifact %q project = %q, want %q", a.Title, a.ProjectID, projectID)
		}
	}

	root := repo.byTitle(t, "Root")
	req := repo.byTitle(t, "Req")
	tc := repo.byTitle(t, "TC")
	island := repo.byTitle(t, "Island")

	// Fresh IDs and remapped parents.
	if root.ID == "old-root" || req.ID == "old-req" {
		t.Errorf("imported artifacts kept their old IDs")
	}
	if root.ParentID != nil {
		t.Errorf("root parent = %v, want nil", *root.ParentID)
	}
	if req.ParentID == nil || *req.ParentID != root.ID {
		t.Errorf("req parent = %v, want root's new ID %q", req.ParentID, root.ID)
	}
	if tc.ParentID == nil || *tc.ParentID != req.ID {
		t.Errorf("tc parent = %v, want req's new ID %q", tc.ParentID, req.ID)
	}

	// Only the fully-mapped link is created, with remapped endpoints.
	if len(linkSvc.created) != 1 {
		t.Fatalf("created %d links, want 1 (dangling link must be skipped)", len(linkSvc.created))
	}
	link := linkSvc.created[0]
	if link.FromID != tc.ID || link.ToID != req.ID || link.Type != "verifies" {
		t.Errorf("link = %s -> %s (%s), want %s -> %s (verifies)", link.FromID, link.ToID, link.Type, tc.ID, req.ID)
	}

	// Both link endpoints carry the snapshot; unlinked artifacts carry none.
	for _, a := range []*artifacts.Artifact{tc, req} {
		snapshot, ok := a.Attributes["links_snapshot"].([]interface{})
		if !ok || len(snapshot) != 1 {
			t.Fatalf("artifact %q links_snapshot = %v, want exactly one entry", a.Title, a.Attributes["links_snapshot"])
		}
		snapLink, ok := snapshot[0].(*links.Link)
		if !ok || snapLink.ID != link.ID {
			t.Errorf("artifact %q snapshot entry = %#v, want the created link %q", a.Title, snapshot[0], link.ID)
		}
	}
	for _, a := range []*artifacts.Artifact{root, island} {
		if _, present := a.Attributes["links_snapshot"]; present {
			t.Errorf("artifact %q has a links_snapshot but has no links", a.Title)
		}
	}
}

// TestImportArtifactsIntoProjectMarkDraft covers the merge-into-existing
// project path: draft stamping, returned IDs in import order, versions 1.
func TestImportArtifactsIntoProjectMarkDraft(t *testing.T) {
	svc, repo, _, _ := newImportFixture()

	ids, err := svc.ImportArtifactsIntoProject("proj-77", roundTripPayload(t), true)
	if err != nil {
		t.Fatalf("ImportArtifactsIntoProject returned error: %v", err)
	}
	if len(ids) != 4 {
		t.Fatalf("returned %d ids, want 4", len(ids))
	}
	if repo.updates != 0 {
		t.Errorf("repo.Update called %d times, want 0", repo.updates)
	}
	for i, a := range repo.saved {
		if a.ID != ids[i] {
			t.Errorf("ids[%d] = %q, want %q (import order)", i, ids[i], a.ID)
		}
		if a.Version != 1 {
			t.Errorf("artifact %q version = %d, want 1", a.Title, a.Version)
		}
		if a.ProjectID != "proj-77" {
			t.Errorf("artifact %q project = %q, want proj-77", a.Title, a.ProjectID)
		}
		if a.Status != artifacts.StatusDraft {
			t.Errorf("artifact %q status = %q, want draft", a.Title, a.Status)
		}
		if a.Attributes["origin"] != "import" {
			t.Errorf("artifact %q origin = %v, want import", a.Title, a.Attributes["origin"])
		}
	}
}

// TestImportRejectsGarbage keeps the parse-error contract.
func TestImportRejectsGarbage(t *testing.T) {
	svc, repo, _, _ := newImportFixture()
	if _, err := svc.ImportProject([]byte("not json"), "org-1"); err == nil {
		t.Fatal("ImportProject accepted garbage")
	}
	if len(repo.saved) != 0 {
		t.Errorf("garbage import still saved %d artifacts", len(repo.saved))
	}
	if _, err := svc.ImportArtifactsIntoProject("", roundTripPayload(t), false); err == nil || !strings.Contains(err.Error(), "project id is required") {
		t.Errorf("empty project id error = %v", err)
	}
}
