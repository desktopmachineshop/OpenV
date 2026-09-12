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
	if svc.Markdown() != openv.ReleaseNotesMarkdown {
		t.Fatalf("Markdown() is not the file")
	}
}
