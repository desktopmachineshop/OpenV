package exports

import (
	"bytes"
	"encoding/csv"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// --- fakes: embed the interface so only the methods ExportProject touches
// need real implementations (anything else would panic loudly).

type fakeArtifactService struct {
	artifacts.Service
	byProject map[string][]*artifacts.Artifact
}

func (f *fakeArtifactService) ListArtifacts(projectID, artifactType string) ([]*artifacts.Artifact, error) {
	return f.byProject[projectID], nil
}

type fakeLinkService struct {
	links.Service
	byProject map[string][]*links.Link
}

func (f *fakeLinkService) GetAllLinks(projectID string) ([]*links.Link, error) {
	return f.byProject[projectID], nil
}

type fakeAttachmentService struct {
	attachments.Service
}

func (f *fakeAttachmentService) GetAttachmentsByArtifact(artifactID string) ([]*attachments.Attachment, error) {
	return nil, nil
}

type fakeProjectRepo struct {
	byID map[string]*ProjectInfo
}

func (f *fakeProjectRepo) FindByID(id string) (*ProjectInfo, error) {
	if p, ok := f.byID[id]; ok {
		return p, nil
	}
	return nil, errors.New("project not found")
}

func newTestService(projectName string, artifactList []*artifacts.Artifact, linkList []*links.Link) *DefaultService {
	return NewService(
		&fakeArtifactService{byProject: map[string][]*artifacts.Artifact{"p1": artifactList}},
		&fakeLinkService{byProject: map[string][]*links.Link{"p1": linkList}},
		&fakeAttachmentService{},
		&fakeProjectRepo{byID: map[string]*ProjectInfo{"p1": {ID: "p1", Name: projectName}}},
		nil,
	)
}

func strPtr(s string) *string { return &s }

func TestExportProjectCSV(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	updated := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)

	artifactList := []*artifacts.Artifact{
		{
			ID:    "a1",
			Type:  "requirement",
			Title: `Pump shall stop, always, on "overheat"`,
			Body:  "Line one\nLine two, with comma",
			Attributes: map[string]interface{}{
				"status": "approved",
			},
			Version:   3,
			CreatedAt: created,
			UpdatedAt: updated,
		},
		{
			ID:        "a2",
			Type:      "test-case",
			Title:     "Verify overheat cutoff",
			Body:      "",
			ParentID:  strPtr("a1"),
			Version:   1,
			CreatedAt: created,
			UpdatedAt: updated,
		},
	}
	linkList := []*links.Link{
		{ID: "l1", FromID: "a2", ToID: "a1", Type: "verifies"},
		{ID: "l2", FromID: "a2", ToID: "a3", Type: "satisfies"},
		{ID: "l3", FromID: "a9", ToID: "a1", Type: "mitigates"}, // unrelated source
	}

	svc := newTestService("Cooling System", artifactList, linkList)

	data, filename, err := svc.ExportProject("p1", FormatCSV)
	if err != nil {
		t.Fatalf("ExportProject(csv) returned error: %v", err)
	}

	if !strings.HasPrefix(filename, "project_Cooling System_") || !strings.HasSuffix(filename, ".csv") {
		t.Errorf("unexpected filename: %q", filename)
	}

	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("exported CSV does not parse: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected header + 2 rows, got %d rows", len(rows))
	}

	wantHeader := []string{"id", "type", "title", "body", "status", "version", "parent_id", "links", "created_at", "updated_at"}
	for i, col := range wantHeader {
		if rows[0][i] != col {
			t.Errorf("header[%d] = %q, want %q", i, rows[0][i], col)
		}
	}

	row1 := rows[1]
	if row1[0] != "a1" || row1[1] != "requirement" {
		t.Errorf("row1 id/type = %q/%q", row1[0], row1[1])
	}
	if row1[2] != `Pump shall stop, always, on "overheat"` {
		t.Errorf("row1 title did not round-trip: %q", row1[2])
	}
	if row1[3] != "Line one\nLine two, with comma" {
		t.Errorf("row1 body did not round-trip: %q", row1[3])
	}
	if row1[4] != "approved" {
		t.Errorf("row1 status = %q, want approved", row1[4])
	}
	if row1[5] != "3" {
		t.Errorf("row1 version = %q, want 3", row1[5])
	}
	if row1[6] != "" {
		t.Errorf("row1 parent_id = %q, want empty", row1[6])
	}
	if row1[7] != "" {
		t.Errorf("row1 links = %q, want empty (only incoming links exist)", row1[7])
	}
	if row1[8] != "2026-01-02T03:04:05Z" || row1[9] != "2026-02-03T04:05:06Z" {
		t.Errorf("row1 timestamps = %q/%q", row1[8], row1[9])
	}

	row2 := rows[2]
	if row2[4] != "" {
		t.Errorf("row2 status = %q, want empty (no status attribute)", row2[4])
	}
	if row2[6] != "a1" {
		t.Errorf("row2 parent_id = %q, want a1", row2[6])
	}
	if row2[7] != "verifies:a1;satisfies:a3" {
		t.Errorf("row2 links = %q, want verifies:a1;satisfies:a3", row2[7])
	}

	// The raw bytes must actually carry RFC 4180 quoting for fields with
	// commas, quotes, and newlines.
	raw := string(data)
	if !strings.Contains(raw, `"Pump shall stop, always, on ""overheat"""`) {
		t.Errorf("raw CSV missing quoted/escaped title field:\n%s", raw)
	}
}

func TestExportProjectCSVEmptyProject(t *testing.T) {
	svc := newTestService("Empty", nil, nil)

	data, _, err := svc.ExportProject("p1", FormatCSV)
	if err != nil {
		t.Fatalf("ExportProject(csv) returned error: %v", err)
	}

	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("exported CSV does not parse: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected header only, got %d rows", len(rows))
	}
	if rows[0][0] != "id" {
		t.Errorf("header row = %v", rows[0])
	}
}

func TestExportProjectUnsupportedFormats(t *testing.T) {
	svc := newTestService("Anything", nil, nil)

	for _, format := range []ExportFormat{FormatExcel, ExportFormat("xml"), ExportFormat("")} {
		_, _, err := svc.ExportProject("p1", format)
		if err == nil {
			t.Errorf("ExportProject(%q) expected error, got nil", format)
			continue
		}
		if !errors.Is(err, ErrUnsupportedFormat) {
			t.Errorf("ExportProject(%q) error = %v, want ErrUnsupportedFormat", format, err)
		}
	}
}

func TestExportFilenameSanitization(t *testing.T) {
	svc := newTestService("Acme \"Rocket\", v2\\..\x07/etc", nil, nil)

	for _, tc := range []struct {
		format ExportFormat
		ext    string
	}{
		{FormatJSON, ".json"},
		{FormatCSV, ".csv"},
	} {
		_, filename, err := svc.ExportProject("p1", tc.format)
		if err != nil {
			t.Fatalf("ExportProject(%q) returned error: %v", tc.format, err)
		}
		if !strings.HasSuffix(filename, tc.ext) {
			t.Errorf("filename %q missing %s extension", filename, tc.ext)
		}
		if !strings.HasPrefix(filename, "project_Acme Rocket, v2..etc_") {
			t.Errorf("filename %q not sanitized as expected", filename)
		}
		if strings.ContainsAny(filename, "\"\\/\x07") {
			t.Errorf("filename %q still contains unsafe characters", filename)
		}
	}
}

func TestSanitizeFilenameComponentEmptyFallback(t *testing.T) {
	svc := newTestService("\"\\/\x01\x02", nil, nil)

	_, filename, err := svc.ExportProject("p1", FormatCSV)
	if err != nil {
		t.Fatalf("ExportProject(csv) returned error: %v", err)
	}
	if !strings.HasPrefix(filename, "project_export_") {
		t.Errorf("filename %q, want fallback prefix project_export_", filename)
	}
}
