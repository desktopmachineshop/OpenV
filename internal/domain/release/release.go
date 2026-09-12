// Package release reads the customer-facing release notes the binary was
// built with (RELEASE_NOTES.md at the repository root) and tells the rest of
// the platform which release this is and what changed in it.
//
// The file is the source of truth for both: the top dated section names the
// running release, so a deployment that carries no new section is not a new
// release as far as members are concerned, and one that does gets announced
// to every account exactly once (see internal/notify/release.go).
package release

import (
	"errors"
	"regexp"
	"strings"
)

// UnreleasedHeading is the section every pull request adds its notes to.
// Promotion turns it into a dated section.
const UnreleasedHeading = "Unreleased"

// versionPattern is what a released section heading must look like: the
// promotion date, optionally suffixed with .N for a second release that day.
var versionPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(\.\d+)?$`)

// Release is one dated section of the notes.
type Release struct {
	// Version is the heading text: 2026-09-12, or 2026-09-12.2.
	Version string `json:"version"`
	// Date is the version without its same-day suffix.
	Date string `json:"date"`
	// Notes are the section's bullet points, without the bullet marker.
	Notes []string `json:"notes"`
	// Markdown is the section's body as written, bullets and all.
	Markdown string `json:"markdown"`
}

// Notes is the parsed file.
type Notes struct {
	// Unreleased holds the bullets waiting for the next promotion.
	Unreleased []string
	// Releases are the dated sections, newest first as they appear in the
	// file.
	Releases []Release
	// Markdown is the whole file as written.
	Markdown string
}

// Current is the release the binary belongs to: the first dated section.
// nil when the file has none yet.
func (n *Notes) Current() *Release {
	if n == nil || len(n.Releases) == 0 {
		return nil
	}
	r := n.Releases[0]
	return &r
}

// ErrMalformed reports a notes file the platform cannot make sense of.
var ErrMalformed = errors.New("malformed release notes")

var headingPattern = regexp.MustCompile(`^##\s+(.+?)\s*$`)

// Parse reads the notes file. Only level-two headings structure it: one
// "Unreleased" section and any number of version sections. Anything above
// the first level-two heading is preamble and ignored. A version heading
// that is not a date is an error, since the version is what the platform
// compares to decide whether it has been updated.
func Parse(markdown string) (*Notes, error) {
	notes := &Notes{Markdown: markdown}
	var (
		current   *Release
		inPending bool
		seen      = map[string]bool{}
	)
	flush := func() {
		if current != nil {
			current.Markdown = strings.TrimSpace(current.Markdown)
			notes.Releases = append(notes.Releases, *current)
			current = nil
		}
		inPending = false
	}
	for _, line := range strings.Split(markdown, "\n") {
		if m := headingPattern.FindStringSubmatch(line); m != nil {
			flush()
			heading := m[1]
			switch {
			case heading == UnreleasedHeading:
				if seen[heading] {
					return nil, errors.New("release notes: more than one Unreleased section")
				}
				inPending = true
			case versionPattern.MatchString(heading):
				if seen[heading] {
					return nil, errors.New("release notes: version " + heading + " appears twice")
				}
				current = &Release{Version: heading, Date: strings.SplitN(heading, ".", 2)[0]}
			default:
				return nil, errors.New("release notes: heading " + heading + " is neither Unreleased nor a release date")
			}
			seen[heading] = true
			continue
		}
		bullet, isBullet := bulletText(line)
		continues := !isBullet && isContinuation(line)
		switch {
		case current != nil:
			current.Markdown += line + "\n"
			if isBullet {
				current.Notes = append(current.Notes, bullet)
			} else if continues && len(current.Notes) > 0 {
				current.Notes[len(current.Notes)-1] += " " + strings.TrimSpace(line)
			}
		case inPending && isBullet:
			notes.Unreleased = append(notes.Unreleased, bullet)
		case inPending && continues && len(notes.Unreleased) > 0:
			notes.Unreleased[len(notes.Unreleased)-1] += " " + strings.TrimSpace(line)
		}
	}
	flush()
	if len(notes.Releases) == 0 && !seen[UnreleasedHeading] {
		return nil, ErrMalformed
	}
	return notes, nil
}

// isContinuation reports whether a line carries on the bullet above it: a
// wrapped bullet is indented, and prose between bullets is not.
func isContinuation(line string) bool {
	return strings.TrimSpace(line) != "" && (strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t"))
}

// bulletText returns a bullet line's text, and whether the line is one.
func bulletText(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	for _, marker := range []string{"- ", "* "} {
		if strings.HasPrefix(trimmed, marker) {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, marker)), true
		}
	}
	return "", false
}

// Service is what the API and the announcer need: the current release and
// the whole history.
type Service interface {
	// Current is the running release; nil when the notes have none.
	Current() *Release
	// Markdown is the whole notes file, for a What's new page.
	Markdown() string
}

// DefaultService serves one parsed file.
type DefaultService struct {
	notes *Notes
}

// NewService parses the notes once. A malformed file is an error at boot
// rather than a silent absence of release information.
func NewService(markdown string) (*DefaultService, error) {
	notes, err := Parse(markdown)
	if err != nil {
		return nil, err
	}
	return &DefaultService{notes: notes}, nil
}

// Empty is a service with no notes at all: what a server falls back to when
// its notes file fails to parse, so a documentation mistake costs the
// release page, never the API.
func Empty() *DefaultService { return &DefaultService{notes: &Notes{}} }

// Current implements Service.
func (s *DefaultService) Current() *Release { return s.notes.Current() }

// Markdown implements Service.
func (s *DefaultService) Markdown() string { return s.notes.Markdown }
