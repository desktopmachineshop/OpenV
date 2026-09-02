package artifacts

import (
	"testing"
	"time"
)

// art builds a test artifact. parent "" means a root.
func art(id, parent, typ string, sortOrder int) *Artifact {
	a := &Artifact{
		ID:        id,
		Type:      typ,
		SortOrder: sortOrder,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if parent != "" {
		p := parent
		a.ParentID = &p
	}
	return a
}

// TestSectionNumbersNestByLevel is the shape the maintainer asked for:
//
//  1. Intro
//     1.1  Background
//     1.2  Goals
//  2. Requirements
//     2.1  Functional
func TestSectionNumbersNestByLevel(t *testing.T) {
	list := []*Artifact{
		art("intro", "", TypeHeading, 1),
		art("background", "intro", TypeHeading, 1),
		art("goals", "intro", TypeHeading, 2),
		art("reqs", "", TypeHeading, 2),
		art("functional", "reqs", TypeHeading, 1),
		art("deep", "functional", TypeHeading, 1),
	}

	got := SectionNumbers(list)
	want := map[string]string{
		"intro":      "1",
		"background": "1.1",
		"goals":      "1.2",
		"reqs":       "2",
		"functional": "2.1",
		"deep":       "2.1.1",
	}
	for id, expected := range want {
		if got[id] != expected {
			t.Errorf("%s = %q, want %q", id, got[id], expected)
		}
	}
}

// TestSectionNumbersFollowOrder is the property that separates a document
// number from a ref: move a section and the numbers move with it.
func TestSectionNumbersFollowOrder(t *testing.T) {
	intro := art("intro", "", TypeHeading, 1)
	reqs := art("reqs", "", TypeHeading, 2)
	list := []*Artifact{intro, reqs}

	if got := SectionNumbers(list); got["intro"] != "1" || got["reqs"] != "2" {
		t.Fatalf("before reorder: intro=%q reqs=%q", got["intro"], got["reqs"])
	}

	// Swap their positions in the document.
	intro.SortOrder, reqs.SortOrder = 2, 1

	got := SectionNumbers(list)
	if got["reqs"] != "1" || got["intro"] != "2" {
		t.Errorf("after reorder: reqs=%q intro=%q, want 1 and 2", got["reqs"], got["intro"])
	}
}

// TestSectionNumbersOnlyNumberHeadings: leaves are cited by their stable ref,
// so they get no clause number and — importantly — a requirement sitting
// between two headings does not consume a number.
func TestSectionNumbersOnlyNumberHeadings(t *testing.T) {
	list := []*Artifact{
		art("intro", "", TypeHeading, 1),
		art("req-a", "intro", TypeRequirement, 1),
		art("background", "intro", TypeHeading, 2),
		art("haz", "", TypeHazard, 2),
		art("reqs", "", TypeHeading, 3),
	}

	got := SectionNumbers(list)
	if _, ok := got["req-a"]; ok {
		t.Error("a requirement was given a section number")
	}
	if _, ok := got["haz"]; ok {
		t.Error("a hazard was given a section number")
	}
	if got["background"] != "1.1" {
		t.Errorf("background = %q, want 1.1 (the requirement must not consume 1.1)", got["background"])
	}
	if got["reqs"] != "2" {
		t.Errorf("reqs = %q, want 2 (the root hazard must not consume 2)", got["reqs"])
	}
}

// TestSectionNumbersThroughNonHeadingParent: a heading nested under a
// non-heading still numbers within its nearest heading ancestor rather than
// restarting at the root.
func TestSectionNumbersThroughNonHeadingParent(t *testing.T) {
	list := []*Artifact{
		art("intro", "", TypeHeading, 1),
		art("req-a", "intro", TypeRequirement, 1),
		art("sub", "req-a", TypeHeading, 1),
	}
	if got := SectionNumbers(list); got["sub"] != "1.1" {
		t.Errorf("sub = %q, want 1.1", got["sub"])
	}
}

// TestSectionNumbersDeterministicForEqualSortOrders keeps numbering stable
// across calls when siblings share a sort order — otherwise a report would
// renumber itself between two runs with no edit in between.
func TestSectionNumbersDeterministicForEqualSortOrders(t *testing.T) {
	build := func() []*Artifact {
		return []*Artifact{
			art("b", "", TypeHeading, 1),
			art("a", "", TypeHeading, 1),
			art("c", "", TypeHeading, 1),
		}
	}
	first := SectionNumbers(build())
	for i := 0; i < 5; i++ {
		again := SectionNumbers(build())
		for id, n := range first {
			if again[id] != n {
				t.Fatalf("run %d: %s = %q, first run said %q", i, id, again[id], n)
			}
		}
	}
	// Tie-break is by ID, so the alphabetical order is the document order.
	if first["a"] != "1" || first["b"] != "2" || first["c"] != "3" {
		t.Errorf("tie-break order = a:%s b:%s c:%s", first["a"], first["b"], first["c"])
	}
}

// TestSectionNumbersSurviveBrokenTrees: a dangling parent (filtered-out or
// deleted ancestor) and a parent cycle must not hang or panic — numbering
// runs on live data from an API request.
func TestSectionNumbersSurviveBrokenTrees(t *testing.T) {
	orphan := art("orphan", "missing-parent", TypeHeading, 1)
	if got := SectionNumbers([]*Artifact{orphan}); got["orphan"] != "1" {
		t.Errorf("orphan = %q, want 1 (dangling parents are roots)", got["orphan"])
	}

	// A two-node cycle: each claims the other as parent.
	x, y := art("x", "y", TypeHeading, 1), art("y", "x", TypeHeading, 1)
	done := make(chan map[string]string, 1)
	go func() { done <- SectionNumbers([]*Artifact{x, y}) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SectionNumbers did not terminate on a parent cycle")
	}

	if got := SectionNumbers(nil); len(got) != 0 {
		t.Errorf("empty input returned %v", got)
	}
}

// TestApplySectionNumbersStampsOnlyThePage: numbering is computed from the
// whole document but written onto the subset being served, so a type-filtered
// page still shows the numbers the full document would give it.
func TestApplySectionNumbersStampsOnlyThePage(t *testing.T) {
	all := []*Artifact{
		art("intro", "", TypeHeading, 1),
		art("background", "intro", TypeHeading, 1),
		art("req", "background", TypeRequirement, 1),
	}
	page := []*Artifact{all[1], all[2]}

	ApplySectionNumbers(all, page)
	if page[0].DocNumber != "1.1" {
		t.Errorf("background DocNumber = %q, want 1.1", page[0].DocNumber)
	}
	if page[1].DocNumber != "" {
		t.Errorf("requirement DocNumber = %q, want empty", page[1].DocNumber)
	}
	// The artifact left out of the page is untouched.
	if all[0].DocNumber != "" {
		t.Errorf("off-page artifact was stamped: %q", all[0].DocNumber)
	}
}
