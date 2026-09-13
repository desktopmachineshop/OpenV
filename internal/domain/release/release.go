// Package release reads the customer-facing release notes the binary was
// built with (RELEASE_NOTES.md at the repository root) and tells the rest of
// the platform which release this is, which stable release exists, and what
// changed in each.
//
// The file is the source of truth for all of it: the newest nightly section
// names the running release, the newest stable section names the stable
// release stable-channel workspaces move to at their upgrade time, and the
// nightly a stable was cut from decides which gated features it carries
// (see features.go and docs/release-policy.md).
package release

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// UnreleasedHeading is the section every pull request adds its notes to.
// Promotion turns it into a dated section.
const UnreleasedHeading = "Unreleased"

// FixPrefix marks a bullet as a fix rather than a change. Fixes reach every
// channel at the next nightly; changes are what stable-channel workspaces
// wait for.
const FixPrefix = "fix:"

var (
	// nightlyPattern is a nightly release heading: the promotion date, with
	// .N for a second release that day.
	nightlyPattern = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})(?:\.(\d+))?$`)
	// stablePattern is a stable release heading: year.month, with .P for a
	// fix release.
	stablePattern = regexp.MustCompile(`^(\d{4})\.(\d{2})(?:\.(\d+))?$`)
	// cutLinePattern is the first line of a stable section, recording when
	// it was cut and from which nightly.
	cutLinePattern = regexp.MustCompile(`^Cut on (\d{4}-\d{2}-\d{2}) from (\d{4}-\d{2}-\d{2}(?:\.\d+)?)\.?\s*$`)
	headingPattern = regexp.MustCompile(`^##\s+(.+?)\s*$`)
)

// Note is one bullet of a release.
type Note struct {
	Text string `json:"text"`
	// Fix marks a bullet written as "fix: …": delivered to every channel at
	// the next nightly rather than held for a stable release.
	Fix bool `json:"fix"`
}

// Release is one nightly section of the notes.
type Release struct {
	// Version is the heading text: 2026-09-12, or 2026-09-12.2.
	Version string `json:"version"`
	// Date is the version without its same-day suffix.
	Date string `json:"date"`
	// Notes are the section's bullet points, without the bullet marker.
	Notes []Note `json:"notes"`
	// Markdown is the section's body as written, bullets and all.
	Markdown string `json:"markdown"`
}

// Stable is one stable section of the notes: a month's release, or a fix
// release of it.
type Stable struct {
	// Version is the heading text: 2026.09, or 2026.09.1 for a fix release.
	Version string `json:"version"`
	// CutOn is the date the release was cut, from the section's first line.
	CutOn string `json:"cut_on"`
	// CutFrom is the nightly the release was cut from: every nightly up to
	// and including it is part of this stable release.
	CutFrom string `json:"cut_from"`
	Notes   []Note `json:"notes"`
	// Markdown is the section's body as written.
	Markdown string `json:"markdown"`
}

// Notes is the parsed file.
type Notes struct {
	// Unreleased holds the bullets waiting for the next promotion.
	Unreleased []Note
	// Releases are the nightly sections, newest first as they appear in
	// the file.
	Releases []Release
	// Stables are the stable sections, newest first.
	Stables []Stable
	// Markdown is the whole file as written.
	Markdown string
}

// Current is the release the binary belongs to: the newest nightly section.
// nil when the file has none yet.
func (n *Notes) Current() *Release {
	if n == nil || len(n.Releases) == 0 {
		return nil
	}
	r := n.Releases[0]
	return &r
}

// CurrentStable is the newest stable section; nil when none has been cut.
func (n *Notes) CurrentStable() *Stable {
	if n == nil || len(n.Stables) == 0 {
		return nil
	}
	s := n.Stables[0]
	return &s
}

// Stable finds a stable section by version; nil when absent.
func (n *Notes) Stable(version string) *Stable {
	if n == nil {
		return nil
	}
	for _, s := range n.Stables {
		if s.Version == version {
			s := s
			return &s
		}
	}
	return nil
}

// ErrMalformed reports a notes file the platform cannot make sense of.
var ErrMalformed = errors.New("malformed release notes")

// Parse reads the notes file. Only level-two headings structure it: one
// "Unreleased" section, nightly sections and stable sections in any order
// (newest first by convention). Anything above the first level-two heading
// is preamble and ignored. A heading that is none of those is an error,
// since the version is what the platform compares to decide whether it has
// been updated.
func Parse(markdown string) (*Notes, error) {
	notes := &Notes{Markdown: markdown}
	var (
		nightly   *Release
		stable    *Stable
		inPending bool
		seen      = map[string]bool{}
	)
	flush := func() {
		if nightly != nil {
			nightly.Markdown = strings.TrimSpace(nightly.Markdown)
			notes.Releases = append(notes.Releases, *nightly)
			nightly = nil
		}
		if stable != nil {
			stable.Markdown = strings.TrimSpace(stable.Markdown)
			notes.Stables = append(notes.Stables, *stable)
			stable = nil
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
			case nightlyPattern.MatchString(heading):
				if seen[heading] {
					return nil, errors.New("release notes: version " + heading + " appears twice")
				}
				nightly = &Release{Version: heading, Date: strings.SplitN(heading, ".", 2)[0]}
			case stablePattern.MatchString(heading):
				if seen[heading] {
					return nil, errors.New("release notes: version " + heading + " appears twice")
				}
				stable = &Stable{Version: heading}
			default:
				return nil, errors.New("release notes: heading " + heading + " is neither Unreleased, a nightly date nor a stable version")
			}
			seen[heading] = true
			continue
		}
		bullet, isBullet := bulletText(line)
		continues := !isBullet && isContinuation(line)
		switch {
		case nightly != nil:
			nightly.Markdown += line + "\n"
			nightly.Notes = appendNote(nightly.Notes, bullet, isBullet, continues, line)
		case stable != nil:
			stable.Markdown += line + "\n"
			if m := cutLinePattern.FindStringSubmatch(strings.TrimSpace(line)); m != nil && stable.CutOn == "" {
				stable.CutOn, stable.CutFrom = m[1], m[2]
				continue
			}
			stable.Notes = appendNote(stable.Notes, bullet, isBullet, continues, line)
		case inPending:
			notes.Unreleased = appendNote(notes.Unreleased, bullet, isBullet, continues, line)
		}
	}
	flush()
	if len(notes.Releases) == 0 && len(notes.Stables) == 0 && !seen[UnreleasedHeading] {
		return nil, ErrMalformed
	}
	for _, s := range notes.Stables {
		if s.CutFrom == "" {
			return nil, errors.New("release notes: stable " + s.Version + " has no 'Cut on … from …' line")
		}
	}
	return notes, nil
}

// appendNote adds a bullet, or joins a wrapped line onto the last one.
func appendNote(notes []Note, bullet string, isBullet, continues bool, line string) []Note {
	switch {
	case isBullet:
		return append(notes, classify(bullet))
	case continues && len(notes) > 0:
		notes[len(notes)-1].Text += " " + strings.TrimSpace(line)
	}
	return notes
}

// classify reads the fix marker off a bullet.
func classify(text string) Note {
	if len(text) >= len(FixPrefix) && strings.EqualFold(text[:len(FixPrefix)], FixPrefix) {
		return Note{Text: strings.TrimSpace(text[len(FixPrefix):]), Fix: true}
	}
	return Note{Text: text}
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

// NightlyKey orders nightly versions: (year, month, day, n). ok is false
// for anything that is not a nightly version.
func NightlyKey(version string) (key [4]int, ok bool) {
	m := nightlyPattern.FindStringSubmatch(version)
	if m == nil {
		return key, false
	}
	key[0], _ = strconv.Atoi(m[1])
	key[1], _ = strconv.Atoi(m[2])
	key[2], _ = strconv.Atoi(m[3])
	key[3] = 1
	if m[4] != "" {
		key[3], _ = strconv.Atoi(m[4])
	}
	return key, true
}

// StableKey orders stable versions: (year, month, patch).
func StableKey(version string) (key [3]int, ok bool) {
	m := stablePattern.FindStringSubmatch(version)
	if m == nil {
		return key, false
	}
	key[0], _ = strconv.Atoi(m[1])
	key[1], _ = strconv.Atoi(m[2])
	if m[3] != "" {
		key[2], _ = strconv.Atoi(m[3])
	}
	return key, true
}

// NightlyAtOrBefore reports whether nightly version a is the same as, or
// older than, nightly version b. Unparseable versions compare as newer, so
// a typo never sneaks a feature past a gate.
func NightlyAtOrBefore(a, b string) bool {
	ka, okA := NightlyKey(a)
	kb, okB := NightlyKey(b)
	if !okA || !okB {
		return false
	}
	for i := range ka {
		if ka[i] != kb[i] {
			return ka[i] < kb[i]
		}
	}
	return true
}

// StableNewer reports whether stable version a is newer than b. An empty
// or unparseable b counts as older than any real a.
func StableNewer(a, b string) bool {
	ka, okA := StableKey(a)
	if !okA {
		return false
	}
	kb, okB := StableKey(b)
	if !okB {
		return true
	}
	for i := range ka {
		if ka[i] != kb[i] {
			return ka[i] > kb[i]
		}
	}
	return false
}

// Service is what the API, the announcer and the feature gate need.
type Service interface {
	// Current is the running nightly release; nil when the notes have none.
	Current() *Release
	// CurrentStable is the newest stable release; nil when none is cut.
	CurrentStable() *Stable
	// Stable looks a stable release up by version; nil when absent.
	Stable(version string) *Stable
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

// CurrentStable implements Service.
func (s *DefaultService) CurrentStable() *Stable { return s.notes.CurrentStable() }

// Stable implements Service.
func (s *DefaultService) Stable(version string) *Stable { return s.notes.Stable(version) }

// Markdown implements Service.
func (s *DefaultService) Markdown() string { return s.notes.Markdown }
