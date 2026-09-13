package release

import (
	"regexp"
	"strconv"
	"strings"
)

// Stable releases (docs/release-policy.md, REQ-135).
//
// Every release is a nightly: the promotion of a green master with new
// notes, numbered by what those notes say. A stable release is not a
// separate build. It is one of those releases designated, once it has
// served the nightly channel for a week, as the one stable-channel
// workspaces move to, by one marker line under its heading:
//
//	## 0.4.0 — 2026-10-01
//
//	Stable channel release since 2026-10-08.
//
// Its notes, for the members who moved to it, are everything since the
// previous stable release: the releases in between merged, group by group.
// The marker is what the Cut stable release workflow writes
// (scripts/release_notes.py cut-stable); nothing else in the file changes.

// StableMarkerPrefix opens the marker line. The date completes it.
const StableMarkerPrefix = "Stable channel release since "

var stableMarkerPattern = regexp.MustCompile(`^Stable channel release since (\d{4}-\d{2}-\d{2})\.?$`)

// StableMarker renders the marker line for a release designated on since.
func StableMarker(since string) string { return StableMarkerPrefix + since + "." }

// Stable is a designated release with the notes of every release since the
// previous stable one.
type Stable struct {
	Version string `json:"version"`
	// Since is the day it became the stable release.
	Since string `json:"since"`
	// Previous is the stable release before it; empty for the first.
	Previous string `json:"previous"`
	// Notes and Categories are the merged notes of every release after
	// Previous up to and including this one, newest release first within
	// each group, in CategoryOrder.
	Notes      []string   `json:"notes"`
	Categories []Category `json:"categories"`
}

// Release is the stable as a release, for the copy that announces one.
func (s *Stable) Release() *Release {
	if s == nil {
		return nil
	}
	return &Release{Version: s.Version, Date: s.Since, Notes: s.Notes, Categories: s.Categories, StableSince: s.Since}
}

// CurrentStable is the newest designated release; nil until one exists.
func (n *Notes) CurrentStable() *Stable {
	if n == nil {
		return nil
	}
	for i := range n.Releases {
		if n.Releases[i].StableSince != "" {
			return n.stableAt(i)
		}
	}
	return nil
}

// Stable looks a designated release up by version.
func (n *Notes) Stable(version string) *Stable {
	if n == nil || version == "" {
		return nil
	}
	for i := range n.Releases {
		if n.Releases[i].Version == version && n.Releases[i].StableSince != "" {
			return n.stableAt(i)
		}
	}
	return nil
}

// stableAt builds the stable for Releases[i], merging the notes of every
// release down to the previous stable one. Releases from before OpenV had
// version numbers predate the stable channel and are never merged in.
func (n *Notes) stableAt(i int) *Stable {
	r := n.Releases[i]
	s := &Stable{Version: r.Version, Since: r.StableSince, Notes: []string{}, Categories: []Category{}}
	var merged Release
	for j := i; j < len(n.Releases); j++ {
		rel := n.Releases[j]
		if j > i && rel.StableSince != "" {
			s.Previous = rel.Version
			break
		}
		if !semverPattern.MatchString(rel.Version) {
			break
		}
		for _, c := range rel.Categories {
			for _, note := range c.Notes {
				merged.Add(c.Name, note)
			}
		}
	}
	s.Categories = orderCategories(merged.Categories)
	for _, c := range s.Categories {
		s.Notes = append(s.Notes, c.Notes...)
	}
	return s
}

// orderCategories puts groups into CategoryOrder, anything else last.
func orderCategories(in []Category) []Category {
	out := make([]Category, 0, len(in))
	for _, name := range CategoryOrder {
		for _, c := range in {
			if c.Name == name {
				out = append(out, c)
			}
		}
	}
	for _, c := range in {
		if !IsCategory(c.Name) {
			out = append(out, c)
		}
	}
	return out
}

// Version ordering. Semantic versions order numerically and every one of
// them is newer than every release from before OpenV had version numbers,
// which order by their date and the .2/.3 suffix; that is the order they
// shipped in.

// versionKey parses a version for comparison; ok is false for a string that
// is neither shape, which no gate ever lets through.
func versionKey(v string) (key [4]int, ok bool) {
	if semverPattern.MatchString(v) {
		parts := strings.Split(v, ".")
		key[0] = 1
		for i, p := range parts {
			key[i+1], _ = strconv.Atoi(p)
		}
		return key, true
	}
	if legacyVersionPattern.MatchString(v) {
		date, suffix, _ := strings.Cut(v, ".")
		day, _ := strconv.Atoi(strings.ReplaceAll(date, "-", ""))
		nth := 1
		if suffix != "" {
			nth, _ = strconv.Atoi(suffix)
		}
		key[1], key[2] = day, nth
		return key, true
	}
	return key, false
}

// Compare orders two versions: negative when a is older than b, zero when
// equal, positive when newer. A version of neither shape orders below
// every real one.
func Compare(a, b string) int {
	ka, oka := versionKey(a)
	kb, okb := versionKey(b)
	switch {
	case !oka && !okb:
		return 0
	case !oka:
		return -1
	case !okb:
		return 1
	}
	for i := range ka {
		if ka[i] != kb[i] {
			if ka[i] < kb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// AtOrBefore reports whether release a shipped no later than release b. An
// unparseable version on either side is false: a gate never opens by
// accident.
func AtOrBefore(a, b string) bool {
	if _, ok := versionKey(a); !ok {
		return false
	}
	if _, ok := versionKey(b); !ok {
		return false
	}
	return Compare(a, b) <= 0
}

// Newer reports whether a is a release newer than b. An empty b (a
// workspace with no stable release yet, an instance with nothing to compare
// with) is older than any real version.
func Newer(a, b string) bool {
	if _, ok := versionKey(a); !ok {
		return false
	}
	if b == "" {
		return true
	}
	return Compare(a, b) > 0
}
