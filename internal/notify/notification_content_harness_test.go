package notify

// The recording side of TestNotificationContent (refactor plan step S10):
// fakes for every channel a notification leaves by, and the attribution of
// each SSE frame, email and push to the stored row it belongs to. See
// notification_content_test.go for what the goldens pin and why.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net/smtp"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/pushsubs"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// ncUpdateEnv set to exactly 1 rewrites the goldens from the current code;
// any other value compares. Only a deliberate, release-noted behavior change
// regenerates them; a refactor never does.
const ncUpdateEnv = "UPDATE_GOLDEN"

// ncGoldenDir holds one directory per notification type, named by the
// type's value, each with the four files ncGoldenFiles names.
const ncGoldenDir = "testdata/notifications"

// ncGoldenFiles are the four goldens of a type: the stored row, the SSE
// frame, the email and the web push. internal/domain/notifications's
// completeness test reads the same names.
var ncGoldenFiles = []string{"row.json", "sse.txt", "email.txt", "push.json"}

// ncTypesDir is the package that declares the notification type constants.
const ncTypesDir = "../domain/notifications"

func ncUpdating() bool { return os.Getenv(ncUpdateEnv) == "1" }

// ncRegenerateCommand is the one command that rewrites these goldens.
const ncRegenerateCommand = ncUpdateEnv + "=1 go test ./internal/notify -count=1 -run '^TestNotificationContent$'"

// ncRegenerate is the command with the reminder a failure prints.
func ncRegenerate() string {
	return ncRegenerateCommand + "\n(only " + ncUpdateEnv + "=1 regenerates; any other value compares)"
}

// ncTypeConstant is one exported Type* string constant of the notifications
// package, in source order.
type ncTypeConstant struct{ name, value string }

// ncTypeConstants parses the notifications package's production files for
// its Type* string constants, so a type added there is found here with no
// edit to this test.
func ncTypeConstants(t *testing.T) []ncTypeConstant {
	t.Helper()
	entries, err := os.ReadDir(ncTypesDir)
	if err != nil {
		t.Fatalf("read %s: %v", ncTypesDir, err)
	}
	var files []string
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			files = append(files, n)
		}
	}
	sort.Strings(files)
	fset := token.NewFileSet()
	var out []ncTypeConstant
	for _, name := range files {
		f, err := parser.ParseFile(fset, filepath.Join(ncTypesDir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, id := range vs.Names {
					if !strings.HasPrefix(id.Name, "Type") || !id.IsExported() || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					v, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("%s: %s: %v", name, id.Name, err)
					}
					out = append(out, ncTypeConstant{name: id.Name, value: v})
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no Type* string constants found in %s", ncTypesDir)
	}
	return out
}

// --- Recording ---------------------------------------------------------

// ncFrame is one SSE broadcast: the stream key, the event name, and the data
// line exactly as the hub writes it (json.Marshal of the value passed to
// BroadcastSession; internal/api/sse.go, pinned by S6).
type ncFrame struct {
	key, event string
	data       []byte
}

// ncEmail is one SMTP hand-off: the envelope and the wire message.
type ncEmail struct {
	addr, from string
	to         []string
	auth       bool
	msg        []byte
}

// ncPush is one web push send: the device and the payload before
// encryption, which is what the service worker receives.
type ncPush struct {
	endpoint string
	payload  []byte
}

// ncDelivery is one stored row and every channel event that followed it
// before the next row was stored.
type ncDelivery struct {
	row    notifications.Notification
	frames []ncFrame
	emails []ncEmail
	pushes []ncPush
	// seq is the row's channel events in the order they were recorded:
	// "sse", "email" and "push" (see ncOrderProblem).
	seq []string
}

// ncRecorder collects one scenario's deliveries. Every channel event is
// attributed to the row stored last before it: the SSE frame and the email
// are sent on the caller's goroutine right after the row is stored, and the
// store waits for the push workers before it stores the next row, so every
// push of a row lands while that row is the last one. The SSE frame and the
// email also wait for the push workers before they are recorded, so a push
// queued before either is recorded before it (see ncOrderProblem).
type ncRecorder struct {
	mu         sync.Mutex
	push       *PushDispatcher
	deliveries []*ncDelivery
	strays     []string
	// refused is every recipient whose row the store refused, in order.
	refused []string
}

func (r *ncRecorder) reset() {
	r.push.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deliveries, r.strays, r.refused = nil, nil, nil
}

// attach records an event against the last stored row, or as a stray when
// no row has been stored yet in this scenario.
func (r *ncRecorder) attach(what string, add func(d *ncDelivery)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.deliveries) == 0 {
		r.strays = append(r.strays, what)
		return
	}
	add(r.deliveries[len(r.deliveries)-1])
}

func (r *ncRecorder) take() ([]*ncDelivery, []string) {
	r.push.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deliveries, r.strays
}

// refusals is every recipient whose row the store refused since reset.
func (r *ncRecorder) refusals() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.refused...)
}

// ncStore is the notifications.Service every delivery path stores through.
// It refuses the row of every recipient in refuse, as a database error would,
// and stores no row for it.
type ncStore struct {
	notifications.Service
	rec    *ncRecorder
	refuse map[string]bool
}

// ncRefused is the error the store answers a refused row with.
var ncRefused = errors.New("refused")

func (s *ncStore) Create(n *notifications.Notification) error {
	// Every push of the previous row has been sent before this row exists.
	s.rec.push.Wait()
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	if s.refuse[n.UserID] {
		s.rec.refused = append(s.rec.refused, n.UserID)
		return ncRefused
	}
	s.rec.deliveries = append(s.rec.deliveries, &ncDelivery{row: *n})
	return nil
}

// ncBroadcaster renders each broadcast as the SSE hub would write its data.
type ncBroadcaster struct{ rec *ncRecorder }

func (b *ncBroadcaster) BroadcastSession(key, event string, data interface{}) {
	payload, err := json.Marshal(data)
	if err != nil {
		payload = []byte("<json.Marshal error: " + err.Error() + ">")
	}
	// A push already queued for this row is sent, and recorded, first.
	b.rec.push.Wait()
	b.rec.attach("SSE "+event+" on "+key, func(d *ncDelivery) {
		d.frames = append(d.frames, ncFrame{key: key, event: event, data: payload})
		d.seq = append(d.seq, "sse")
	})
}

// smtpSend stands in for smtp.SendMail under the real SMTPMailer.
func (r *ncRecorder) smtpSend(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
	e := ncEmail{addr: addr, from: from, to: append([]string(nil), to...), auth: a != nil,
		msg: append([]byte(nil), msg...)}
	// A push already queued for this row is sent, and recorded, first.
	r.push.Wait()
	r.attach("email to "+strings.Join(to, ","), func(d *ncDelivery) {
		d.emails = append(d.emails, e)
		d.seq = append(d.seq, "email")
	})
	return nil
}

// ncPushSender records each send and answers 201, as a push service does.
type ncPushSender struct{ rec *ncRecorder }

func (s *ncPushSender) Send(sub *pushsubs.Subscription, payload []byte) (int, error) {
	p := ncPush{endpoint: sub.Endpoint, payload: append([]byte(nil), payload...)}
	s.rec.attach("push to "+sub.Endpoint, func(d *ncDelivery) {
		d.pushes = append(d.pushes, p)
		d.seq = append(d.seq, "push")
	})
	return 201, nil
}

// ncSubs gives every recipient one subscribed device.
type ncSubs struct{}

func (ncSubs) ListForUser(userID string) ([]*pushsubs.Subscription, error) {
	return []*pushsubs.Subscription{{ID: "sub-" + userID, UserID: userID,
		Endpoint: ncEndpoint(userID), P256dh: "p256dh", Auth: "auth"}}, nil
}
func (ncSubs) Forget(string) (int64, error)       { return 0, nil }
func (ncSubs) MarkUsed(string, time.Time) error   { return nil }
func (ncSubs) MarkFailed(string, time.Time) error { return nil }

// ncEndpoint is the one device ncSubs gives a recipient.
func ncEndpoint(userID string) string { return "https://push.example.test/" + userID }

// ncUsers answers every recipient as opted in to email and push, with an
// address made from the id ("user-ada" is ada@example.test).
type ncUsers struct{}

func (ncUsers) GetByID(id string) (*users.User, error) {
	return &users.User{ID: id, Email: ncAddress(id), EmailNotifications: true, PushNotifications: true}, nil
}

// ncAddress is the email address ncUsers gives a recipient.
func ncAddress(userID string) string { return strings.TrimPrefix(userID, "user-") + "@example.test" }

// ncChannels is one wiring of the side channels, read from the environment
// once, as cmd/server does at boot.
type ncChannels struct {
	rec   *ncRecorder
	store *ncStore
	bc    *ncBroadcaster
	email *EmailDispatcher
	push  *PushDispatcher
}

// ncLinkBase is the frontend base URL the email links start with; the
// trailing slash is FRONTEND_URL as an operator may write it.
const ncLinkBase = "https://openv.example.test/"

func newNCChannels(t *testing.T) *ncChannels {
	t.Helper()
	t.Setenv("OPENV_SMTP_HOST", "smtp.example.test")
	t.Setenv("OPENV_SMTP_PORT", "")
	t.Setenv("OPENV_SMTP_USER", "")
	t.Setenv("OPENV_SMTP_PASSWORD", "")
	t.Setenv("OPENV_SMTP_FROM", "openv@example.test")
	rec := &ncRecorder{}
	mailer := MailerFromEnv()
	mailer.send = rec.smtpSend
	ch := &ncChannels{
		rec:   rec,
		store: &ncStore{rec: rec},
		bc:    &ncBroadcaster{rec: rec},
		email: NewEmailDispatcher(mailer, ncUsers{}, ncLinkBase, EmailTypesFromEnv()),
		push:  NewPushDispatcher(&ncPushSender{rec: rec}, ncSubs{}, ncUsers{}, PushTypesFromEnv()),
	}
	rec.push = ch.push
	return ch
}

// --- Order and addressing ----------------------------------------------

// ncChannelRank is each channel's place in one row's delivery: every site
// sends the SSE frame, then the email, then the push.
var ncChannelRank = map[string]int{"sse": 0, "email": 1, "push": 2}

// ncOrderProblem reports a row whose channels were recorded out of that
// order, or "". The frame and the email are sent on the caller's goroutine,
// in the order the site sends them, so an email before the frame (the bell
// waiting on an SMTP round trip) shows. The push is sent from a worker of
// the push dispatcher once it is queued, but the recording broadcaster and
// mailer wait for the push workers (PushDispatcher.Wait) before they record
// a frame or an email, so a push queued before either is recorded before it
// and shows too. Under today's order nothing is queued at those points, and
// the waits return at once.
func ncOrderProblem(d *ncDelivery) string {
	for i := 1; i < len(d.seq); i++ {
		if ncChannelRank[d.seq[i]] < ncChannelRank[d.seq[i-1]] {
			return fmt.Sprintf("to %s the channels arrived as %s; each row's SSE frame comes before its email, "+
				"and its email before its push", d.row.UserID, strings.Join(d.seq, " → "))
		}
	}
	return ""
}

// ncAddressProblems reports any channel event of a row that is not
// addressed to the row's recipient: the SSE frame on their stream, the
// email to their address, the push to their device.
func ncAddressProblems(d *ncDelivery) []string {
	user := d.row.UserID
	var out []string
	for _, f := range d.frames {
		if f.key != StreamKey(user) || f.event != "notification" {
			out = append(out, fmt.Sprintf("the row for %s was followed by SSE %s on %s", user, f.event, f.key))
		}
	}
	for _, e := range d.emails {
		if len(e.to) != 1 || e.to[0] != ncAddress(user) {
			out = append(out, fmt.Sprintf("the row for %s was followed by an email to %s", user, strings.Join(e.to, ", ")))
		}
	}
	for _, p := range d.pushes {
		if p.endpoint != ncEndpoint(user) {
			out = append(out, fmt.Sprintf("the row for %s was followed by a push to %s", user, p.endpoint))
		}
	}
	return out
}

// ncLogs records every log record at Info and above (what a deployment
// logs) as one line: the level, the message and each attribute as key=value,
// in the order the call gave them.
type ncLogs struct {
	mu    sync.Mutex
	lines []string
}

func (l *ncLogs) Enabled(_ context.Context, level slog.Level) bool { return level >= slog.LevelInfo }

func (l *ncLogs) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Level.String() + " " + r.Message)
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%s", a.Key, a.Value.Resolve().String())
		return true
	})
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, b.String())
	return nil
}

// WithAttrs and WithGroup are not used by the delivery paths; a logger built
// with either would lose its attributes here, which the exact comparison of
// the lines would show.
func (l *ncLogs) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *ncLogs) WithGroup(string) slog.Handler      { return l }

func (l *ncLogs) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.lines
	l.lines = nil
	return out
}

// --- Rendering ---------------------------------------------------------

// ncRun is one scenario's deliveries under one wiring, with the wall clock
// around it (notifications.New stamps CreatedAt with time.Now).
type ncRun struct {
	deliveries []*ncDelivery
	start, end time.Time
}

// ncRowView is the stored row with its two generated values normalised: the
// id (checked to be a UUID) and CreatedAt (checked to be the wall clock
// during the scenario). Every other field is the value handed to the store,
// and EntityRef's Go value types are listed beside it, since the jsonb
// column keeps an int and a string apart.
func ncRowView(d *ncDelivery, run ncRun) (map[string]interface{}, []string) {
	var problems []string
	out := map[string]interface{}{}
	v := reflect.ValueOf(d.row)
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		fv := v.Field(i).Interface()
		switch f.Name {
		case "ID":
			if _, err := uuid.Parse(d.row.ID); err != nil {
				problems = append(problems, fmt.Sprintf("id %q is not a UUID", d.row.ID))
			}
			out[f.Name] = "<uuid>"
		case "CreatedAt":
			if d.row.CreatedAt.Before(run.start) || d.row.CreatedAt.After(run.end) {
				problems = append(problems, fmt.Sprintf("created_at %s is not the wall clock during the delivery (%s to %s)",
					d.row.CreatedAt.Format(time.RFC3339Nano), run.start.Format(time.RFC3339Nano), run.end.Format(time.RFC3339Nano)))
			}
			out[f.Name] = "<time.Now() when the row was built>"
		case "EntityRef":
			out[f.Name] = d.row.EntityRef
			types := map[string]string{}
			for k, val := range d.row.EntityRef {
				types[k] = fmt.Sprintf("%T", val)
			}
			out["EntityRef Go types"] = types
		default:
			out[f.Name] = fv
		}
	}
	return out, problems
}

// ncNormalise replaces the row's id and created_at, as JSON encodes them, in
// an SSE data line.
func ncNormalise(data []byte, row notifications.Notification) string {
	s := string(data)
	if id, err := json.Marshal(row.ID); err == nil {
		s = strings.ReplaceAll(s, string(id), `"<id>"`)
	}
	if at, err := json.Marshal(row.CreatedAt); err == nil {
		s = strings.ReplaceAll(s, string(at), `"<created_at>"`)
	}
	return s
}

// ncWire shows an SMTP message with each CRLF as a line end. A bare LF or CR
// on the wire would show as ␊ or ␍, so the golden pins the line ends too.
func ncWire(msg []byte) string {
	var b strings.Builder
	for i := 0; i < len(msg); i++ {
		switch {
		case msg[i] == '\r' && i+1 < len(msg) && msg[i+1] == '\n':
			b.WriteByte('\n')
			i++
		case msg[i] == '\r':
			b.WriteString("␍")
		case msg[i] == '\n':
			b.WriteString("␊\n")
		default:
			b.WriteByte(msg[i])
		}
	}
	return b.String()
}

// ncJSON renders a JSON golden: two-space indent, sorted map keys, no HTML
// escaping, and a trailing newline.
func ncJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatalf("encode golden: %v", err)
	}
	return buf.Bytes()
}

// --- Golden files --------------------------------------------------------

// ncCheckGolden compares got with the golden at path (relative to this
// package), or writes it under UPDATE_GOLDEN=1.
func ncCheckGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	shown := "internal/notify/" + filepath.ToSlash(path)
	if ncUpdating() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create the directory for %s: %v", shown, err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", shown, err)
		}
		t.Logf("regenerated %s", shown)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nCreate it with:\n  %s", shown, err, ncRegenerate())
	}
	if !bytes.Equal(want, got) {
		t.Errorf("golden %s does not match what the code delivers:\n%s\n"+
			"A refactor never changes a golden (X6 rewrites these deliveries and must leave all of them byte for byte).\n"+
			"If this is a deliberate, release-noted behavior change, regenerate it with:\n  %s",
			shown, ncDiff(string(want), string(got)), ncRegenerate())
	}
}

// ncDiffLimit caps the diff a failure prints.
const ncDiffLimit = 60

// ncDiff is a compact line diff of the golden (want) against the current
// output (got): changed lines marked - and +, each run with two lines of
// context, headed by the golden's line number.
func ncDiff(want, got string) string {
	ops := ncDiffLines(strings.Split(want, "\n"), strings.Split(got, "\n"))
	show := make([]bool, len(ops))
	changed := 0
	for i, op := range ops {
		if op.kind == ' ' {
			continue
		}
		changed++
		for k := max(0, i-2); k <= min(len(ops)-1, i+2); k++ {
			show[k] = true
		}
	}
	var b strings.Builder
	printed := 0
	for i, op := range ops {
		if !show[i] {
			continue
		}
		if printed == ncDiffLimit {
			fmt.Fprintf(&b, "... (diff truncated: %d changed lines in all)\n", changed)
			break
		}
		if i == 0 || !show[i-1] {
			fmt.Fprintf(&b, "@@ golden line %d @@\n", op.line)
		}
		fmt.Fprintf(&b, "%c %s\n", op.kind, op.text)
		printed++
	}
	return b.String()
}

// ncDiffOp is one line of a diff: kind is ' ' (both), '-' (golden only) or
// '+' (current output only); line is the golden's line number at that point.
type ncDiffOp struct {
	kind byte
	text string
	line int
}

// ncDiffLines is a longest-common-subsequence line diff.
func ncDiffLines(a, b []string) []ncDiffOp {
	n, m := len(a), len(b)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []ncDiffOp
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			ops = append(ops, ncDiffOp{' ', a[i], i + 1})
			i++
			j++
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, ncDiffOp{'-', a[i], i + 1})
			i++
		default:
			ops = append(ops, ncDiffOp{'+', b[j], i + 1})
			j++
		}
	}
	return ops
}
