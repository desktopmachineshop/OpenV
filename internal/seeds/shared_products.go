package seeds

import (
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/sharedproducts"
)

// EnsureSharedProductPool publishes the starter pool (see product_concepts.go)
// into the shared demo products, and returns how many rows it added.
//
// DEPLOYMENT-WIDE, not per workspace: the pool is one list the whole
// deployment reads, so unlike EnsureOrgDefaults this runs once at boot rather
// than for every org.
//
// Idempotent by the pool's own rule. shared_products dedupes on the normalized
// name, so a second boot finds every entry already there and adds nothing; the
// repository reports that as ErrDuplicate, which is the expected outcome here
// rather than a failure. That also means a member who has already published
// something under one of these names keeps their row — the seed never
// overwrites a person's entry, it only fills a gap.
//
// Best-effort by design: a pool that cannot be seeded must not stop the server
// from starting, because nothing else depends on it. Failures are logged and
// the boot continues.
func EnsureSharedProductPool(repo sharedproducts.Repository) (int, error) {
	if repo == nil {
		return 0, nil
	}
	added := 0
	var firstErr error
	for _, b := range builtinProducts() {
		// Through the same door as a published product: Sanitize is what the
		// publish endpoint applies, so a starter entry cannot carry anything
		// a member would be refused for — over-long prose, a link, markup.
		clean, err := sharedproducts.Sanitize(sharedproducts.Product{
			Category:    b.Category,
			Name:        b.Name,
			Description: b.Description,
			Vision:      b.Vision,
			Problem:     b.Problem,
			TargetUsers: b.TargetUsers,
		})
		if err != nil {
			slog.Error("seed shared product: rejected by validation", "name", b.Name, "error", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		clean.ID = uuid.New().String()
		clean.CreatedAt = time.Now().UTC()
		// No CreatedByOrg/CreatedByUser: nobody published these, and the
		// columns are nullable precisely so moderation can tell the
		// difference between an entry with an author and one without.

		switch err := repo.Create(&clean); {
		case err == nil:
			added++
		case errors.Is(err, sharedproducts.ErrDuplicate):
			// Already in the pool, from an earlier boot or from a member who
			// got there first. Either way there is nothing to do.
		default:
			slog.Error("seed shared product: insert failed", "name", clean.Name, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if added > 0 {
		slog.Info("shared product pool seeded", "added", added, "of", len(builtinProducts()))
	}
	return added, firstErr
}
