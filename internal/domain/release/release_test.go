package release

import (
	"strings"
	"testing"

	openv "github.com/openv/requirements-platform"
)

const sample = `# Notes

Preamble that is ignored.

## Unreleased

### New features

- Pending one
  wrapped onto a second line

### Bug fixes

- Pending fix

## 0.3.0 — 2026-09-20

### New features

- Third feature

## 0.2.1 — 2026-09-16

Stable channel release since 2026-09-23.

### Bug fixes

- A fix carried into stable

## 0.2.0 — 2026-09-14

### New features

- Second of the day
  continued line

Some prose in the section.

## 0.1.0 — 2026-09-13

Stable channel release since 2026-09-13.

### Maintenance updates

- Tidied

## 2026-09-12

- First of the day
- Fix: another
`

func TestParseSectionsAndBullets(t *testing.T) {
	n, err := Parse(sample)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(n.Unreleased) != 2 || n.Unreleased[0] != "Pending one wrapped onto a second line" || n.Unreleased[1] != "Pending fix" {
		t.Fatalf("unreleased = %+v", n.Unreleased)
	}
	if len(n.Releases) != 5 {
		t.Fatalf("releases = %d, want 5", len(n.Releases))
	}
	cur := n.Current()
	if cur == nil || cur.Version != "0.3.0" || cur.Date != "2026-09-20" || cur.StableSince != "" {
		t.Fatalf("current = %+v", cur)
	}
	second := n.Releases[2]
	if len(second.Notes) != 1 || second.Notes[0] != "Second of the day continued line" {
		t.Fatalf("0.2.0 notes = %+v", second.Notes)
	}
	if !strings.Contains(second.Markdown, "Some prose in the section.") || strings.HasSuffix(second.Markdown, "\n") {
		t.Fatalf("0.2.0 markdown = %q", second.Markdown)
	}
	older := n.Releases[4]
	if older.Version != "2026-09-12" || len(older.Notes) != 2 || older.Notes[1] != "Fix: another" {
		t.Fatalf("older release = %+v", older)
	}
}

// TestParseStableMarker: the marker line designates a release as stable and
// stays out of its notes and body; the stable's notes are every release
// since the previous stable, merged group by group.
func TestParseStableMarker(t *testing.T) {
	n, err := Parse(sample)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	s := n.CurrentStable()
	if s == nil || s.Version != "0.2.1" || s.Since != "2026-09-23" || s.Previous != "0.1.0" {
		t.Fatalf("stable = %+v", s)
	}
	if got := strings.Join(s.Notes, "|"); got != "Second of the day continued line|A fix carried into stable" {
		t.Fatalf("stable notes = %q", got)
	}
	if len(s.Categories) != 2 || s.Categories[0].Name != CategoryFeatures || s.Categories[1].Name != CategoryFixes {
		t.Fatalf("stable groups = %+v", s.Categories)
	}
	marked := n.Releases[1]
	if marked.StableSince != "2026-09-23" || strings.Contains(marked.Markdown, StableMarkerPrefix) || len(marked.Notes) != 1 {
		t.Fatalf("marked release = %+v", marked)
	}
	// The first stable stops at the legacy releases: they predate the channel.
	first := n.Stable("0.1.0")
	if first == nil || first.Previous != "" || len(first.Notes) != 1 || first.Notes[0] != "Tidied" {
		t.Fatalf("first stable = %+v", first)
	}
	if n.Stable("0.2.0") != nil || n.Stable("0.9.0") != nil {
		t.Fatalf("an undesignated release was looked up as stable")
	}
	if StableMarker("2026-10-01") != "Stable channel release since 2026-10-01." {
		t.Fatalf("StableMarker = %q", StableMarker("2026-10-01"))
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
	if n.Current() != nil || n.CurrentStable() != nil {
		t.Fatalf("current = %+v, want nil", n.Current())
	}
}

func TestVersionOrdering(t *testing.T) {
	if !AtOrBefore("2026-09-12", "2026-09-12.2") || AtOrBefore("2026-09-13", "2026-09-12.2") || !AtOrBefore("2026-09-12", "2026-09-12") {
		t.Fatalf("AtOrBefore on dated versions")
	}
	if !AtOrBefore("0.2.0", "0.10.0") || AtOrBefore("1.0.0", "0.10.0") || !AtOrBefore("2026-09-12.3", "0.1.0") {
		t.Fatalf("AtOrBefore on numbered versions")
	}
	if AtOrBefore("garbage", "0.1.0") || AtOrBefore("0.1.0", "garbage") {
		t.Fatalf("unparseable version passed a gate")
	}
	if !Newer("0.2.0", "0.1.9") || !Newer("0.1.1", "0.1.0") || Newer("0.1.0", "0.1.0") || !Newer("0.1.0", "") || Newer("", "0.1.0") {
		t.Fatalf("Newer")
	}
	if Compare("0.1.0", "2026-09-12.3") <= 0 {
		t.Fatalf("a numbered release must outrank every dated one")
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
