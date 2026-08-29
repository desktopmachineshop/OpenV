package postgres

import "github.com/openv/requirements-platform/internal/domain/embeddings"

// The vector width in migration 0016 (embeddingDimensions) MUST equal the
// canonical embeddings.Dimensions. This compile-time assertion fails the build
// if the two ever drift apart. It is a zero-cost check in a _check file rather
// than an import inside migrations.go so the migration registry keeps its
// stdlib-only imports.
const _ = uint(embeddingDimensions - embeddings.Dimensions)
const _ = uint(embeddings.Dimensions - embeddingDimensions)
