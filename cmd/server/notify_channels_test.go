//go:build unix

package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	openv "github.com/openv/requirements-platform"
	"github.com/openv/requirements-platform/internal/api"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/pushsubs"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/runnersessions"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/notify"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// TestEachNotificationProducerSendsByEmailAndPush pins what refactor step X6
// (#547) left to the wiring alone (#379, the X6 follow-up): stage notify
// builds one notify.Channels, the email and the web push dispatcher, and
// hands it to every notification producer through SetChannels, there and in
// stage release. testdata/boot_steps.txt records that each SetChannels call
// is made, not the value it is given, and the producers' own tests (S10's
// TestNotificationContent among them) build their channels themselves, so a
// producer handed notify.Channels{}, or a Channels holding one dispatcher,
// would leave every other test green while its notifications stopped going
// out by email or by push.
//
// The test runs the real stages storage, workspace, notify and release in
// process, as main() calls them, over a database of its own
// (OPENV_TEST_DATABASE_URL; skipped when unset: the notifications store and
// the push subscriptions are Postgres repositories). Mail goes to the tour's
// mail catcher (OPENV_SMTP_HOST), and pushes to a push service of the
// test's own, which every device's endpoint names (OPENV_VAPID_* set).
// Stages projects, agents and realtime are not run; what stage notify reads
// from them has a stand-in: the SSE hub, a run service whose month's spend is
// fixed, and stage workspace's runner session service (RUNNER_POOL_KEY set)
// with its month's leased minutes fixed.
//
// Each of the six producers is driven once, towards an account opted in to
// both channels with one device: the bus notifier and the budget monitor by
// a failed run's agentrun.finished on the bus, the hosted-minutes monitor by
// Check, and the release announcer, the stable-release scheduler and the
// dedicated instance's support-window warner by stage release itself, at
// boot. For each, the test finds the row of one type the producer stored for
// that account, and requires a mail to the account whose subject is the
// row's title and a push to its device whose payload, decrypted as the
// browser does, carries the same title. The first boot keeps the default
// type lists, which hold the notifier's run_failed and the monitors'
// budget_threshold and hosted_minutes; no release type is on either default
// list, so the second boot, the one that runs stage release, sets
// OPENV_EMAIL_NOTIFICATION_TYPES and OPENV_PUSH_NOTIFICATION_TYPES to them,
// as S10's second run does. A failure names the producer and the channel
// its notification never reached.
func TestEachNotificationProducerSendsByEmailAndPush(t *testing.T) {
	db := freshDatabase(t)
	conn, err := postgres.Connect(db.url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	built, err := release.NewService(openv.ReleaseNotesMarkdown)
	if err != nil || built.Current() == nil || built.CurrentStable() == nil {
		t.Fatalf("RELEASE_NOTES.md as built names no current release or no stable one (%v): stage release would "+
			"start no announcer, or a stable scheduler and support-window warner with nothing to send", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mailbox := startTourMailCatcher(t, &tourMailSpec{})
	pushes := startPushCatcher(t)
	feed := serveUnhanded(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// The shared service's public release feed (notify.ReleaseFeed): a
		// stable release newer than any this binary runs, designated so
		// long ago that the support window of the instance's own has closed.
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"version":"999.0.0","stable":"999.0.0","stable_since":"2000-01-01"}`)
	}))
	vapidPublic, vapidPrivate := notificationsPushKeyPair("x6 channels VAPID key")
	for name, value := range map[string]string{
		"OPENV_SMTP_HOST":                  "127.0.0.1",
		"OPENV_SMTP_PORT":                  strconv.Itoa(mailbox.port()),
		"OPENV_SMTP_FROM":                  "openv@example.test",
		"OPENV_SMTP_USER":                  "",
		"OPENV_SMTP_PASSWORD":              "",
		"OPENV_VAPID_PUBLIC_KEY":           vapidPublic,
		"OPENV_VAPID_PRIVATE_KEY":          vapidPrivate,
		"OPENV_VAPID_SUBJECT":              "mailto:openv@example.test",
		"OPENV_EMAIL_NOTIFICATION_TYPES":   "",
		"OPENV_PUSH_NOTIFICATION_TYPES":    "",
		"OPENV_BILLING_GRANDFATHER_BEFORE": "",
		"RUNNER_POOL_KEY":                  "x6-channels-runner-pool",
		"OPENV_DEPLOYMENT":                 "dedicated",
		"OPENV_RELEASE_FEED_URL":           feed + "/api/v1/public/release",
	} {
		t.Setenv(name, value)
	}

	const budgetUSD, allowanceMinutes = 20.0, 60
	agentsDir, uploadsDir := t.TempDir(), t.TempDir()
	boot := func() *app {
		t.Helper()
		a := &app{ctx: ctx, db: conn, agentsDir: agentsDir, uploadsDir: uploadsDir}
		a.storage()
		a.workspace()
		if a.runnerSessionService == nil {
			t.Fatal("stage workspace built no runner session service with RUNNER_POOL_KEY set, so stage notify " +
				"would build no hosted-minutes monitor")
		}
		// The stand-ins: the month's spend has reached the budget, and the
		// month's leased minutes the allowance.
		a.runService = agentruns.NewDefaultService(spentRuns{spend: budgetUSD}, nil, nil)
		a.runnerSessionService = leasedMinutes{Service: a.runnerSessionService, used: allowanceMinutes}
		a.sseHub = api.NewSSEHub()
		return a
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	a := boot()
	h := &channelsHarness{t: t, db: conn, mail: mailbox, push: pushes}
	account := func(name string) *pinAccount {
		t.Helper()
		seed := "x6 channels device " + name
		acc := &pinAccount{name: name, id: uuid.NewString(), email: name + "@example.test",
			device: notificationsPushScalar(seed)}
		auth, err := base64.URLEncoding.DecodeString(notificationsPushDeviceAuth(seed))
		must(err)
		acc.auth = auth
		now := time.Now().UTC()
		must(a.userRepo.SaveUser(&users.User{ID: acc.id, Email: acc.email, Name: name,
			AuthProvider: users.ProviderPassword, CreatedAt: now, UpdatedAt: now}))
		must(a.userService.SetEmailNotifications(acc.id, true))
		must(a.userService.SetPushNotifications(acc.id, true))
		endpoint := pushes.device(acc)
		must(a.pushSubRepo.Upsert(&pushsubs.Subscription{ID: uuid.NewString(), UserID: acc.id, Endpoint: endpoint,
			P256dh: notificationsPushDeviceKey(seed), Auth: notificationsPushDeviceAuth(seed), CreatedAt: now}))
		return acc
	}
	workspace := func(name string, admin *pinAccount) string {
		t.Helper()
		org, err := a.orgService.CreateOrg(name, orgs.TypeCompany, admin.id)
		must(err)
		return org.ID
	}

	// lena launched the run that fails; bart is the admin of a workspace
	// with a monthly budget, mina of one with a cloud runner allowance, nora
	// of a nightly-channel workspace and stan of a stable-channel one, still
	// on no stable release.
	lena, bart, mina, nora, stan := account("lena"), account("bart"), account("mina"), account("nora"), account("stan")
	budgetOrg, minutesOrg := workspace("Budget Works", bart), workspace("Minutes Works", mina)
	workspace("Nightly Works", nora)
	stableOrg := workspace("Stable Works", stan)
	budget := budgetUSD
	_, err = a.orgService.SetMonthlyBudget(budgetOrg, &budget)
	must(err)
	limits, err := json.Marshal(map[string]int{orgs.LimitHostedRunnerMinutesMonth: allowanceMinutes})
	must(err)
	_, err = conn.Exec(`UPDATE organizations SET limits = $2 WHERE id = $1`, minutesOrg, string(limits))
	must(err)
	// A workspace moving onto a plan whose admins choose the channel stays
	// on nightly until its admin chooses stable.
	_, err = a.orgService.SetPlan(stableOrg, orgs.PlanTeam)
	must(err)
	_, err = a.orgService.SetReleaseChannel(stableOrg, orgs.ChannelStable)
	must(err)

	// The first boot, with the default type lists. It stops after stage
	// notify: stage release, which announces the release once per
	// database, runs in the second.
	a.notify()
	if a.minutesMonitor == nil {
		t.Fatal("stage notify built no hosted-minutes monitor")
	}
	a.bus.Publish(domainevents.New(domainevents.RunFinished, "", uuid.NewString(), "",
		map[string]interface{}{"status": "failed", "launched_by": lena.id}).WithOrg(budgetOrg))
	a.minutesMonitor.Check(minutesOrg)
	h.await([]channelPin{
		{"the bus notifier (notify.NewNotifier)", lena, notifications.TypeRunFailed},
		{"the budget monitor (notify.NewBudgetMonitor)", bart, notifications.TypeBudgetThreshold},
		{"the hosted-minutes monitor (notify.NewMinutesMonitor)", mina, notifications.TypeHostedMinutes},
	})

	// The second boot, on the same database, with the release types on
	// both lists, through stage release.
	releaseTypes := strings.Join([]string{notifications.TypeReleasePublished, notifications.TypeReleaseScheduled,
		notifications.TypeReleaseSupportWindow}, ",")
	t.Setenv("OPENV_EMAIL_NOTIFICATION_TYPES", releaseTypes)
	t.Setenv("OPENV_PUSH_NOTIFICATION_TYPES", releaseTypes)
	a = boot()
	a.notify()
	a.release()
	// Only the announcer tells nora of a release (no workspace of hers is
	// on the stable channel, where the stable scheduler's turn-on would tell
	// her too), only the stable scheduler tells stan one is scheduled, and
	// only the support-window warner warns anyone of the support window.
	h.await([]channelPin{
		{"the release announcer (notify.NewReleaseAnnouncer)", nora, notifications.TypeReleasePublished},
		{"the stable-release scheduler (notify.NewStableScheduler)", stan, notifications.TypeReleaseScheduled},
		{"the support-window warner (notify.NewSupportWindowWatcher)", nora, notifications.TypeReleaseSupportWindow},
	})
}

// pinAccount is an account opted in to email and push, with one device: the
// device's private key and auth secret open the pushes sent to it.
type pinAccount struct {
	name, id, email string
	device          *ecdh.PrivateKey
	auth            []byte
}

// channelPin is one producer's notification: the producer as stage notify
// or stage release builds it, the recipient, and the type it stores.
type channelPin struct {
	producer string
	to       *pinAccount
	ntype    string
}

// leasedMinutes is a runner session service whose month's leased minutes
// are fixed.
type leasedMinutes struct {
	runnersessions.Service
	used int
}

func (m leasedMinutes) MinutesUsed(string, time.Time) (int, error) { return m.used, nil }

// channelsAwaitWithin bounds the wait for a boot's mails and pushes; only a
// failing run waits it out.
const channelsAwaitWithin = 10 * time.Second

// channelsHarness reads each pinned producer's stored rows and what the mail
// catcher and the push service received for their recipient.
type channelsHarness struct {
	t    *testing.T
	db   *sql.DB
	mail *tourMailCatcher
	push *pushCatcher
}

// await waits until every pin's notification has gone out by email and by
// push, and fails the test for each one that has not when the wait ends.
func (h *channelsHarness) await(pins []channelPin) {
	h.t.Helper()
	deadline := time.Now().Add(channelsAwaitWithin)
	for {
		missing := h.missing(pins)
		if len(missing) == 0 {
			return
		}
		if time.Now().After(deadline) {
			for _, m := range missing {
				h.t.Error(m)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// missing is, per pin, the channel its notification has not reached yet: a
// stored row of its type for its recipient whose title no mail to the
// recipient has as its subject, or no push to the recipient's device
// carries.
func (h *channelsHarness) missing(pins []channelPin) []string {
	h.t.Helper()
	const wiring = "; stage notify hands every producer a.notifyChannels through SetChannels (wire_notify.go)"
	var out []string
	for _, p := range pins {
		titles := h.titles(p.to, p.ntype)
		if len(titles) == 0 {
			out = append(out, fmt.Sprintf("%s stored no %s notification for %s, so the test did not drive it "+
				"and checked neither channel", p.producer, p.ntype, p.to.name))
			continue
		}
		if subjects := h.subjects(p.to); !anyOf(subjects, titles) {
			out = append(out, fmt.Sprintf("%s: its %s notification to %s (%q) never reached the email dispatcher: "+
				"no mail to %s has that subject (mails to it: %q)%s", p.producer, p.ntype, p.to.name, titles[0],
				p.to.email, subjects, wiring))
		}
		if pushed := h.push.titles(p.to); !anyOf(pushed, titles) {
			out = append(out, fmt.Sprintf("%s: its %s notification to %s (%q) never reached the push dispatcher: "+
				"no push to %s's device carries that title (pushes to it: %q)%s", p.producer, p.ntype, p.to.name,
				titles[0], p.to.name, pushed, wiring))
		}
	}
	return out
}

// anyOf reports whether got holds any of want.
func anyOf(got, want []string) bool {
	return slices.ContainsFunc(got, func(s string) bool { return slices.Contains(want, s) })
}

// titles are the titles of the rows of a type stored for an account.
func (h *channelsHarness) titles(to *pinAccount, ntype string) []string {
	h.t.Helper()
	rows, err := h.db.Query(`SELECT title FROM notifications WHERE user_id = $1 AND type = $2`, to.id, ntype)
	if err != nil {
		h.t.Fatalf("read %s's %s notifications: %v", to.name, ntype, err)
	}
	defer rows.Close()
	var titles []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			h.t.Fatal(err)
		}
		titles = append(titles, title)
	}
	if err := rows.Err(); err != nil {
		h.t.Fatal(err)
	}
	return titles
}

// subjects are the decoded subjects of the mails the catcher took for an
// account, in the order they came.
func (h *channelsHarness) subjects(to *pinAccount) []string {
	var out []string
	for _, m := range h.mail.messages() {
		if m.data == nil || !slices.Contains(m.to, to.email) {
			continue
		}
		msg, err := mail.ReadMessage(bytes.NewReader(m.data))
		if err != nil {
			out = append(out, "<a mail that does not parse: "+err.Error()+">")
			continue
		}
		subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
		if err != nil {
			out = append(out, "<a subject that does not decode: "+err.Error()+">")
			continue
		}
		out = append(out, subject)
	}
	return out
}

// pushCatcher is a push service: each device's endpoint is a path of it.
// It opens every push it is sent with the device's keys and keeps the title
// of the payload (notify.PushPayload), and answers 201 Created, so the
// dispatcher marks the device used.
type pushCatcher struct {
	url     string
	mu      sync.Mutex
	devices map[string]*pinAccount // endpoint path → owner
	got     map[string][]string    // owner → the title of each push, in order
}

func startPushCatcher(t *testing.T) *pushCatcher {
	t.Helper()
	c := &pushCatcher{devices: map[string]*pinAccount{}, got: map[string][]string{}}
	c.url = serveUnhanded(t, http.HandlerFunc(c.serve))
	return c
}

// device registers an account's device and returns its endpoint.
func (c *pushCatcher) device(acc *pinAccount) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	path := "/push/" + acc.name
	c.devices[path] = acc
	return c.url + path
}

func (c *pushCatcher) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	acc := c.devices[r.URL.Path]
	if acc == nil || err != nil || r.Method != http.MethodPost {
		// Not 404 or 410: those make the dispatcher delete the device.
		http.Error(w, "not a device of this push service", http.StatusBadRequest)
		return
	}
	title, err := pushTitle(body, acc)
	if err != nil {
		title = fmt.Sprintf("<a push that does not open: %v>", err)
	}
	c.got[acc.name] = append(c.got[acc.name], title)
	w.WriteHeader(http.StatusCreated)
}

// pushTitle opens a push to an account's device and reads its payload's
// title.
func pushTitle(body []byte, acc *pinAccount) (string, error) {
	plain, err := openPush(body, acc.device, acc.auth)
	if err != nil {
		return "", err
	}
	var payload notify.PushPayload
	err = json.Unmarshal(plain, &payload)
	return payload.Title, err
}

// titles are the titles of the pushes an account's device received.
func (c *pushCatcher) titles(acc *pinAccount) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.got[acc.name]...)
}

// serveUnhanded serves handler on a port of the registry until the test
// ends, and returns its base URL.
func serveUnhanded(t *testing.T, handler http.Handler) string {
	t.Helper()
	srv := httptest.NewUnstartedServer(handler)
	_ = srv.Listener.Close()
	srv.Listener = listenUnhanded(t)
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

// openPush decrypts a web push body as the browser does (RFC 8291, the
// aes128gcm content coding of RFC 8188 in one record): the header is a
// 16-byte salt, the record size, and the sender's one-time public key as the
// key id; the keys come from the ECDH secret of that key and the device's,
// and the device's auth secret; the record ends with the delimiter 2 and
// zero padding.
func openPush(body []byte, device *ecdh.PrivateKey, auth []byte) ([]byte, error) {
	if len(body) < 21 || len(body) < 21+int(body[20]) {
		return nil, errors.New("shorter than its header")
	}
	salt, keyID, record := body[:16], body[21:21+int(body[20])], body[21+int(body[20]):]
	sender, err := ecdh.P256().NewPublicKey(keyID)
	if err != nil {
		return nil, err
	}
	shared, err := device.ECDH(sender)
	if err != nil {
		return nil, err
	}
	ikm, err := hkdf.Key(sha256.New, shared, auth, "WebPush: info\x00"+string(device.PublicKey().Bytes())+string(keyID), 32)
	if err != nil {
		return nil, err
	}
	key, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, record, nil)
	if err != nil {
		return nil, err
	}
	plain = bytes.TrimRight(plain, "\x00")
	if len(plain) == 0 || plain[len(plain)-1] != 2 {
		return nil, errors.New("the record does not end with the last record's delimiter")
	}
	return plain[:len(plain)-1], nil
}
