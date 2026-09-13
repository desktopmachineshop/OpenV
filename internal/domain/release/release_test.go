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
* fix: Pending fix

## 2026.09

Cut on 2026-10-01 from 2026-09-12.2.

### Changes

- Second of the day
  continued line

### Fixes

- A fix carried into stable

## 2026-09-12.2

- Second of the day
  continued line

Some prose in the section.

## 2026-09-12

- First of the day
- Fix: another
`

func TestParseSectionsAndBullets(t *testing.T) {
	n, err := Parse(sample)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(n.Unreleased) != 2 || n.Unreleased[0].Text != "Pending one wrapped onto a second line" || n.Unreleased[0].Fix {
		t.Fatalf("unreleased = %+v", n.Unreleased)
	}
	if !n.Unreleased[1].Fix || n.Unreleased[1].Text != "Pending fix" {
		t.Fatalf("fix bullet = %+v", n.Unreleased[1])
	}
	if len(n.Releases) != 2 {
		t.Fatalf("releases = %d, want 2", len(n.Releases))
	}
	cur := n.Current()
	if cur == nil || cur.Version != "2026-09-12.2" || cur.Date != "2026-09-12" {
		t.Fatalf("current = %+v", cur)
	}
	if len(cur.Notes) != 1 || cur.Notes[0].Text != "Second of the day continued line" {
		t.Fatalf("current notes = %+v", cur.Notes)
	}
	if !strings.Contains(cur.Markdown, "Some prose in the section.") || strings.HasSuffix(cur.Markdown, "\n") {
		t.Fatalf("current markdown = %q", cur.Markdown)
	}
	older := n.Releases[1]
	if older.Version != "2026-09-12" || len(older.Notes) != 2 || !older.Notes[1].Fix || older.Notes[1].Text != "another" {
		t.Fatalf("older release = %+v", older)
	}
}

func TestParseStableSections(t *testing.T) {
	n, err := Parse(sample)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	s := n.CurrentStable()
	if s == nil || s.Version != "2026.09" || s.CutOn != "2026-10-01" || s.CutFrom != "2026-09-12.2" {
		t.Fatalf("stable = %+v", s)
	}
	if len(s.Notes) != 2 || s.Notes[0].Text != "Second of the day continued line" || s.Notes[1].Text != "A fix carried into stable" {
		t.Fatalf("stable notes = %+v", s.Notes)
	}
	if n.Stable("2026.09") == nil || n.Stable("2026.08") != nil {
		t.Fatalf("Stable lookup")
	}
	if _, err := Parse("## 2026.09\n\n- no cut line\n"); err == nil {
		t.Fatalf("a stable without a cut line parsed")
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
	if !NightlyAtOrBefore("2026-09-12", "2026-09-12.2") || NightlyAtOrBefore("2026-09-13", "2026-09-12.2") || !NightlyAtOrBefore("2026-09-12", "2026-09-12") {
		t.Fatalf("NightlyAtOrBefore")
	}
	if NightlyAtOrBefore("garbage", "2026-09-12") {
		t.Fatalf("unparseable version passed a gate")
	}
	if !StableNewer("2026.10", "2026.09") || !StableNewer("2026.09.1", "2026.09") || StableNewer("2026.09", "2026.09") || !StableNewer("2026.09", "") {
		t.Fatalf("StableNewer")
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
	if svc.Markdown() != openv.ReleaseNotesMarkdown {
		t.Fatalf("Markdown() is not the file")
	}
}
