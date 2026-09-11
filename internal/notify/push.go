package notify

// Web push delivery for high-signal notifications (REQ-109). This mirrors the
// email side channel in email.go: when a notification row is created (and
// pushed over SSE), an eligible type destined for an opted-in recipient is
// also encrypted and posted to every device that member has subscribed.
//
// Like email, push is OPT-IN INFRASTRUCTURE: with no VAPID key pair
// configured the dispatcher is disabled, /api/v1/me/push/config reports
// enabled=false, nothing is sent and the app behaves exactly as before.
//
// Unlike email, sends do not happen on the caller's goroutine. A member can
// have many devices and a push service can be slow, so Dispatch hands the
// notification to a FIXED pool of workers over a bounded queue: the process
// never runs more than pushWorkers sends at once however fast notifications
// arrive, and a queue that is full drops (and counts) rather than blocking
// the request path — the alternative, one unbounded goroutine per
// notification, turns a stalled push service into unbounded memory.
// Failures are logged and swallowed; push never fails a run or a notification.

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/pushsubs"
)

// Environment variables read by VAPIDFromEnv and PushTypesFromEnv. Named
// constants read at the call site, following ratelimit.go / verification.go.
const (
	envVAPIDPublicKey  = "OPENV_VAPID_PUBLIC_KEY"
	envVAPIDPrivateKey = "OPENV_VAPID_PRIVATE_KEY"
	envVAPIDSubject    = "OPENV_VAPID_SUBJECT"
	envPushTypes       = "OPENV_PUSH_NOTIFICATION_TYPES"
)

const (
	// pushTTLSeconds is how long a push service may hold an undelivered
	// message for a device that is offline. A day: past that the alert has
	// been overtaken by the app itself.
	pushTTLSeconds = 24 * 60 * 60

	// pushWorkers is the size of the fixed goroutine pool that drains the
	// dispatch queue. It bounds simultaneous sends across the whole process,
	// so a burst of notifications — or a member with many devices — cannot
	// open an unbounded number of outbound connections.
	pushWorkers = 8

	// pushQueueDepth is how many notifications may wait for a worker. Deep
	// enough to ride out a slow push service, bounded so that one that has
	// stopped answering cannot grow the queue without limit: past this,
	// Dispatch drops the notification (counted, and logged) rather than
	// blocking the caller. In-app and SSE delivery are unaffected — push is
	// the side channel.
	pushQueueDepth = 1024

	// pushRequestTimeout bounds ONE request to a push service. Without it the
	// send inherits http.Client's zero value — no timeout at all — and a push
	// service that accepts the connection and then says nothing holds a
	// worker for as long as it likes.
	pushRequestTimeout = 10 * time.Second

	// pushBodyLimit truncates the notification body for the phone banner;
	// anything longer is unreadable there and only inflates the payload.
	pushBodyLimit = 200
)

// VAPIDConfig is the server's Voluntary Application Server Identification key
// pair plus the contact subject sent to push services (RFC 8292). Generate a
// pair with `make vapid-keys` (cmd/openv-vapid).
type VAPIDConfig struct {
	PublicKey  string
	PrivateKey string
	// Subject identifies the operator to the push service so it can get in
	// touch about a misbehaving deployment. It must be a mailto: or https:
	// URI; anything else disables the feature rather than sending junk.
	Subject string
}

// Enabled reports whether the deployment can send web push at all.
func (c VAPIDConfig) Enabled() bool {
	return c.PublicKey != "" && c.PrivateKey != "" && validVAPIDSubject(c.Subject)
}

func validVAPIDSubject(s string) bool {
	return strings.HasPrefix(s, "mailto:") || strings.HasPrefix(s, "https://")
}

// VAPIDFromEnv reads the VAPID configuration. Every variable unset is the
// default and leaves push off; a half-configured or malformed set logs why
// and also leaves it off, so a typo never silently degrades to "no pushes
// and no explanation".
func VAPIDFromEnv() VAPIDConfig {
	c := VAPIDConfig{
		PublicKey:  strings.TrimSpace(os.Getenv(envVAPIDPublicKey)),
		PrivateKey: strings.TrimSpace(os.Getenv(envVAPIDPrivateKey)),
		Subject:    strings.TrimSpace(os.Getenv(envVAPIDSubject)),
	}
	switch {
	case c.PublicKey == "" && c.PrivateKey == "" && c.Subject == "":
		slog.Info("push: " + envVAPIDPublicKey + " unset; web push disabled (in-app + SSE delivery unaffected)")
	case c.PublicKey == "" || c.PrivateKey == "":
		slog.Warn("push: VAPID key pair incomplete; web push disabled",
			"have_public", c.PublicKey != "", "have_private", c.PrivateKey != "")
	case !validVAPIDSubject(c.Subject):
		slog.Warn("push: " + envVAPIDSubject + " must be a mailto: or https: URI; web push disabled")
	default:
		slog.Info("push: web push enabled", "subject", c.Subject)
	}
	return c
}

// DefaultPushTypes are the higher-signal notification types that reach a
// phone by default — deliberately the same set as DefaultEmailTypes. Chatter
// @mentions and interview-completed stay in-app: a buzzing pocket is a
// stronger interruption than an inbox, not a weaker one.
func DefaultPushTypes() []string { return DefaultEmailTypes() }

// PushTypesFromEnv reads the comma-separated OPENV_PUSH_NOTIFICATION_TYPES
// override, or returns DefaultPushTypes when it is unset/empty.
func PushTypesFromEnv() []string {
	return typeListFromEnv(envPushTypes, DefaultPushTypes)
}

// PushSender delivers one encrypted payload to one device. Status is the push
// service's HTTP status (0 when the request never got that far); err covers
// transport and encryption failures only, so a 410 is a status, not an error.
type PushSender interface {
	Send(sub *pushsubs.Subscription, payload []byte) (status int, err error)
}

// WebPushSender is the real sender: RFC 8291 payload encryption and an RFC
// 8292 VAPID Authorization header, both from webpush-go.
type WebPushSender struct {
	vapid  VAPIDConfig
	client webpush.HTTPClient
}

// DefaultPushHTTPClient is the client the sender uses to reach push services:
// http.DefaultClient's behaviour plus a timeout, which is the one thing the
// default lacks and the one thing that matters here.
func DefaultPushHTTPClient() *http.Client {
	return &http.Client{Timeout: pushRequestTimeout}
}

// NewWebPushSender builds a sender for the given keys. A nil client is NOT
// passed through to webpush-go — its fallback is a bare http.Client with no
// timeout — it is replaced by DefaultPushHTTPClient.
func NewWebPushSender(vapid VAPIDConfig, client webpush.HTTPClient) *WebPushSender {
	if client == nil {
		client = DefaultPushHTTPClient()
	}
	return &WebPushSender{vapid: vapid, client: client}
}

// Send encrypts and posts one message.
func (s *WebPushSender) Send(sub *pushsubs.Subscription, payload []byte) (int, error) {
	resp, err := webpush.SendNotification(payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{
		HTTPClient:      s.client,
		Subscriber:      s.vapid.Subject,
		VAPIDPublicKey:  s.vapid.PublicKey,
		VAPIDPrivateKey: s.vapid.PrivateKey,
		TTL:             pushTTLSeconds,
	})
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close() //nolint:errcheck // response body is drained below
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// PushSubscriptionStore is the slice of pushsubs.Service the dispatcher needs.
type PushSubscriptionStore interface {
	ListForUser(userID string) ([]*pushsubs.Subscription, error)
	Forget(endpoint string) (int64, error)
	MarkUsed(id string, at time.Time) error
	MarkFailed(id string, at time.Time) error
}

// PushDispatcher turns a freshly-created notification into best-effort web
// pushes. It gates on: a sender being wired (VAPID configured), the type
// being push-eligible, and the recipient being opted in.
type PushDispatcher struct {
	sender   PushSender
	subs     PushSubscriptionStore
	users    UserDirectory
	eligible map[string]bool
	// queue holds notifications waiting for a worker; workers starts the
	// pool on the first dispatch, so a deployment with push off (and every
	// test that never dispatches) runs no extra goroutines.
	queue   chan *notifications.Notification
	workers sync.Once
	// dropped counts notifications refused because the queue was full. Read
	// by Dropped for logs and tests; it is the signal that push is behind.
	dropped atomic.Int64
	// inflight tracks queued work so tests (and only tests) can wait for
	// delivery to settle.
	inflight sync.WaitGroup
	// now is injectable so tests can pin the timestamps written to the
	// subscription rows; defaults to time.Now.
	now func() time.Time
}

// NewPushDispatcher wires a dispatcher. sender nil (the usual case when no
// VAPID keys are configured) leaves push off and every Dispatch a no-op.
// eligibleTypes is the allow-list (see DefaultPushTypes / PushTypesFromEnv).
// Deep links are same-origin PATHS (see PushPayload.URL), so no base URL is
// needed here — the browser resolves them against the app's own origin.
func NewPushDispatcher(sender PushSender, subs PushSubscriptionStore, dir UserDirectory, eligibleTypes []string) *PushDispatcher {
	elig := make(map[string]bool, len(eligibleTypes))
	for _, t := range eligibleTypes {
		if t = strings.TrimSpace(t); t != "" {
			elig[t] = true
		}
	}
	return &PushDispatcher{
		sender:   sender,
		subs:     subs,
		users:    dir,
		eligible: elig,
		queue:    make(chan *notifications.Notification, pushQueueDepth),
		now:      time.Now,
	}
}

// Enabled reports whether the dispatcher can send anything.
func (d *PushDispatcher) Enabled() bool { return d != nil && d.sender != nil }

// Eligible reports whether a type is on the push allow-list.
func (d *PushDispatcher) Eligible(ntype string) bool {
	return d != nil && d.eligible[ntype]
}

// Dispatch queues best-effort pushes for one notification and returns at
// once — it never blocks on a push service, and never blocks on the queue
// either: a full queue is a drop, counted and logged. It is a no-op when the
// dispatcher is nil, no sender is wired, the type is not eligible, or the
// recipient has not opted in. Nil-safe, so callers can hold a nil
// *PushDispatcher and call it unconditionally.
func (d *PushDispatcher) Dispatch(n *notifications.Notification) {
	if !d.Enabled() || n == nil || !d.eligible[n.Type] {
		return
	}
	d.workers.Do(d.startWorkers)
	d.inflight.Add(1)
	select {
	case d.queue <- n:
	default:
		d.inflight.Done()
		slog.Warn("push: dispatch queue full; notification not pushed",
			"user_id", n.UserID, "type", n.Type, "dropped_total", d.dropped.Add(1))
	}
}

// startWorkers launches the fixed pool. Once only, on the first dispatch.
func (d *PushDispatcher) startWorkers() {
	for i := 0; i < pushWorkers; i++ {
		go func() {
			for n := range d.queue {
				d.deliver(n)
				d.inflight.Done()
			}
		}()
	}
}

// Dropped is the number of notifications refused because the dispatch queue
// was full since boot. Non-zero means push is running behind — the push
// service is slow or down — and is worth an operator's attention; in-app and
// SSE delivery are unaffected.
func (d *PushDispatcher) Dropped() int64 {
	if d == nil {
		return 0
	}
	return d.dropped.Load()
}

// Wait blocks until every queued dispatch has settled. For tests and an
// orderly shutdown only — the request path never calls it.
func (d *PushDispatcher) Wait() {
	if d != nil {
		d.inflight.Wait()
	}
}

// deliver loads the recipient, checks the opt-in, and sends the payload to
// each of their devices in turn. The per-device sends are sequential on
// purpose: concurrency across the process comes from the worker pool, which
// bounds it, and a member's handful of devices is not worth fanning out
// further (nor worth letting one stalled device hold several workers).
func (d *PushDispatcher) deliver(n *notifications.Notification) {
	u, err := d.users.GetByID(n.UserID)
	if err != nil {
		slog.Error("push: failed to load recipient", "user_id", n.UserID, "error", err)
		return
	}
	if u == nil || !u.PushNotifications {
		return
	}
	subs, err := d.subs.ListForUser(n.UserID)
	if err != nil {
		slog.Error("push: failed to list subscriptions", "user_id", n.UserID, "error", err)
		return
	}
	if len(subs) == 0 {
		return
	}
	payload, err := json.Marshal(renderPush(n))
	if err != nil {
		slog.Error("push: failed to encode payload", "user_id", n.UserID, "error", err)
		return
	}
	for _, sub := range subs {
		d.sendOne(sub, payload, n.Type)
	}
}

// sendOne delivers to one device and records the outcome:
//   - 2xx: the device is alive — stamp last_used_at, clearing any failure;
//   - 404/410: the push service says this subscription is gone for good —
//     delete it, so a reinstalled browser does not accumulate dead rows;
//   - anything else (including a transport error): stamp failed_at and KEEP
//     the row; push services rate-limit and have outages, and a later
//     success clears the mark.
func (d *PushDispatcher) sendOne(s *pushsubs.Subscription, payload []byte, ntype string) {
	status, err := d.sender.Send(s, payload)
	now := d.now()
	switch {
	case err != nil:
		slog.Warn("push: send failed", "user_id", s.UserID, "type", ntype, "error", err)
		d.markFailed(s, now)
	case status == http.StatusNotFound || status == http.StatusGone:
		if _, err := d.subs.Forget(s.Endpoint); err != nil {
			slog.Error("push: failed to delete gone subscription", "subscription_id", s.ID, "error", err)
			return
		}
		slog.Info("push: subscription gone; deleted", "user_id", s.UserID, "status", status)
	case status >= 200 && status < 300:
		if err := d.subs.MarkUsed(s.ID, now); err != nil {
			slog.Error("push: failed to record send", "subscription_id", s.ID, "error", err)
		}
	default:
		slog.Warn("push: push service refused the message", "user_id", s.UserID, "type", ntype, "status", status)
		d.markFailed(s, now)
	}
}

func (d *PushDispatcher) markFailed(s *pushsubs.Subscription, at time.Time) {
	if err := d.subs.MarkFailed(s.ID, at); err != nil {
		slog.Error("push: failed to record failure", "subscription_id", s.ID, "error", err)
	}
}

// PushPayload is the JSON the service worker's push handler receives. Kept
// flat and small: push services cap the encrypted payload (4 KB here), and
// the worker only needs enough to draw a banner and know where a tap goes.
type PushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// URL is a SAME-ORIGIN PATH ("/projects/…") to the notification's
	// subject — the same destination the in-app bell and the email link to,
	// minus the origin. It has to be a path: the service worker follows a
	// tap with WindowClient.navigate, which rejects a cross-origin URL, and
	// an absolute FRONTEND_URL link is cross-origin whenever the deployment
	// is reached by any other name. Emails, which have no such window, keep
	// their absolute links.
	URL string `json:"url"`
	// Tag coalesces banners: a new notification with a tag already on screen
	// replaces it instead of stacking. Scoped per type and project so a
	// second failed run in another project still gets its own banner.
	Tag string `json:"tag"`
}

// renderPush builds the worker payload for a notification.
func renderPush(n *notifications.Notification) PushPayload {
	return PushPayload{
		Title: n.Title,
		Body:  truncate(n.Body, pushBodyLimit),
		URL:   notificationPath(n.EntityRef),
		Tag:   pushTag(n),
	}
}

// pushTag scopes coalescing to one type within one project (or workspace, for
// the budget alerts that are not project-scoped).
func pushTag(n *notifications.Notification) string {
	if scope := refString(n.EntityRef, "project_id"); scope != "" {
		return n.Type + ":" + scope
	}
	if scope := refString(n.EntityRef, "org_id"); scope != "" {
		return n.Type + ":" + scope
	}
	return n.Type
}
