package events

import (
	"sync"
	"testing"
	"time"

	domain "github.com/openv/requirements-platform/internal/domain/events"
)

// TestPublishCountsDropsWhenQueueFull verifies that events published while
// the dispatch queue is full are counted as dropped instead of vanishing
// silently, and that queued events are not.
func TestPublishCountsDropsWhenQueueFull(t *testing.T) {
	b := NewBus(nil, nil)

	block := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once
	b.Subscribe(func(domain.Event) {
		once.Do(func() { close(started) })
		<-block
	})
	defer close(block)

	// Park the dispatch loop inside the subscriber so nothing drains.
	b.Publish(domain.Event{EventType: "test.first"})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("subscriber never received the first event")
	}

	// Fill the queue to capacity: no drops yet.
	for i := 0; i < cap(b.queue); i++ {
		b.Publish(domain.Event{EventType: "test.fill"})
	}
	if got := b.Dropped(); got != 0 {
		t.Fatalf("expected 0 drops while filling the queue, got %d", got)
	}

	// Everything past capacity is dropped and counted.
	for i := 0; i < 5; i++ {
		b.Publish(domain.Event{EventType: "test.overflow"})
	}
	if got := b.Dropped(); got != 5 {
		t.Fatalf("expected 5 dropped events, got %d", got)
	}
}
