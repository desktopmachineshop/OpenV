package reports

import (
	"fmt"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/products"
)

// contentsFixture is a project with a product definition and the given
// number of sections, each holding one requirement: a contents entry per
// section, plus one for the product definition.
func contentsFixture(sections int) *exports.ProjectExport {
	data := &exports.ProjectExport{
		ProjectName:    "Contents probe",
		ProductProfile: &products.ProductProfile{Vision: "Every entry names its own page."},
	}
	for i := 1; i <= sections; i++ {
		heading := fmt.Sprintf("h%d", i)
		data.Artifacts = append(data.Artifacts,
			&artifacts.Artifact{ID: heading, Type: artifacts.TypeHeading, Ref: fmt.Sprintf("HDG-%d", i),
				Title: fmt.Sprintf("Section %d", i), SortOrder: i, Version: 1},
			&artifacts.Artifact{ID: fmt.Sprintf("r%d", i), ParentID: ptr(heading), Type: artifacts.TypeRequirement,
				Ref: fmt.Sprintf("REQ-%d", i), Title: fmt.Sprintf("Requirement %d", i),
				Body: "The system shall keep its place in the contents.", SortOrder: 1, Version: 1})
	}
	return data
}

// pdfPageCount counts a PDF's page objects.
func pdfPageCount(pdf []byte) int {
	s := string(pdf)
	return strings.Count(s, "/Type /Page\n") + strings.Count(s, "/Type /Page ")
}

// TestPDFContentsNameTheirOwnPages: every contents entry names the page its
// section starts on, and the body follows the contents with no blank page
// between them (#379 bug 66). A PDF with contents is the one without them
// plus the contents' own pages. Forty sections and the product definition
// are one entry more than a contents page holds, so the contents take two
// pages and both are filled.
func TestPDFContentsNameTheirOwnPages(t *testing.T) {
	for _, sections := range []int{3, 40} {
		t.Run(fmt.Sprintf("%d sections", sections), func(t *testing.T) {
			data := contentsFixture(sections)
			opts := RenderOptions{Content: exports.DefaultContent()}
			opts.Content.VVStatus = false

			r, out, err := renderReportPDF(data, opts)
			if err != nil {
				t.Fatalf("pdf: %v", err)
			}
			entries := r.m.tocEntries()
			if len(entries) != sections+1 {
				t.Fatalf("contents entries = %d, want %d", len(entries), sections+1)
			}
			for _, e := range entries {
				if printed, at := r.contentsPages[e.key], r.headingPages[e.key]; printed != at {
					t.Errorf("contents entry %q names page %d, but it starts on page %d", e.title, printed, at)
				}
			}
			contentsPages := tocPageCount(len(entries))
			if got, want := r.headingPages[entries[0].key], 1+contentsPages+1; got != want {
				t.Errorf("the first section starts on page %d, want %d: right after the cover and %d page(s) of contents",
					got, want, contentsPages)
			}

			without := opts
			without.Content.TOC = false
			_, plain, err := renderReportPDF(data, without)
			if err != nil {
				t.Fatalf("pdf without contents: %v", err)
			}
			if got, want := pdfPageCount(out), pdfPageCount(plain)+contentsPages; got != want {
				t.Errorf("pages = %d, want %d: the %d without contents and %d of contents, none blank",
					got, want, pdfPageCount(plain), contentsPages)
			}
		})
	}
}
