package release

import (
	"strings"
	"testing"

	openv "github.com/openv/requirements-platform"
)

const sample = `# Notes

Preamble that is ignored.

## Unreleased

- Pending one
  wrapped onto a second line
* Pending two

## 2026-09-12.2

- Second of the day
  continued line

Some prose in the section.

## 2026-09-12

- First of the day
- Another
`

func TestParseSectionsAndBullets(t *testing.T) {
	n, err := Parse(sample)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := strings.Join(n.Unreleased, "|"); got != "Pending one wrapped onto a second line|Pending two" {
		t.Fatalf("unreleased = %q", got)
	}
	if len(n.Releases) != 2 {
		t.Fatalf("releases = %d, want 2", len(n.Releases))
	}
	cur := n.Current()
	if cur == nil || cur.Version != "2026-09-12.2" || cur.Date != "2026-09-12" {
		t.Fatalf("current = %+v", cur)
	}
	if len(cur.Notes) != 1 || cur.Notes[0] != "Second of the day continued line" {
		t.Fatalf("current notes = %q", cur.Notes)
	}
	if !strings.Contains(cur.Markdown, "Some prose in the section.") || strings.HasSuffix(cur.Markdown, "\n") {
		t.Fatalf("current markdown = %q", cur.Markdown)
	}
	if n.Releases[1].Version != "2026-09-12" || len(n.Releases[1].Notes) != 2 {
		t.Fatalf("older release = %+v", n.Releases[1])
	}
}

func TestParseRejectsBadHeadingsAndDuplicates(t *testing.T) {
	for name, md := range map[string]string{
		"not a date":     "## Unreleased\n\n## v1.2\n- x\n",
		"duplicate":      "## 2026-09-12\n- a\n## 2026-09-12\n- b\n",
		"two unreleased": "## Unreleased\n## Unreleased\n",
		"no sections":    "# Just a title\n\nprose\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(md); err == nil {
				t.Fatalf("Parse accepted %q", md)
			}
		})
	}
}

// TestParseNoReleaseYet: a file with only an Unreleased section is valid and
// has no current release, so nothing is announced.
func TestParseNoReleaseYet(t *testing.T) {
	n, err := Parse("## Unreleased\n\n- soon\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if n.Current() != nil {
		t.Fatalf("current = %+v, want nil", n.Current())
	}
}

// TestEmbeddedNotesParse pins the file the binary ships with: it must parse,
// and must name a release, or the server would boot with nothing to serve.
func TestEmbeddedNotesParse(t *testing.T) {
	svc, err := NewService(openv.ReleaseNotesMarkdown)
	if err != nil {
		t.Fatalf("RELEASE_NOTES.md does not parse: %v", err)
	}
	if svc.Current() == nil {
		t.Fatalf("RELEASE_NOTES.md names no release")
	}
	// The served history is the parsed sections, never the file: the file
	// also holds the Unreleased section and, historically, a block of
	// instructions for contributors that customers were shown.
	released := svc.Released()
	if len(released) == 0 {
		t.Fatalf("Released() names no release")
	}
	for _, r := range released {
		if strings.Contains(r.Markdown, "pull request") {
			t.Errorf("release %s carries contributor guidance: %q", r.Version, r.Markdown)
		}
	}
}

// The shape the notes are written in now: a semantic version and the day it
// shipped, with the bullets grouped under the three headings a reader wants
// to tell apart.
const numbered = `# Notes

## Unreleased

### Bug fixes

- Pending fix

## 0.2.0 — 2026-09-14

### New features

- Export to Excel
- Import from Excel

### Bug fixes

- Baselines load again

## 2026-09-12

- From before the numbering
`

func TestParseGroupsNotesUnderTheirHeadings(t *testing.T) {
	n, err := Parse(numbered)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cur := n.Current()
	if cur == nil || cur.Version != "0.2.0" || cur.Date != "2026-09-14" {
		t.Fatalf("current = %+v", cur)
	}
	// Flat as well as grouped: the update banner and the notification want a
	// plain list, in the order the file wrote them.
	if got := strings.Join(cur.Notes, "|"); got != "Export to Excel|Import from Excel|Baselines load again" {
		t.Fatalf("notes = %q", got)
	}
	if len(cur.Categories) != 2 {
		t.Fatalf("categories = %+v", cur.Categories)
	}
	if cur.Categories[0].Name != CategoryFeatures || len(cur.Categories[0].Notes) != 2 {
		t.Errorf("first group = %+v", cur.Categories[0])
	}
	if cur.Categories[1].Name != CategoryFixes || cur.Categories[1].Notes[0] != "Baselines load again" {
		t.Errorf("second group = %+v", cur.Categories[1])
	}
	if len(n.UnreleasedCategories) != 1 || n.UnreleasedCategories[0].Name != CategoryFixes {
		t.Errorf("unreleased groups = %+v", n.UnreleasedCategories)
	}
}

// The releases from before OpenV had version numbers keep their date heading
// and have no groups. Renaming them would announce them a second time to
// every account, since the claim is keyed on the version string.
func TestALegacySectionIsReadAsItWasWritten(t *testing.T) {
	n, err := Parse(numbered)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	old := n.Releases[1]
	if old.Version != "2026-09-12" || old.Date != "2026-09-12" {
		t.Fatalf("legacy release = %+v", old)
	}
	if len(old.Categories) != 1 || old.Categories[0].Name != UncategorizedNotes {
		t.Fatalf("legacy groups = %+v", old.Categories)
	}
}

// A group heading nobody recognises would put its bullets somewhere the app
// never renders, so it is refused where it is cheap to fix.
func TestParseRejectsAnUnknownGroup(t *testing.T) {
	if _, err := Parse("## Unreleased\n\n### Improvements\n\n- x\n"); err == nil {
		t.Fatal("Parse accepted an invented group heading")
	}
}

// A group heading never leaks past the section it was written in.
func TestAGroupDoesNotCarryIntoTheNextSection(t *testing.T) {
	n, err := Parse("## 0.2.0\n\n### New features\n\n- New\n\n## 2026-09-12\n\n- Old\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if n.Releases[1].Categories[0].Name != UncategorizedNotes {
		t.Fatalf("the group leaked into the legacy section: %+v", n.Releases[1].Categories)
	}
}

func TestHeadlineNamesAReleaseTheWayItIsAnnounced(t *testing.T) {
	if got := Headline("0.2.0"); got != "OpenV version upgraded to 0.2.0" {
		t.Errorf("Headline(0.2.0) = %q", got)
	}
	// "upgraded to 2026-09-12" reads as nonsense.
	if got := Headline("2026-09-12.2"); got != "OpenV update 2026-09-12.2" {
		t.Errorf("Headline of a legacy version = %q", got)
	}
}
