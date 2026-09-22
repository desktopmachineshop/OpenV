package billing

import (
	"context"
	"time"
)

// Start reconciles once now and then every interval until ctx ends, in the
// shape of the other polling services. Constructed only when a provider is
// configured; with none there is nothing to poll and nothing is started.
func (s *Service) Start(ctx context.Context, interval time.Duration) {
	if !s.Enabled() {
		return
	}
	if interval > 0 {
		s.interval = interval
	}
	s.startSeatSync(ctx)
	go func() {
		s.Reconcile(ctx)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.Reconcile(ctx)
			}
		}
	}()
}
