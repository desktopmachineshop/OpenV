// Package snapshot holds the project snapshot: ProjectExport, which the JSON
// export writes, a baseline stores and every report, download, coverage and
// quality read takes, and LinkedArtifact, the far end of a link that crosses
// into another project. It moved here from exports under the same names
// (refactor plan P1, R8), and exports keeps both as aliases.
package snapshot

import (
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/products"
)

// ProjectExport contains all project data for export
type ProjectExport struct {
	ExportedAt  time.Time `json:"exported_at"`
	Version     string    `json:"version"`
	ProjectID   string    `json:"project_id"`
	ProjectName string    `json:"project_name"`
	ProjectDesc string    `json:"project_description"`
	// BaselineName names the baseline a snapshot was loaded from, when it was
	// one rather than the live project. It is set by the download layer for
	// the formats that show it on the page (the Excel cover sheet; the PDF and
	// Word renderers take it as an argument), and is empty for a live export.
	BaselineName   string                    `json:"baseline_name,omitempty"`
	Artifacts      []*artifacts.Artifact     `json:"artifacts"`
	Links          []*links.Link             `json:"links"`
	Attachments    []*attachments.Attachment `json:"attachments"`
	ProductProfile *products.ProductProfile  `json:"product_profile,omitempty"`
	// AttributeDefs carries the org/project attribute definitions effective for
	// the project (Definitions). The ReqIF export and download load them to
	// type enum attributes as ReqIF enumerations, and a baseline's snapshot
	// keeps them (Snapshot, REQ-5), which an import of it restores as the new
	// project's own; the live JSON, CSV and Excel exports leave it nil. A
	// snapshot captured before baselines kept them has none.
	AttributeDefs []*attributes.Definition `json:"attribute_definitions,omitempty"`
	// LinkedArtifacts describes the far end of every link that crosses into
	// another project (REQ-145): enough to name a parent requirement a local
	// one refines, or the child requirements refining a local one, without
	// carrying the other project's data. Empty when no link crosses.
	LinkedArtifacts []*LinkedArtifact `json:"linked_artifacts,omitempty"`
}

// LinkedArtifact is an artifact of another project that one of this
// project's links points at or comes from.
type LinkedArtifact struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	Ref         string `json:"ref"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Status      string `json:"status"`
}

// QualifiedRef is how a foreign artifact is cited: "Landing gear / REQ-12".
func (l *LinkedArtifact) QualifiedRef() string {
	if l == nil {
		return ""
	}
	ref := l.Ref
	if ref == "" {
		ref = l.ID
		if len(ref) > 8 {
			ref = ref[:8]
		}
	}
	if l.ProjectName == "" {
		return ref
	}
	return l.ProjectName + " / " + ref
}

// LinkedByID indexes the foreign endpoints by artifact id.
func (e *ProjectExport) LinkedByID() map[string]*LinkedArtifact {
	out := make(map[string]*LinkedArtifact, len(e.LinkedArtifacts))
	for _, l := range e.LinkedArtifacts {
		if l != nil {
			out[l.ID] = l
		}
	}
	return out
}
