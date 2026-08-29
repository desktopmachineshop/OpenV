package artifacts

import (
	"strings"
	"unicode"
)

// SearchHit is one row of a cross-project artifact search. ProjectName is
// resolved by the API layer (the repository only knows project ids).
type SearchHit struct {
	ArtifactID  string `json:"artifact_id"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Snippet     string `json:"snippet"`
	// Score is the semantic-similarity score (0..1, higher is closer) for hits
	// from the semantic/hybrid modes. It is omitted for pure keyword hits,
	// which have no vector distance.
	Score float64 `json:"score,omitempty"`
}

// snippetRadius is how many runes of context Snippet keeps on each side of
// the first match.
const snippetRadius = 80

// Snippet returns a short excerpt of body around the first case-insensitive
// occurrence of query, with ellipses where text was trimmed. When the query
// does not occur in the body (e.g. a title-only match), the head of the body
// is returned instead. Newlines are collapsed so the excerpt renders on one
// line.
func Snippet(body, query string) string {
	body = strings.Join(strings.Fields(body), " ")
	if body == "" {
		return ""
	}

	bodyRunes := []rune(body)
	idx := indexFold(bodyRunes, []rune(query))

	start, end := 0, len(bodyRunes)
	if idx >= 0 {
		start = idx - snippetRadius
		end = idx + len([]rune(query)) + snippetRadius
	} else {
		end = 2 * snippetRadius
	}
	if start < 0 {
		start = 0
	}
	if end > len(bodyRunes) {
		end = len(bodyRunes)
	}

	out := string(bodyRunes[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(bodyRunes) {
		out = out + "…"
	}
	return out
}

// indexFold returns the rune index of the first case-insensitive occurrence
// of needle in haystack, or -1. Folding is per-rune so indices always map
// back onto the original haystack.
func indexFold(haystack, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return -1
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if unicode.ToLower(haystack[i+j]) != unicode.ToLower(needle[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// escapeLike escapes LIKE/ILIKE wildcards in a user query so it matches
// literally (backslash is postgres' default escape character).
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// LikePattern builds the '%query%' ILIKE pattern for a raw user query, with
// wildcard characters escaped so they match literally.
func LikePattern(query string) string {
	return "%" + escapeLike(query) + "%"
}
