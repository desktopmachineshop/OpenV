package exports

import (
	"sort"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// A download is one snapshot of a project narrowed by a Selection and then
// rendered — as JSON, CSV, ReqIF, a PDF or a Word document, with the chosen
// attachments alongside it. Every format reads the same narrowed snapshot, so
// "the requirements sections, no headings, with their figures" means the same
// thing whichever button produced the file.
//
// The narrowing lives here, apart from any renderer and any transport, because
// it is the part with rules worth testing: what a section includes, what an
// empty list means, and what happens to a link whose other end was left out.

// Attachment categories a download can include. They are derived from the
// attachment itself rather than stored, so a project that has only ever held
// figures offers only figures.
const (
	// CategoryFigures is a numbered, citable figure (REQ-17-FIG-1).
	CategoryFigures = "figures"
	// CategoryImages is an image with no figure number — an attachment whose
	// artifact has no reference to build one from.
	CategoryImages = "images"
	// CategoryDocuments is a PDF, Word file or other prose document.
	CategoryDocuments = "documents"
	// CategoryData is tabular or structured data: CSV, spreadsheets, JSON, XML.
	CategoryData = "data"
	// CategoryOther is everything else, so nothing attached is unreachable.
	CategoryOther = "other"
)

// AttachmentCategory names the kind of file an attachment holds. The figure
// number wins over the mime type: a numbered drawing is a figure whatever
// format it was uploaded in.
func AttachmentCategory(a *attachments.Attachment) string {
	if a == nil {
		return CategoryOther
	}
	if a.FigureRef != "" {
		return CategoryFigures
	}
	mime := strings.ToLower(strings.TrimSpace(a.MimeType))
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	switch {
	case strings.HasPrefix(mime, "image/"):
		return CategoryImages
	case mime == "application/pdf",
		strings.HasPrefix(mime, "text/plain"),
		strings.HasPrefix(mime, "text/markdown"),
		strings.Contains(mime, "wordprocessingml"),
		mime == "application/msword",
		strings.Contains(mime, "opendocument.text"):
		return CategoryDocuments
	case mime == "text/csv",
		mime == "text/tab-separated-values",
		mime == "application/json",
		mime == "application/xml",
		mime == "text/xml",
		strings.Contains(mime, "spreadsheetml"),
		mime == "application/vnd.ms-excel",
		strings.Contains(mime, "opendocument.spreadsheet"):
		return CategoryData
	default:
		return CategoryOther
	}
}

// Selection narrows what a download contains. Every list is "empty means
// everything", so the zero Selection with IncludeHeadings set is the whole
// project — what the old export buttons produced.
type Selection struct {
	// Sections are artifact IDs whose subtrees to keep. An artifact is in when
	// it is one of these or sits underneath one.
	Sections []string `json:"sections,omitempty"`
	// Types are the artifact types to keep ("requirement", "test-case"). It
	// does not govern headings — IncludeHeadings does.
	Types []string `json:"types,omitempty"`
	// IncludeHeadings keeps the headings that organise the document. With it
	// off, a download is the artifacts alone.
	IncludeHeadings bool `json:"include_headings"`
	// Attachments are the categories of file to include beside the document.
	// Empty means none: attachments turn a download into an archive, so they
	// are opt-in.
	Attachments []string `json:"attachments,omitempty"`
}

// Everything is the selection the plain export has always produced: the whole
// project, headings included, no attachment files.
func Everything() Selection {
	return Selection{IncludeHeadings: true}
}

// NarrowsArtifacts reports whether the selection leaves anything out of the
// document itself. A selection that only adds attachments does not.
func (s Selection) NarrowsArtifacts() bool {
	return len(s.Sections) > 0 || len(s.Types) > 0 || !s.IncludeHeadings
}

// WantsAttachment reports whether files of a category belong in the download.
func (s Selection) WantsAttachment(category string) bool {
	for _, c := range s.Attachments {
		if c == category {
			return true
		}
	}
	return false
}

func toSet(values []string) map[string]bool {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]bool, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			set[v] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// inSections reports which artifacts sit in one of the chosen subtrees. A
// pre-existing parent cycle terminates rather than hanging: an artifact whose
// ancestry loops is simply not in a section.
func inSections(list []*artifacts.Artifact, roots map[string]bool) map[string]bool {
	if roots == nil {
		return nil
	}
	byID := make(map[string]*artifacts.Artifact, len(list))
	for _, a := range list {
		if a != nil {
			byID[a.ID] = a
		}
	}
	inside := make(map[string]bool, len(list))
	for _, a := range list {
		if a == nil {
			continue
		}
		seen := map[string]bool{}
		for cur := a; cur != nil && !seen[cur.ID]; {
			seen[cur.ID] = true
			if roots[cur.ID] {
				inside[a.ID] = true
				break
			}
			if cur.ParentID == nil {
				break
			}
			cur = byID[*cur.ParentID]
		}
	}
	return inside
}

// Apply returns a copy of the snapshot holding only what the selection asks
// for. The original is left alone: callers hand the same snapshot to more than
// one renderer.
//
// A link survives only when both of its ends do — half a link would assert a
// relationship to something the reader cannot see. Attachments survive when
// their artifact does and their category was asked for.
func Apply(data *ProjectExport, sel Selection) *ProjectExport {
	if data == nil {
		return nil
	}

	sections := inSections(data.Artifacts, toSet(sel.Sections))
	types := toSet(sel.Types)

	kept := make([]*artifacts.Artifact, 0, len(data.Artifacts))
	keptIDs := make(map[string]bool, len(data.Artifacts))
	for _, a := range data.Artifacts {
		if a == nil {
			continue
		}
		if sections != nil && !sections[a.ID] {
			continue
		}
		if a.Type == artifacts.TypeHeading {
			if !sel.IncludeHeadings {
				continue
			}
		} else if types != nil && !types[a.Type] {
			continue
		}
		kept = append(kept, a)
		keptIDs[a.ID] = true
	}

	out := *data
	out.Artifacts = kept
	out.Links = filterLinks(data.Links, keptIDs)
	out.Attachments = filterAttachments(data.Attachments, keptIDs)
	return &out
}

// Categories lists the attachment categories present in a snapshot, with a
// count and total size for each, ordered so the common ones come first. It is
// what a download form offers: the categories this project actually holds,
// rather than a fixed list that is mostly empty.
func Categories(list []*attachments.Attachment) []CategoryCount {
	counts := map[string]*CategoryCount{}
	for _, a := range list {
		if a == nil {
			continue
		}
		c := AttachmentCategory(a)
		if counts[c] == nil {
			counts[c] = &CategoryCount{Category: c}
		}
		counts[c].Count++
		counts[c].Bytes += a.FileSize
	}
	order := []string{CategoryFigures, CategoryImages, CategoryDocuments, CategoryData, CategoryOther}
	rank := map[string]int{}
	for i, c := range order {
		rank[c] = i
	}
	out := make([]CategoryCount, 0, len(counts))
	for _, c := range counts {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return rank[out[i].Category] < rank[out[j].Category] })
	return out
}

// CategoryCount is one attachment category and how much of it a project holds.
type CategoryCount struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
	Bytes    int    `json:"bytes"`
}

// filterLinks keeps only links whose two ends both survived the selection.
func filterLinks(list []*links.Link, kept map[string]bool) []*links.Link {
	out := make([]*links.Link, 0, len(list))
	for _, l := range list {
		if l == nil || !kept[l.FromID] || !kept[l.ToID] {
			continue
		}
		out = append(out, l)
	}
	return out
}

// filterAttachments keeps the attachments of surviving artifacts. An
// attachment describes the artifact it hangs on, so one whose artifact was
// left out goes with it.
//
// The selection's categories are NOT applied here: this is the snapshot's
// attachment metadata, which a JSON export carries so an import can restore
// it. Which files are actually packed beside the document is SelectedFiles.
func filterAttachments(list []*attachments.Attachment, kept map[string]bool) []*attachments.Attachment {
	out := make([]*attachments.Attachment, 0, len(list))
	for _, a := range list {
		if a == nil || !kept[a.ArtifactID] {
			continue
		}
		out = append(out, a)
	}
	return out
}

// SelectedFiles lists the attachment files to pack beside a rendered
// download: those already in the snapshot whose category was asked for.
func SelectedFiles(data *ProjectExport, sel Selection) []*attachments.Attachment {
	if data == nil || len(sel.Attachments) == 0 {
		return nil
	}
	out := make([]*attachments.Attachment, 0, len(data.Attachments))
	for _, a := range data.Attachments {
		if a != nil && sel.WantsAttachment(AttachmentCategory(a)) {
			out = append(out, a)
		}
	}
	return out
}
