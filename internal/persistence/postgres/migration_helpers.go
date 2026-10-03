// Declarations the numbered migrations reach (refactor plan M10; the
// declmove spec came from internal/tools/liftmigrations). The stored-data
// freeze hashes each with every migration that reaches it
// (testdata/freeze/migrations.txt), so none of them ever changes: a new
// migration that needs something else declares something new.

package postgres

import "strings"

// backfillRefPrefix is the type→prefix mapping frozen at the time migration
// 0018 shipped. It intentionally duplicates artifacts.RefPrefix (same
// stdlib-only-imports rationale as embeddingDimensions below, plus one more:
// a migration is a historical document — if the live mapping ever changes,
// this backfill must keep producing what it produced on the day it ran).
func backfillRefPrefix(artifactType string) string {
	switch artifactType {
	case "heading":
		return "HDG"
	case "description":
		return "DSC"
	case "persona":
		return "PER"
	case "user-need":
		return "NEED"
	case "requirement":
		return "REQ"
	case "design-item":
		return "DES"
	case "test-case":
		return "TC"
	case "hazard":
		return "HAZ"
	case "other":
		return "ART"
	}
	clean := func(s string) string {
		out := make([]rune, 0, len(s))
		for _, r := range s {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				out = append(out, r)
			}
		}
		return string(out)
	}
	parts := strings.Split(artifactType, "-")
	if len(parts) > 1 {
		initials := ""
		for _, p := range parts {
			if c := clean(p); c != "" {
				initials += strings.ToUpper(c[:1])
			}
		}
		if initials != "" {
			return initials
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

// embeddingDimensions is the vector width baked into the artifact_embeddings
// schema. It mirrors embeddings.Dimensions; it is duplicated here as an
// untyped constant rather than imported so the migration registry keeps its
// stdlib-only import set (and to avoid a persistence->domain import purely for
// a literal). The two MUST stay in sync — the compile-time assertion in
// embedding_dim_check.go enforces that.
const embeddingDimensions = 1536
