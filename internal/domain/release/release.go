// Package release reads the customer-facing release notes the binary was
// built with (RELEASE_NOTES.md at the repository root) and tells the rest of
// the platform which release this is and what changed in it.
//
// The file is the source of truth for both: the top section names the running
// release, so a deployment that carries no new section is not a new release as
// far as members are concerned, and one that does gets announced to every
// account exactly once (see internal/notify/release.go).
//
// A release is a semantic version, and its notes are grouped under the three
// headings a reader actually wants to tell apart: what is new, what was
// tidied, what was fixed. Only the parsed structure is ever served — never the
// file itself — because the file also carries an Unreleased section and, at
// one point, a block of instructions for contributors that customers had no
// business reading.
package release

import (
	"errors"
	"regexp"
	"strings"
)

// UnreleasedHeading is the section every pull request adds its notes to.
// Promotion turns it into a dated section.
const UnreleasedHeading = "Unreleased"

// versionPattern is what a release section heading must look like: a semantic
// version, optionally followed by an em-dashed date for a reader's benefit.
var versionPattern = regexp.MustCompile(`^(\d+\.\d+\.\d+)(?:\s+[—-]\s+(\d{4}-\d{2}-\d{2}))?$`)

// legacyVersionPattern matches the date headings used before OpenV had
// version numbers. They are kept because those releases were announced under
// those names and rewriting them would tell customers a different story than
// the one they were already told; nothing new is ever written in this shape.
var legacyVersionPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(\.\d+)?$`)

// semverPattern is a version heading with no date in it: what every release
// from 0.1.0 onwards is named.
var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// Headline names a release the way a member is told about it. Releases from
// before OpenV had version numbers are named by their date, and "upgraded to
// 2026-09-12" reads as nonsense, so those keep a plainer phrasing.
func Headline(version string) string {
	if semverPattern.MatchString(version) {
		return "OpenV version upgraded to " + version
	}
	return "OpenV update " + version
}

// Category names, in the order a reader wants them: what can I now do, what
// quietly improved, what stopped being broken.
const (
	CategoryFeatures    = "New features"
	CategoryMaintenance = "Maintenance updates"
	CategoryFixes       = "Bug fixes"
)

// CategoryOrder is every category, in reading order. A section may use any
// subset; empty ones are not rendered.
var CategoryOrder = []string{CategoryFeatures, CategoryMaintenance, CategoryFixes}

// categorySet is CategoryOrder as a lookup.
var categorySet = map[string]bool{
	CategoryFeatures: true, CategoryMaintenance: true, CategoryFixes: true,
}

// IsCategory reports whether a sub-heading names one of the three groups.
func IsCategory(heading string) bool { return categorySet[heading] }

// Category is one group of notes within a release.
type Category struct {
	Name  string   `json:"name"`
	Notes []string `json:"notes"`
}

// Release is one section of the notes.
type Release struct {
	// Version is the semantic version: 0.1.0. Releases from before OpenV had
	// version numbers carry their original date heading instead.
	Version string `json:"version"`
	// Date is the day it was promoted, when the heading names one.
	Date string `json:"date"`
	// Notes are every bullet in the section, in order, whatever group they
	// sit in. Kept flat as well as grouped because the notification and the
	// update banner want a plain list.
	Notes []string `json:"notes"`
	// Categories are the section's notes grouped under their headings, in
	// reading order, omitting any group the release had nothing for.
	Categories []Category `json:"categories"`
	// Markdown is the section's body as written, bullets and all.
	Markdown string `json:"markdown"`
	// StableSince is set when the release is a stable release: the day it
	// was designated one, from the marker line under its heading (see
	// stable.go). Empty for a plain nightly.
	StableSince string `json:"stable_since,omitempty"`
}

// Add files a bullet under a category, creating the group on first use so the
// order follows the file rather than CategoryOrder.
func (r *Release) Add(category, note string) {
	r.Notes = append(r.Notes, note)
	for i := range r.Categories {
		if r.Categories[i].Name == category {
			r.Categories[i].Notes = append(r.Categories[i].Notes, note)
			return
		}
	}
	r.Categories = append(r.Categories, Category{Name: category, Notes: []string{note}})
}

// Notes is the parsed file.
type Notes struct {
	// Unreleased holds the bullets waiting for the next promotion.
	Unreleased []string
	// UnreleasedCategories is the same bullets grouped, which is what decides
	// the size of the next version bump.
	UnreleasedCategories []Category
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

// extendLast appends to the most recent bullet, in both the flat list and the
// group it was filed under, so a wrapped line stays one note in each.
func (r *Release) extendLast(text string) {
	if len(r.Notes) == 0 {
		return
	}
	r.Notes[len(r.Notes)-1] += text
	if n := len(r.Categories); n > 0 {
		last := &r.Categories[n-1]
		if len(last.Notes) > 0 {
			last.Notes[len(last.Notes)-1] += text
		}
	}
}

// UnreleasedBy files a pending bullet under its category.
func (n *Notes) UnreleasedBy(category, note string) {
	for i := range n.UnreleasedCategories {
		if n.UnreleasedCategories[i].Name == category {
			n.UnreleasedCategories[i].Notes = append(n.UnreleasedCategories[i].Notes, note)
			return
		}
	}
	n.UnreleasedCategories = append(n.UnreleasedCategories, Category{Name: category, Notes: []string{note}})
}

func (n *Notes) extendLastUnreleased(text string) {
	if c := len(n.UnreleasedCategories); c > 0 {
		last := &n.UnreleasedCategories[c-1]
		if len(last.Notes) > 0 {
			last.Notes[len(last.Notes)-1] += text
		}
	}
}

// ErrMalformed reports a notes file the platform cannot make sense of.
var ErrMalformed = errors.New("malformed release notes")

var headingPattern = regexp.MustCompile(`^##\s+(.+?)\s*$`)

// categoryPattern matches the level-three heading that opens a group.
var categoryPattern = regexp.MustCompile(`^###\s+(.+?)\s*$`)

// UncategorizedNotes is where a bullet written before any category heading is
// filed. Legacy sections are entirely uncategorized; a new one should not be,
// and the release-notes check refuses a pull request that leaves one that way.
const UncategorizedNotes = "Changes"

// Parse reads the notes file. Only level-two headings structure it: one
// "Unreleased" section and any number of version sections. Anything above
// the first level-two heading is preamble and ignored. A heading that is
// neither is an error, since the version is what the platform compares to
// decide whether it has been updated, and a level-three heading that is not
// one of the three categories is an error too — a note filed under an
// invented group would simply not be shown.
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
	// category is the group bullets are currently being filed under. It resets
	// at every section, so a group heading never leaks across a release.
	category := UncategorizedNotes
	for _, line := range strings.Split(markdown, "\n") {
		if m := headingPattern.FindStringSubmatch(line); m != nil {
			flush()
			category = UncategorizedNotes
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
				parts := versionPattern.FindStringSubmatch(heading)
				current = &Release{Version: parts[1], Date: parts[2]}
			case legacyVersionPattern.MatchString(heading):
				if seen[heading] {
					return nil, errors.New("release notes: version " + heading + " appears twice")
				}
				current = &Release{Version: heading, Date: strings.SplitN(heading, ".", 2)[0]}
			default:
				return nil, errors.New("release notes: heading " + heading + " is neither Unreleased nor a version")
			}
			seen[heading] = true
			continue
		}
		if m := categoryPattern.FindStringSubmatch(line); m != nil {
			if !IsCategory(m[1]) {
				return nil, errors.New("release notes: " + m[1] + " is not one of " + strings.Join(CategoryOrder, ", "))
			}
			category = m[1]
			if current != nil {
				current.Markdown += line + "\n"
			}
			continue
		}
		if current != nil && current.StableSince == "" {
			if m := stableMarkerPattern.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				// The marker is metadata, not a note: it names the day the
				// release became the stable one and stays out of the body.
				current.StableSince = m[1]
				continue
			}
		}
		bullet, isBullet := bulletText(line)
		continues := !isBullet && isContinuation(line)
		switch {
		case current != nil:
			current.Markdown += line + "\n"
			if isBullet {
				current.Add(category, bullet)
			} else if continues && len(current.Notes) > 0 {
				current.extendLast(" " + strings.TrimSpace(line))
			}
		case inPending && isBullet:
			notes.Unreleased = append(notes.Unreleased, bullet)
			notes.UnreleasedBy(category, bullet)
		case inPending && continues && len(notes.Unreleased) > 0:
			notes.Unreleased[len(notes.Unreleased)-1] += " " + strings.TrimSpace(line)
			notes.extendLastUnreleased(" " + strings.TrimSpace(line))
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

// Service is what the API, the announcer and the stable scheduler need: the
// current release, the whole history, and the stable releases within it.
type Service interface {
	// Current is the running release; nil when the notes have none.
	Current() *Release
	// Released is every release, newest first, for a What's new page.
	//
	// Deliberately not the file: it also holds the Unreleased section and
	// whatever guidance for contributors happens to sit at the top, and
	// serving it whole once put both in front of customers.
	Released() []Release
	// CurrentStable is the newest stable release; nil until one is
	// designated.
	CurrentStable() *Stable
	// Stable looks a stable release up by version; nil when the notes do
	// not name it as one.
	Stable(version string) *Stable
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

// Released implements Service.
func (s *DefaultService) Released() []Release {
	if s == nil || s.notes == nil {
		return nil
	}
	out := make([]Release, len(s.notes.Releases))
	copy(out, s.notes.Releases)
	return out
}

// CurrentStable implements Service.
func (s *DefaultService) CurrentStable() *Stable { return s.notes.CurrentStable() }

// Stable implements Service.
func (s *DefaultService) Stable(version string) *Stable { return s.notes.Stable(version) }
