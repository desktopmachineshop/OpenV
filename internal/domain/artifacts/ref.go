package artifacts

import (
	"strconv"
	"strings"
)

// Stable short refs (e.g. "REQ-12", "TC-3") give every artifact a compact,
// human- and agent-readable address that is unique within its project and
// stable across versions. They exist primarily for the AI context surface:
// UUIDs cost ~10 tokens each and mean nothing to a model, while refs are
// 2-4 tokens and carry the artifact type in the prefix. Refs are assigned
// server-side on create (per-project, per-prefix counters — see
// ArtifactRepository.Save) and never change or get reused afterwards.
//
// This is unrelated to CreateArtifactRequest.Ref, the proposal-mode
// temporary token (issue #235) that only names a not-yet-created artifact
// within a single agent run.

// refPrefixes maps catalog types to their ref prefixes.
var refPrefixes = map[string]string{
	TypeHeading:     "HDG",
	TypeDescription: "DSC",
	TypePersona:     "PER",
	TypeUserNeed:    "NEED",
	TypeRequirement: "REQ",
	TypeDesignItem:  "DES",
	TypeTestCase:    "TC",
	TypeHazard:      "HAZ",
	TypeOther:       "ART",
}

// RefPrefix returns the ref prefix for an artifact type. Catalog types use
// the fixed table above; a legacy/unknown type derives a prefix from its
// name (initials of hyphen-separated words, else the first three letters,
// uppercased), falling back to "ART" when nothing usable remains. The
// derivation is deterministic so the same legacy type always maps to the
// same prefix.
func RefPrefix(artifactType string) string {
	if p, ok := refPrefixes[artifactType]; ok {
		return p
	}
	clean := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	parts := strings.Split(artifactType, "-")
	if len(parts) > 1 {
		var initials strings.Builder
		for _, p := range parts {
			if c := clean(p); c != "" {
				initials.WriteString(strings.ToUpper(c[:1]))
			}
		}
		if initials.Len() > 0 {
			return initials.String()
		}
	}
	if c := clean(artifactType); c != "" {
		if len(c) > 3 {
			c = c[:3]
		}
		return strings.ToUpper(c)
	}
	return "ART"
}

// ParseRef splits a stable ref of the form PREFIX-NUM. ok is false for
// anything else (including proposal-mode temporary tokens like "t1", whose
// lowercase prefix or missing number fails the shape check).
func ParseRef(ref string) (prefix string, num int, ok bool) {
	i := strings.LastIndex(ref, "-")
	if i <= 0 || i == len(ref)-1 {
		return "", 0, false
	}
	prefix = ref[:i]
	for _, r := range prefix {
		if !(r >= 'A' && r <= 'Z') {
			return "", 0, false
		}
	}
	num, err := strconv.Atoi(ref[i+1:])
	if err != nil || num <= 0 {
		return "", 0, false
	}
	return prefix, num, true
}

// FormatRef renders a prefix and number as a stable ref string.
func FormatRef(prefix string, num int) string {
	return prefix + "-" + strconv.Itoa(num)
}
