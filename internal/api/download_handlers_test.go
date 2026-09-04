package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/exports"
)

func queryRequest(t *testing.T, query string) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet, "/api/v1/projects/p1/download/json?"+query, nil)
}

// The defaults are the contract the old export endpoint had: no filters means
// the whole project, headings and all, with no attachment files.
func TestSelectionFromQueryDefaultsToTheWholeProject(t *testing.T) {
	got := selectionFromQuery(queryRequest(t, ""))
	if !got.IncludeHeadings {
		t.Error("headings should be in by default")
	}
	if len(got.Sections) != 0 || len(got.Types) != 0 || len(got.Attachments) != 0 {
		t.Errorf("selection = %+v, want no narrowing", got)
	}
	if got.NarrowsArtifacts() {
		t.Error("the default selection should narrow nothing")
	}
}

func TestSelectionFromQueryReadsTheFilters(t *testing.T) {
	got := selectionFromQuery(queryRequest(t, "sections=s1,s2&types=requirement&headings=0&attachments=figures,data"))
	if len(got.Sections) != 2 || got.Sections[1] != "s2" {
		t.Errorf("sections = %v", got.Sections)
	}
	if len(got.Types) != 1 || got.Types[0] != "requirement" {
		t.Errorf("types = %v", got.Types)
	}
	if got.IncludeHeadings {
		t.Error("headings=0 should drop the headings")
	}
	if !got.WantsAttachment(exports.CategoryFigures) || !got.WantsAttachment(exports.CategoryData) {
		t.Errorf("attachments = %v, want both categories", got.Attachments)
	}
}

func TestSelectionFromQueryAcceptsFalseForHeadings(t *testing.T) {
	if selectionFromQuery(queryRequest(t, "headings=false")).IncludeHeadings {
		t.Error("headings=false should drop the headings")
	}
	if !selectionFromQuery(queryRequest(t, "headings=1")).IncludeHeadings {
		t.Error("headings=1 should keep them")
	}
}

func TestCsvParamIgnoresBlanks(t *testing.T) {
	cases := map[string][]string{
		"":            nil,
		"   ":         nil,
		",,":          nil,
		"a, b ,,c":    {"a", "b", "c"},
		"figures":     {"figures"},
		"figures,":    {"figures"},
		" figures , ": {"figures"},
	}
	for in, want := range cases {
		got := csvParam(in)
		if len(got) != len(want) {
			t.Errorf("csvParam(%q) = %v, want %v", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("csvParam(%q) = %v, want %v", in, got, want)
				break
			}
		}
	}
}
