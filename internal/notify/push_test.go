package notify

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/pushsubs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// fakePushSender records every send and answers a scripted status/error per
// endpoint (the zero entry means 201 Created, the happy path).
type fakePushSender struct {
	mu      sync.Mutex
	sent    map[string][][]byte
	status  map[string]int
	err     map[string]error
	maxSeen int
	live    int
}

func newFakePushSender() *fakePushSender {
	return &fakePushSender{sent: map[string][][]byte{}, status: map[string]int{}, err: map[string]error{}}
}

func (f *fakePushSender) Send(sub *pushsubs.Subscription, payload []byte) (int, error) {
	f.mu.Lock()
	f.live++
	if f.live > f.maxSeen {
		f.maxSeen = f.live
	}
	f.sent[sub.Endpoint] = append(f.sent[sub.Endpoint], payload)
	status, err := f.status[sub.Endpoint], f.err[sub.Endpoint]
	f.mu.Unlock()

	// Hold the slot briefly so overlapping sends are observable.
	time.Sleep(time.Millisecond)

	f.mu.Lock()
	f.live--
	f.mu.Unlock()
	if err != nil {
		return 0, err
	}
	if status == 0 {
		status = http.StatusCreated
	}
	return status, nil
}

func (f *fakePushSender) count(endpoint string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent[endpoint])
}

func (f *fakePushSender) payload(t *testing.T, endpoint string) PushPayload {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	raw := f.sent[endpoint]
	if len(raw) == 0 {
		t.Fatalf("no payload sent to %s", endpoint)
	}
	var p PushPayload
	if err := json.Unmarshal(raw[0], &p); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	return p
}

// fakeSubStore is an in-memory PushSubscriptionStore that records the
// bookkeeping calls the dispatcher makes.
type fakeSubStore struct {
	mu     sync.Mutex
	subs   map[string][]*pushsubs.Subscription
	used   []string
	failed []string
	gone   []string
	err    error
}

func (f *fakeSubStore) ListForUser(userID string) ([]*pushsubs.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subs[userID], f.err
}

func (f *fakeSubStore) Forget(endpoint string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gone = append(f.gone, endpoint)
	return 1, nil
}

func (f *fakeSubStore) MarkUsed(id string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.used = append(f.used, id)
	return nil
}

func (f *fakeSubStore) MarkFailed(id string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = append(f.failed, id)
	return nil
}

func (f *fakeSubStore) snapshot() (used, failed, gone []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.used...), append([]string(nil), f.failed...), append([]string(nil), f.gone...)
}

// fakeDirectory answers GetByID from a map.
type fakeDirectory struct {
	users map[string]*users.User
	err   error
}

func (f *fakeDirectory) GetByID(id string) (*users.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.users[id], nil
}

func pushNotification(userID, ntype string) *notifications.Notification {
	return notifications.New("org-1", userID, ntype, "Agent run failed",
		"An agent run you launched finished with an error.",
		map[string]interface{}{"kind": "run", "run_id": "run-9", "project_id": "proj-1"})
}

// pushOptedIn is a member who has turned web push on (the email helper in
// email_test.go flips the other flag).
func pushOptedIn(id string) *users.User {
	return &users.User{ID: id, Email: id + "@example.com", PushNotifications: true}
}

// TestVAPIDConfigEnabled: push is on only with both keys and a mailto:/https:
// subject — a missing half or a bare address leaves it off.
func TestVAPIDConfigEnabled(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  VAPIDConfig
		want bool
	}{
		{"complete mailto", VAPIDConfig{"pub", "priv", "mailto:ops@example.com"}, true},
		{"complete https", VAPIDConfig{"pub", "priv", "https://example.com/ops"}, true},
		{"zero value", VAPIDConfig{}, false},
		{"no private key", VAPIDConfig{PublicKey: "pub", Subject: "mailto:ops@example.com"}, false},
		{"no public key", VAPIDConfig{PrivateKey: "priv", Subject: "mailto:ops@example.com"}, false},
		{"bare address", VAPIDConfig{"pub", "priv", "ops@example.com"}, false},
		{"http subject", VAPIDConfig{"pub", "priv", "http://example.com"}, false},
	} {
		if got := tc.cfg.Enabled(); got != tc.want {
			t.Errorf("%s: Enabled() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestPushTypesFromEnv: the default allow-list matches email's, and the
// override is parsed the same way.
func TestPushTypesFromEnv(t *testing.T) {
	if got, want := len(DefaultPushTypes()), len(DefaultEmailTypes()); got != want {
		t.Fatalf("default push types = %d, want the %d email types", got, want)
	}
	t.Setenv(envPushTypes, " run_failed , ,mention ")
	got := PushTypesFromEnv()
	if len(got) != 2 || got[0] != notifications.TypeRunFailed || got[1] != notifications.TypeMention {
		t.Fatalf("PushTypesFromEnv() = %v, want [run_failed mention]", got)
	}
	t.Setenv(envPushTypes, " , ")
	if got := PushTypesFromEnv(); len(got) != len(DefaultPushTypes()) {
		t.Fatalf("a separators-only override must fall back to the default, got %v", got)
	}
}

// TestPushDispatcherGates: nothing is sent without a sender, for a type off
// the allow-list, or for a recipient who has not opted in.
func TestPushDispatcherGates(t *testing.T) {
	sub := pushsubs.New("u-1", "https://push.example.com/a", "p", "a", "Pixel")
	store := &fakeSubStore{subs: map[string][]*pushsubs.Subscription{"u-1": {sub}}}
	dir := &fakeDirectory{users: map[string]*users.User{"u-1": pushOptedIn("u-1")}}

	// No sender (no VAPID keys configured): a no-op, and Enabled says so.
	off := NewPushDispatcher(nil, store, dir, DefaultPushTypes())
	if off.Enabled() {
		t.Fatal("a dispatcher with no sender must not report Enabled")
	}
	off.Dispatch(pushNotification("u-1", notifications.TypeRunFailed))
	off.Wait()
	if used, failed, gone := store.snapshot(); len(used)+len(failed)+len(gone) != 0 {
		t.Fatal("a disabled dispatcher touched the store")
	}

	// A nil dispatcher is safe to call (callers hold one unconditionally).
	var nilDispatcher *PushDispatcher
	nilDispatcher.Dispatch(pushNotification("u-1", notifications.TypeRunFailed))
	nilDispatcher.Wait()

	sender := newFakePushSender()
	d := NewPushDispatcher(sender, store, dir, DefaultPushTypes())

	// A type off the allow-list never reaches the sender.
	d.Dispatch(pushNotification("u-1", notifications.TypeMention))
	d.Wait()
	if sender.count(sub.Endpoint) != 0 {
		t.Fatal("an ineligible type was pushed")
	}
	if d.Eligible(notifications.TypeMention) {
		t.Fatal("mention must not be push-eligible by default")
	}

	// An opted-OUT recipient gets nothing, even for an eligible type.
	dir.users["u-1"] = &users.User{ID: "u-1", PushNotifications: false}
	d.Dispatch(pushNotification("u-1", notifications.TypeRunFailed))
	d.Wait()
	if sender.count(sub.Endpoint) != 0 {
		t.Fatal("an opted-out recipient was pushed")
	}

	// An unknown recipient is not an error, just nothing to do.
	d.Dispatch(pushNotification("nobody", notifications.TypeRunFailed))
	d.Wait()
	if sender.count(sub.Endpoint) != 0 {
		t.Fatal("an unknown recipient was pushed")
	}
}

// TestPushDispatcherFanOut: an eligible notification for an opted-in member
// reaches every one of their devices, with a payload the worker can render.
func TestPushDispatcherFanOut(t *testing.T) {
	phone := pushsubs.New("u-1", "https://push.example.com/phone", "p", "a", "Pixel")
	tablet := pushsubs.New("u-1", "https://push.example.com/tablet", "p", "a", "iPad")
	other := pushsubs.New("u-2", "https://push.example.com/other", "p", "a", "Someone else")
	store := &fakeSubStore{subs: map[string][]*pushsubs.Subscription{
		"u-1": {phone, tablet},
		"u-2": {other},
	}}
	dir := &fakeDirectory{users: map[string]*users.User{"u-1": pushOptedIn("u-1"), "u-2": pushOptedIn("u-2")}}
	sender := newFakePushSender()
	d := NewPushDispatcher(sender, store, dir, DefaultPushTypes())

	n := pushNotification("u-1", notifications.TypeRunFailed)
	d.Dispatch(n)
	d.Wait()

	if sender.count(phone.Endpoint) != 1 || sender.count(tablet.Endpoint) != 1 {
		t.Fatalf("fan-out reached phone=%d tablet=%d, want 1 each",
			sender.count(phone.Endpoint), sender.count(tablet.Endpoint))
	}
	if sender.count(other.Endpoint) != 0 {
		t.Fatal("another member's device was pushed")
	}

	got := sender.payload(t, phone.Endpoint)
	if got.Title != n.Title || got.Body != n.Body {
		t.Fatalf("payload = %+v, want the notification's title and body", got)
	}
	// The deep link is a same-origin PATH: the service worker navigates an
	// open window to it, and navigate() refuses a cross-origin URL.
	if want := "/projects/proj-1/agent-runs?run=run-9"; got.URL != want {
		t.Fatalf("url = %q, want %q", got.URL, want)
	}
	// Coalescing is scoped per type and project.
	if want := notifications.TypeRunFailed + ":proj-1"; got.Tag != want {
		t.Fatalf("tag = %q, want %q", got.Tag, want)
	}

	used, failed, gone := store.snapshot()
	if len(used) != 2 || len(failed) != 0 || len(gone) != 0 {
		t.Fatalf("bookkeeping: used=%v failed=%v gone=%v, want two used", used, failed, gone)
	}
}

// TestPushDispatcherOutcomes: 410/404 delete the subscription, other failures
// mark it, and a success records the use.
func TestPushDispatcherOutcomes(t *testing.T) {
	okSub := pushsubs.New("u-1", "https://push.example.com/ok", "p", "a", "ok")
	goneSub := pushsubs.New("u-1", "https://push.example.com/gone", "p", "a", "gone")
	missingSub := pushsubs.New("u-1", "https://push.example.com/missing", "p", "a", "missing")
	rateSub := pushsubs.New("u-1", "https://push.example.com/rate", "p", "a", "rate-limited")
	brokenSub := pushsubs.New("u-1", "https://push.example.com/broken", "p", "a", "transport error")

	store := &fakeSubStore{subs: map[string][]*pushsubs.Subscription{
		"u-1": {okSub, goneSub, missingSub, rateSub, brokenSub},
	}}
	dir := &fakeDirectory{users: map[string]*users.User{"u-1": pushOptedIn("u-1")}}
	sender := newFakePushSender()
	sender.status[goneSub.Endpoint] = http.StatusGone
	sender.status[missingSub.Endpoint] = http.StatusNotFound
	sender.status[rateSub.Endpoint] = http.StatusTooManyRequests
	sender.err[brokenSub.Endpoint] = errors.New("dial tcp: connection refused")

	d := NewPushDispatcher(sender, store, dir, DefaultPushTypes())
	d.Dispatch(pushNotification("u-1", notifications.TypeProposalPending))
	d.Wait()

	used, failed, gone := store.snapshot()
	if len(used) != 1 || used[0] != okSub.ID {
		t.Fatalf("used = %v, want only the 201 subscription", used)
	}
	if len(gone) != 2 || !contains(gone, goneSub.Endpoint) || !contains(gone, missingSub.Endpoint) {
		t.Fatalf("gone = %v, want the 410 and the 404 endpoints deleted", gone)
	}
	if len(failed) != 2 || !contains(failed, rateSub.ID) || !contains(failed, brokenSub.ID) {
		t.Fatalf("failed = %v, want the 429 and the transport error marked (and kept)", failed)
	}
}

// TestPushDispatcherBoundedConcurrency: however many notifications are
// dispatched, and however many devices each recipient has, the worker pool
// never has more than pushWorkers sends in flight — and it is still doing
// them concurrently.
func TestPushDispatcherBoundedConcurrency(t *testing.T) {
	var subs []*pushsubs.Subscription
	for i := 0; i < 4; i++ {
		subs = append(subs, pushsubs.New("u-1", fmt.Sprintf("https://push.example.com/%d", i), "p", "a", "device"))
	}
	store := &fakeSubStore{subs: map[string][]*pushsubs.Subscription{"u-1": subs}}
	dir := &fakeDirectory{users: map[string]*users.User{"u-1": pushOptedIn("u-1")}}
	sender := newFakePushSender()
	d := NewPushDispatcher(sender, store, dir, DefaultPushTypes())

	for i := 0; i < pushWorkers*4; i++ {
		d.Dispatch(pushNotification("u-1", notifications.TypeReviewRequested))
	}
	d.Wait()

	sender.mu.Lock()
	peak := sender.maxSeen
	sender.mu.Unlock()
	if peak > pushWorkers {
		t.Fatalf("%d sends were in flight at once, want at most %d", peak, pushWorkers)
	}
	if peak < 2 {
		t.Fatalf("sends were fully serialized (peak %d); the pool is not concurrent", peak)
	}
	if d.Dropped() != 0 {
		t.Fatalf("%d notifications were dropped with a queue %d deep", d.Dropped(), pushQueueDepth)
	}
}

// blockingPushSender holds every send until released — a push service that
// accepts the connection and then says nothing.
type blockingPushSender struct {
	release chan struct{}
	mu      sync.Mutex
	sent    int
}

func (b *blockingPushSender) Send(*pushsubs.Subscription, []byte) (int, error) {
	<-b.release
	b.mu.Lock()
	b.sent++
	b.mu.Unlock()
	return http.StatusCreated, nil
}

func (b *blockingPushSender) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sent
}

// TestPushDispatcherDoesNotBlockOnAStalledPushService: with every worker
// stuck in a send that outlives any timeout, Dispatch still returns at once.
// Notifications queue up to pushQueueDepth and are then DROPPED — counted,
// not blocked on — so the notification path (and the request that triggered
// it) is never pinned by a push service that has stopped answering.
func TestPushDispatcherDoesNotBlockOnAStalledPushService(t *testing.T) {
	sub := pushsubs.New("u-1", "https://push.example.com/stuck", "p", "a", "device")
	store := &fakeSubStore{subs: map[string][]*pushsubs.Subscription{"u-1": {sub}}}
	dir := &fakeDirectory{users: map[string]*users.User{"u-1": pushOptedIn("u-1")}}
	sender := &blockingPushSender{release: make(chan struct{})}
	d := NewPushDispatcher(sender, store, dir, DefaultPushTypes())

	total := pushQueueDepth + pushWorkers + 64
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < total; i++ {
			d.Dispatch(pushNotification("u-1", notifications.TypeRunFailed))
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Dispatch blocked on a stalled push service")
	}

	dropped := d.Dropped()
	if dropped == 0 {
		t.Fatalf("%d notifications were dispatched into a queue %d deep, yet none was dropped",
			total, pushQueueDepth)
	}
	if dropped > int64(total) {
		t.Fatalf("dropped = %d, more than the %d dispatched", dropped, total)
	}

	// Released, the queued work drains: nothing is lost but the drops.
	close(sender.release)
	d.Wait()
	if got, want := sender.count(), total-int(dropped); got != want {
		t.Fatalf("sent %d messages, want the %d that were queued", got, want)
	}
}

// TestWebPushSenderAlwaysHasARequestTimeout: webpush-go falls back to a bare
// http.Client — no timeout at all — when handed a nil client, which is how a
// single unresponsive push service would hold a worker forever.
func TestWebPushSenderAlwaysHasARequestTimeout(t *testing.T) {
	cfg := VAPIDConfig{PublicKey: "pub", PrivateKey: "priv", Subject: "mailto:ops@example.com"}

	client, ok := NewWebPushSender(cfg, nil).client.(*http.Client)
	if !ok {
		t.Fatal("a nil client was passed straight through to webpush-go")
	}
	if client.Timeout != pushRequestTimeout {
		t.Fatalf("default client timeout = %v, want %v", client.Timeout, pushRequestTimeout)
	}
	if DefaultPushHTTPClient().Timeout <= 0 {
		t.Fatal("DefaultPushHTTPClient has no timeout")
	}

	// An explicit client is honoured as given.
	custom := &http.Client{Timeout: time.Second}
	if got, ok := NewWebPushSender(cfg, custom).client.(*http.Client); !ok || got != custom {
		t.Fatalf("client = %v, want the one passed in", got)
	}
}

// TestRenderPushURLIsASameOriginPath: the payload url is a PATH, never an
// absolute link. The service worker follows a tap with
// WindowClient.navigate, which rejects a cross-origin URL — and an absolute
// link built from FRONTEND_URL is cross-origin the moment the deployment is
// reached by any other name.
func TestRenderPushURLIsASameOriginPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		ref  map[string]interface{}
		want string
	}{
		{"run", map[string]interface{}{"kind": "run", "run_id": "run-9", "project_id": "proj-1"}, "/projects/proj-1/agent-runs?run=run-9"},
		{"artifact", map[string]interface{}{"kind": "artifact", "project_id": "proj-1"}, "/projects/proj-1/requirements"},
		{"budget alert", map[string]interface{}{"kind": "org_usage", "org_id": "org-1"}, "/org/settings?tab=usage"},
		{"no ref", nil, "/projects"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := renderPush(notifications.New("org-1", "u-1", notifications.TypeRunFailed, "Title", "Body", tc.ref)).URL
			if got != tc.want {
				t.Fatalf("url = %q, want %q", got, tc.want)
			}
			if !strings.HasPrefix(got, "/") || strings.Contains(got, "://") {
				t.Fatalf("url = %q, want a same-origin path", got)
			}
		})
	}
}

// TestPushDispatcherStoreErrors: a failing directory or subscription store is
// logged and swallowed, never propagated into the notification path.
func TestPushDispatcherStoreErrors(t *testing.T) {
	sender := newFakePushSender()

	dirErr := &fakeDirectory{err: errors.New("db down")}
	d := NewPushDispatcher(sender, &fakeSubStore{}, dirErr, DefaultPushTypes())
	d.Dispatch(pushNotification("u-1", notifications.TypeRunFailed))
	d.Wait()

	storeErr := &fakeSubStore{err: errors.New("db down")}
	dir := &fakeDirectory{users: map[string]*users.User{"u-1": pushOptedIn("u-1")}}
	d = NewPushDispatcher(sender, storeErr, dir, DefaultPushTypes())
	d.Dispatch(pushNotification("u-1", notifications.TypeRunFailed))
	d.Wait()

	if len(sender.sent) != 0 {
		t.Fatalf("sent %d messages despite lookup failures", len(sender.sent))
	}
}

// TestPushTagFallsBackToWorkspace: a budget alert is not project-scoped, so
// its tag falls back to the workspace, then to the bare type.
func TestPushTagFallsBackToWorkspace(t *testing.T) {
	budget := notifications.New("org-1", "u-1", notifications.TypeBudgetThreshold,
		"Workspace nearing budget", "80% spent",
		map[string]interface{}{"kind": "org_usage", "org_id": "org-1"})
	if want := notifications.TypeBudgetThreshold + ":org-1"; pushTag(budget) != want {
		t.Fatalf("tag = %q, want %q", pushTag(budget), want)
	}
	bare := notifications.New("", "u-1", notifications.TypeRunFailed, "t", "b", nil)
	if pushTag(bare) != notifications.TypeRunFailed {
		t.Fatalf("tag = %q, want the bare type", pushTag(bare))
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestNotifierDispatchesPush wires a Notifier with a push dispatcher and
// drives one real event through it: the notification row is stored, the SSE
// frame is broadcast, and the same recipient's devices are pushed — the
// fan-out point this feature hangs off.
func TestNotifierDispatchesPush(t *testing.T) {
	store := &fakeStore{}
	memberSvc := &fakeMembers{list: []*members.Member{
		member("u-owner", members.RoleOwner, "Owner", "owner@example.com"),
		member("u-viewer", members.RoleViewer, "Viewer", "viewer@example.com"),
	}}
	broadcaster := &fakeBroadcaster{}

	device := pushsubs.New("u-owner", "https://push.example.com/owner", "p", "a", "Pixel")
	viewerDevice := pushsubs.New("u-viewer", "https://push.example.com/viewer", "p", "a", "Pixel")
	subs := &fakeSubStore{subs: map[string][]*pushsubs.Subscription{
		"u-owner":  {device},
		"u-viewer": {viewerDevice},
	}}
	dir := &fakeDirectory{users: map[string]*users.User{
		"u-owner":  pushOptedIn("u-owner"),
		"u-viewer": pushOptedIn("u-viewer"),
	}}
	sender := newFakePushSender()
	push := NewPushDispatcher(sender, subs, dir, DefaultPushTypes())

	n := NewNotifier(store, memberSvc, broadcaster).SetPushDispatcher(push)
	n.Handle(domainevents.Event{
		EventType: domainevents.ProposalCreated,
		ProjectID: "proj-1",
		EntityID:  "prop-1",
		OrgID:     "org-1",
		Actor:     "agent:a-1",
		Payload:   map[string]interface{}{"run_id": "run-9"},
	})
	push.Wait()

	if len(store.created) != 1 || store.created[0].UserID != "u-owner" {
		t.Fatalf("stored %d notifications, want one for the owner", len(store.created))
	}
	if sender.count(device.Endpoint) != 1 {
		t.Fatalf("the owner's device was pushed %d times, want 1", sender.count(device.Endpoint))
	}
	// A viewer is not a reviewer: no notification row, so no push either.
	if sender.count(viewerDevice.Endpoint) != 0 {
		t.Fatal("a viewer's device was pushed")
	}
	got := sender.payload(t, device.Endpoint)
	if got.Title != "Agent proposal pending review" {
		t.Fatalf("push title = %q", got.Title)
	}
	if want := "/projects/proj-1/agent-runs?run=run-9"; got.URL != want {
		t.Fatalf("push url = %q, want %q", got.URL, want)
	}
}
