package events

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	domain "github.com/openv/requirements-platform/internal/domain/events"
)

// dropLogInterval rate-limits the queue-full warning so a sustained stall
// doesn't flood the log — the counter still records every drop.
const dropLogInterval = 10 * time.Second

// DefaultBus persists events synchronously and dispatches to subscribers
// asynchronously so a slow subscriber can never block a request handler.
type DefaultBus struct {
	repo domain.Repository
	// orgResolver maps a project id to its owning org, used to backfill
	// OrgID for publishers that don't know their tenant. May be nil.
	orgResolver func(projectID string) string

	mu          sync.RWMutex
	subscribers []func(domain.Event)
	queue       chan domain.Event

	// dropped counts events discarded because the dispatch queue was full.
	// A dropped event was still persisted, but subscribers (SSE, automation
	// triggers, orchestration hooks) never saw it — e.g. a dropped
	// WorkItemMoved means automations silently never fire for that move.
	dropped     atomic.Uint64
	lastDropLog atomic.Int64 // unix nanos of the last drop warning
}

// NewBus creates and starts a bus. repo may be nil (dispatch-only mode);
// orgResolver may be nil (no org backfill).
func NewBus(repo domain.Repository, orgResolver func(projectID string) string) *DefaultBus {
	b := &DefaultBus{
		repo:        repo,
		orgResolver: orgResolver,
		queue:       make(chan domain.Event, 256),
	}
	go b.dispatchLoop()
	return b
}

// Publish stores the event and queues it for dispatch.
func (b *DefaultBus) Publish(e domain.Event) {
	if e.OrgID == "" && e.ProjectID != "" && b.orgResolver != nil {
		e.OrgID = b.orgResolver(e.ProjectID)
	}
	if b.repo != nil {
		if err := b.repo.Save(e); err != nil {
			slog.Error("events: failed to persist event",
				"event_type", e.EventType, "org_id", e.OrgID, "error", err)
		}
	}
	select {
	case b.queue <- e:
	default:
		total := b.dropped.Add(1)
		now := time.Now().UnixNano()
		last := b.lastDropLog.Load()
		if now-last >= int64(dropLogInterval) && b.lastDropLog.CompareAndSwap(last, now) {
			slog.Warn("events: dispatch queue full, dropping event",
				"event_type", e.EventType,
				"org_id", e.OrgID,
				"dropped_total", total,
				"queue_capacity", cap(b.queue))
		}
	}
}

// Dropped reports how many events have been discarded because the dispatch
// queue was full.
func (b *DefaultBus) Dropped() uint64 {
	return b.dropped.Load()
}

// Subscribe registers a handler called for every published event.
func (b *DefaultBus) Subscribe(fn func(domain.Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscribers = append(b.subscribers, fn)
}

func (b *DefaultBus) dispatchLoop() {
	for e := range b.queue {
		b.mu.RLock()
		subs := make([]func(domain.Event), len(b.subscribers))
		copy(subs, b.subscribers)
		b.mu.RUnlock()
		for _, fn := range subs {
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("events: subscriber panic",
							"event_type", e.EventType, "panic", r)
					}
				}()
				fn(e)
			}()
		}
	}
}
