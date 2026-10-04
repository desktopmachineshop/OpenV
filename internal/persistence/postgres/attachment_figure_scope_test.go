package postgres

import (
	"testing"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
)

// A figure's reference is unique within its project, as the artifact
// reference it is built on is (#379 bug 142). Migration 0023 made
// figure_ref unique across every project, so the first figure on a second
// project's REQ-1 collided with any other project's REQ-1-FIG-1 and the
// upload answered 500 "Failed to save attachment metadata". Migration 0054
// keys the index by the figure's artifact, whose counter numbers its
// figures and which belongs to one project, where an artifact reference is
// unique among the live artifacts (idx_artifacts_project_ref): two figures
// of one project share a reference only on one artifact, and the index
// still refuses that, as it did a duplicate anywhere before.
func TestAFigureReferenceIsUniquePerProject(t *testing.T) {
	db := rtDB(t)
	org, first, second := uuid.New().String(), uuid.New().String(), uuid.New().String()
	rtSeedOrg(t, db, org)
	rtSeedProject(t, db, first, org, "Pump")
	rtSeedProject(t, db, second, org, "Valve")

	artifactRepo := NewArtifactRepository(db)
	requirement := func(project string) *artifacts.Artifact {
		t.Helper()
		a := artifacts.NewArtifact(artifacts.CreateArtifactRequest{ProjectID: project, Type: "requirement",
			Title: "Show the wiring", Body: "The system shall show its wiring diagram."})
		if err := artifactRepo.Save(a); err != nil {
			t.Fatal(err)
		}
		if a.Ref != "REQ-1" {
			t.Fatalf("the project's first requirement is %q, want REQ-1", a.Ref)
		}
		return a
	}
	repo := NewAttachmentRepository(db)
	svc := attachments.NewDefaultService(repo)
	figure := func(on *artifacts.Artifact, name string) (*attachments.Attachment, error) {
		f := attachments.NewAttachment(attachments.CreateAttachmentRequest{ArtifactID: on.ID, Filename: name,
			OriginalFilename: name, MimeType: "image/png", FilePath: "/tmp/" + uuid.New().String(), FileSize: 10})
		return f, svc.CreateFigure(f, on.Ref)
	}

	pump, valve := requirement(first), requirement(second)
	pumpFig, err := figure(pump, "pump.png")
	if err != nil || pumpFig.FigureRef != "REQ-1-FIG-1" {
		t.Fatalf("the first project's figure: %q, %v; want REQ-1-FIG-1", pumpFig.FigureRef, err)
	}
	valveFig, err := figure(valve, "valve.png")
	if err != nil || valveFig.FigureRef != "REQ-1-FIG-1" {
		t.Fatalf("the second project's first figure on its REQ-1: %q, %v; want REQ-1-FIG-1 saved", valveFig.FigureRef, err)
	}
	for project, want := range map[string]string{first: pumpFig.ID, second: valveFig.ID} {
		got, err := repo.FindByProjectID(project)
		if err != nil || len(got) != 1 || got[0].ID != want || got[0].FigureRef != "REQ-1-FIG-1" {
			t.Errorf("project %s's figures: %v, %v; want its own REQ-1-FIG-1 alone", project, got, err)
		}
	}

	// Within one project a reference names one figure. A second figure
	// drawing a number its artifact already handed out, as two uploads
	// would if the counter were ever bypassed, is refused and leaves
	// nothing behind.
	rtSeed(t, db, `UPDATE attachment_figure_counters SET next_num = 1 WHERE artifact_id = $1`, pump.ID)
	_, err = figure(pump, "again.png")
	rtWantPQ(t, "a second REQ-1-FIG-1 on the first project's REQ-1", err, "23505", "idx_attachments_artifact_figure_ref")
	if got, err := repo.FindByArtifactID(pump.ID); err != nil || len(got) != 1 || got[0].ID != pumpFig.ID {
		t.Errorf("the first project's REQ-1 after the refused figure: %v, %v; want its one figure", got, err)
	}
	// No other artifact of the project can mint one: its live artifacts
	// never share a reference.
	_, err = db.Exec(`INSERT INTO artifacts (id, project_id, type, title, ref) VALUES ($1, $2, 'requirement', 'Twin', 'REQ-1')`,
		uuid.New().String(), first)
	rtWantPQ(t, "a second live REQ-1 in the first project", err, "23505", "idx_artifacts_project_ref")
}
