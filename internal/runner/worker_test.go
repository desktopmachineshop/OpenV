package runner

import (
	"sync"
	"testing"
	"time"
)

// TestStartHeartbeatBeatsUntilStopped: the loop beats repeatedly at the given
// interval and never again once stop has returned.
func TestStartHeartbeatBeatsUntilStopped(t *testing.T) {
	var mu sync.Mutex
	beats := 0
	stop := startHeartbeat(2*time.Millisecond, func() {
		mu.Lock()
		beats++
		mu.Unlock()
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := beats
		mu.Unlock()
		if n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("heartbeat fired %d times, want >= 3", n)
		}
		time.Sleep(time.Millisecond)
	}

	stop()
	mu.Lock()
	atStop := beats
	mu.Unlock()

	// The interval is 2ms; if the goroutine survived stop it would beat
	// many times over this window.
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	final := beats
	mu.Unlock()
	if final != atStop {
		t.Errorf("beats after stop returned: %d -> %d", atStop, final)
	}
}

// TestStartHeartbeatStopWaitsForInflightBeat: stop must not return while a
// beat is still executing — the caller relies on that to hand heartbeating
// over to the log pump without two concurrent pushers.
func TestStartHeartbeatStopWaitsForInflightBeat(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var mu sync.Mutex
	inFlight := false
	stop := startHeartbeat(time.Millisecond, func() {
		mu.Lock()
		inFlight = true
		mu.Unlock()
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		mu.Lock()
		inFlight = false
		mu.Unlock()
	})

	<-entered // a beat is now blocked mid-flight
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(release)
	}()
	stop() // must block until the beat above finishes

	mu.Lock()
	still := inFlight
	mu.Unlock()
	if still {
		t.Error("stop returned while a beat was still in flight")
	}

	stop() // idempotent: second call must not panic or block
}
