package events

import (
	"log"
	"sync"

	domain "github.com/openv/requirements-platform/internal/domain/events"
)

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
			log.Printf("events: failed to persist event %s: %v", e.EventType, err)
		}
	}
	select {
	case b.queue <- e:
	default:
		log.Printf("events: dispatch queue full, dropping event %s", e.EventType)
	}
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
						log.Printf("events: subscriber panic on %s: %v", e.EventType, r)
					}
				}()
				fn(e)
			}()
		}
	}
}
