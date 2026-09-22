package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// DefaultMaxSeats is the quantity above which a seat push is refused.
const DefaultMaxSeats = 500

// ErrTooManySeats is the refusal to bill a quantity above the ceiling.
var ErrTooManySeats = errors.New("seat count is above the billing ceiling; not pushed")

// Seat sync keeps a Business subscription's quantity equal to the workspace's
// seats — members plus pending invitations, the number the seat check reads.
//
// The member count leads and the provider follows, never the other way
// round: a membership change commits locally and then enqueues the workspace
// here, so the handler never sees a provider error and never waits on one.
// The queue coalesces: one goroutine holds a set of dirty workspaces and
// drains it every couple of seconds, so a bulk invite is one push and a
// burst of retries is not a burst of prorations. A push lost to a full
// queue, a provider outage or a crash is repaired on the next reconcile
// tick, which compares every billed Business workspace's count with the
// quantity the provider holds. Lite is never synced: it bills one person
// whatever the member count.
//
// Above the ceiling the push is refused and reported rather than sent: we
// would rather under-bill for an hour than let a runaway invitation loop
// charge a five-figure invoice nobody asked for.

// SeatsChanged records that a workspace's seat count may have changed.
// Non-blocking; safe on a nil or disabled service.
func (s *Service) SeatsChanged(orgID string) {
	if !s.Enabled() || s.seatQueue == nil || orgID == "" {
		return
	}
	select {
	case s.seatQueue <- orgID:
	default:
		s.log.Warn("seat queue full; the next reconcile repairs the quantity", "org_id", orgID)
	}
}

// SetMaxSeats sets the quantity ceiling; 0 keeps the default.
func (s *Service) SetMaxSeats(n int) {
	if n > 0 {
		s.maxSeats = n
	}
}

// startSeatSync runs the drain loop until ctx ends.
func (s *Service) startSeatSync(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.seatDelay)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.FlushSeats(ctx)
			}
		}
	}()
}

// FlushSeats drains whatever is queued into a set and pushes each workspace
// once, and returns how many it attempted. The drain loop calls it on its
// timer; a test, or a shutdown that wants the last change pushed, calls it
// directly.
func (s *Service) FlushSeats(ctx context.Context) int {
	dirty := map[string]struct{}{}
	for {
		select {
		case id := <-s.seatQueue:
			dirty[id] = struct{}{}
			continue
		default:
		}
		break
	}
	for id := range dirty {
		org, err := s.orgs.Get(id)
		if err != nil {
			if !errors.Is(err, orgs.ErrNotFound) {
				s.log.Warn("seat sync could not read workspace", "org_id", id, "error", err)
			}
			continue
		}
		if err := s.pushSeats(ctx, org); err != nil {
			s.log.Warn("seat quantity not pushed; the next reconcile repairs it", "org_id", id, "error", err)
		}
	}
	return len(dirty)
}

// seatsDrifted reports whether a workspace's billed quantity differs from
// its seat count: only for a live Business subscription with an item to
// update, and never when the count cannot be read.
func (s *Service) seatsDrifted(org *orgs.Org) (want int, drifted bool) {
	if org == nil || org.BilledPlan != orgs.PlanBusiness || !org.Billing.Live() || org.Billing.ItemRef == "" || s.seats == nil {
		return 0, false
	}
	n, err := s.seats(org.ID)
	if err != nil {
		s.log.Warn("could not count seats", "org_id", org.ID, "error", err)
		return 0, false
	}
	if n < 1 {
		n = 1
	}
	return n, n != org.Billing.Seats
}

// pushSeats sets the provider quantity to the workspace's seat count when
// they differ, then re-reads the subscription so the mirrored snapshot
// shows the new quantity without waiting for a tick.
func (s *Service) pushSeats(ctx context.Context, org *orgs.Org) error {
	want, drifted := s.seatsDrifted(org)
	if !drifted {
		return nil
	}
	if want > s.maxSeats {
		s.metrics.SeatPushRefused()
		s.log.Error("seat count above the billing ceiling; quantity not pushed",
			"org_id", org.ID, "seats", want, "ceiling", s.maxSeats, "billed", org.Billing.Seats)
		return fmt.Errorf("%w: %d seats, ceiling %d", ErrTooManySeats, want, s.maxSeats)
	}
	if err := s.provider.SetItemQuantity(ctx, org.Billing.ItemRef, want); err != nil {
		return err
	}
	s.log.Info("seat quantity pushed", "org_id", org.ID, "from", org.Billing.Seats, "to", want)
	readAt := s.now()
	sub, err := s.provider.GetSubscription(ctx, org.Billing.SubscriptionRef)
	if err != nil {
		// The push landed; the snapshot catches up on the next tick.
		return nil
	}
	s.apply(ctx, sub, nil, readAt)
	return nil
}

// repairSeatDrift is the reconcile tick's pass over billed workspaces: every
// Business subscription whose quantity differs from the count is pushed,
// and the number that differed is reported.
func (s *Service) repairSeatDrift(ctx context.Context, billed []*orgs.Org) {
	drift := 0
	for _, org := range billed {
		fresh, err := s.orgs.Get(org.ID)
		if err != nil {
			continue
		}
		if _, drifted := s.seatsDrifted(fresh); !drifted {
			continue
		}
		drift++
		if err := s.pushSeats(ctx, fresh); err != nil {
			s.log.Warn("seat drift not repaired this tick", "org_id", org.ID, "error", err)
		}
	}
	s.metrics.SeatDrift(drift)
}
