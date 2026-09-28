//go:build unix

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The API tour's mail catcher (refactor plan §6.4 S5c). The server sends
// mail with net/smtp, which dials the SMTP host directly, never through
// HTTP_PROXY, so the recording proxy cannot see it; and with no
// OPENV_SMTP_HOST every send is a silent no-op, so nothing would show that a
// mail was attempted. An area that sets tourArea.mail gets a minimal SMTP
// server of the test's own on 127.0.0.1, through the port registry, which the
// server's OPENV_SMTP_HOST, OPENV_SMTP_PORT and OPENV_SMTP_FROM name (the
// golden shows the port as <mail catcher port>). It speaks what
// net/smtp.SendMail needs with no OPENV_SMTP_USER: a 220 greeting, EHLO or
// HELO answered with no extension (so no STARTTLS and no AUTH), MAIL, RCPT,
// DATA up to the lone dot, RSET, NOOP and QUIT; it refuses the recipients
// tourMailSpec.refuse names with 554 at RCPT, so the server's send fails.
//
// Every mail it receives, and every refusal, goes into the golden's
// outbound_mail: the envelope, the headers and the body's lines, normalised,
// sorted by their text (the server sends from goroutines, so the order they
// arrive in is not fixed), after the steps. Their wording is S10's to pin
// (I18); the tour pins that a mail went, to whom, with which link. An area
// awaits every mail it causes (tour.awaitMail) before it relies on it or
// ends; one that arrives after the area's last request fails the area.
//
// With a mail server the server requires a verified address of password
// accounts (unless OPENV_EMAIL_VERIFICATION=off), and walls an unverified
// one on every route but the auth ones, so tour.register (and tour.adopt's
// caller, by hand) follows the verification link the catcher received,
// through POST /api/v1/auth/verify-email (setup, one authIPLimiter token of
// 127.0.0.1's per account).

// tourMailSpec is an area's mail catcher (tourArea.mail).
type tourMailSpec struct {
	// refuse lists recipient addresses, or "@domain" suffixes, the catcher
	// refuses at RCPT TO with tourMailRefusal, so the server's send fails.
	refuse []string
}

// tourMailRefusal is the catcher's answer to a refused recipient.
const tourMailRefusal = "554 5.7.1 refused by the tour's mail catcher"

// tourMailCatcher is the SMTP server of an area.
type tourMailCatcher struct {
	ln   net.Listener
	spec *tourMailSpec
	wg   sync.WaitGroup
	mu   sync.Mutex
	msgs []*tourMailMessage
	open int       // connections open
	last time.Time // the last byte read or written
}

// tourMailMessage is one mail the catcher took, or one it refused.
type tourMailMessage struct {
	tr      *tour
	at      time.Time
	from    string
	to      []string
	refused string // the refusal the catcher answered at RCPT TO; no data then
	data    []byte // the message as sent, with its CRLF lines
}

// startTourMailCatcher listens on a port of the registry and serves each
// connection until the test ends.
func startTourMailCatcher(t *testing.T, spec *tourMailSpec) *tourMailCatcher {
	t.Helper()
	c := &tourMailCatcher{ln: listenUnhanded(t), spec: spec, last: time.Now()}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			conn, err := c.ln.Accept()
			if err != nil {
				return
			}
			c.touch(1)
			c.wg.Add(1)
			go func() {
				defer c.wg.Done()
				defer c.touch(-1)
				c.serve(conn)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = c.ln.Close()
		c.wg.Wait()
	})
	return c
}

func (c *tourMailCatcher) port() int { return c.ln.Addr().(*net.TCPAddr).Port }

// touch notes activity; open counts connections.
func (c *tourMailCatcher) touch(open int) {
	c.mu.Lock()
	c.open += open
	c.last = time.Now()
	c.mu.Unlock()
}

// refuses reports whether the catcher refuses a recipient.
func (c *tourMailCatcher) refuses(to string) bool {
	for _, r := range c.spec.refuse {
		if strings.EqualFold(to, r) || (strings.HasPrefix(r, "@") && strings.HasSuffix(strings.ToLower(to), strings.ToLower(r))) {
			return true
		}
	}
	return false
}

// serve speaks SMTP on one connection.
func (c *tourMailCatcher) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	br := bufio.NewReader(conn)
	say := func(line string) bool {
		c.touch(0)
		_, err := io.WriteString(conn, line+"\r\n")
		return err == nil
	}
	if !say("220 tour-mail-catcher ESMTP") {
		return
	}
	var from string
	var to []string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		c.touch(0)
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(line)
		if i := strings.IndexByte(verb, ' '); i >= 0 {
			verb = verb[:i]
		}
		switch verb {
		case "EHLO", "HELO":
			say("250 tour-mail-catcher")
		case "MAIL":
			from, to = smtpAddress(line), nil
			say("250 2.1.0 OK")
		case "RCPT":
			addr := smtpAddress(line)
			if c.refuses(addr) {
				c.add(&tourMailMessage{at: time.Now(), from: from, to: []string{addr}, refused: tourMailRefusal})
				say(tourMailRefusal)
				continue
			}
			to = append(to, addr)
			say("250 2.1.5 OK")
		case "DATA":
			if !say("354 End data with <CR><LF>.<CR><LF>") {
				return
			}
			var data bytes.Buffer
			for {
				l, err := br.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" || l == ".\n" {
					break
				}
				data.WriteString(strings.TrimPrefix(l, ".")) // dot-unstuffing
			}
			c.add(&tourMailMessage{at: time.Now(), from: from, to: to, data: data.Bytes()})
			from, to = "", nil
			say("250 2.0.0 OK: queued")
		case "RSET":
			from, to = "", nil
			say("250 2.0.0 OK")
		case "NOOP":
			say("250 2.0.0 OK")
		case "QUIT":
			say("221 2.0.0 Bye")
			return
		default:
			say("502 5.5.2 command not implemented")
		}
	}
}

// smtpAddress is the address between < and > of a MAIL or RCPT command.
func smtpAddress(line string) string {
	i, j := strings.IndexByte(line, '<'), strings.IndexByte(line, '>')
	if i < 0 || j < i {
		return ""
	}
	return line[i+1 : j]
}

func (c *tourMailCatcher) add(m *tourMailMessage) {
	c.mu.Lock()
	c.msgs = append(c.msgs, m)
	c.mu.Unlock()
}

// messages is what the catcher holds now.
func (c *tourMailCatcher) messages() []*tourMailMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*tourMailMessage(nil), c.msgs...)
}

// prepareMail starts the area's mail catcher and names it in the server's
// environment.
func (tr *tour) prepareMail(env map[string]string) map[string]string {
	if tr.area.mail == nil {
		return env
	}
	tr.mail = startTourMailCatcher(tr.t, tr.area.mail)
	env["OPENV_SMTP_HOST"] = "127.0.0.1"
	env["OPENV_SMTP_PORT"] = strconv.Itoa(tr.mail.port())
	env["OPENV_SMTP_FROM"] = "tour-mailer@example.com"
	tr.shown["OPENV_SMTP_PORT"] = "<mail catcher port>"
	return env
}

// awaitMail waits until the catcher holds n mails, or refusals, to an
// address, and returns them in the order they came.
func (tr *tour) awaitMail(to string, n int) []*tourMailMessage {
	tr.t.Helper()
	if tr.mail == nil {
		tr.t.Fatalf("awaitMail %s: the area has no mail catcher (tourArea.mail)", to)
	}
	deadline := time.Now().Add(tourAwaitWithin)
	for {
		var got []*tourMailMessage
		for _, m := range tr.mail.messages() {
			if contains(m.to, to) {
				m.tr = tr
				got = append(got, m)
			}
		}
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			tr.t.Fatalf("waited %s for %d mails to %s; the catcher holds %d for it, and %d in all", tourAwaitWithin, n, to,
				len(got), len(tr.mail.messages()))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// capture registers, under name, the first group of re (the whole match when
// it has none) in the mail: a token in its link.
func (m *tourMailMessage) capture(name, re string) string {
	m.tr.t.Helper()
	return m.tr.captureIn(name, string(m.data), re, "the mail to "+strings.Join(m.to, ", "))
}

// verifyByMail follows the verification link an account's first mail holds
// (setup), on a server that walls unverified accounts.
func (tr *tour) verifyByMail(a *tourActor) {
	tr.t.Helper()
	if tr.mail == nil {
		tr.t.Fatalf("%s's registration answered email_verified false, but the area has no mail catcher to read the "+
			"verification link from", a.name)
	}
	token := tr.awaitMail(a.email, 1)[0].capture(a.name+".verification", `verify-email\?token=([0-9a-f]{64})`)
	tr.setup("verify "+a.name+"'s address with the link the catcher received", tr.anon,
		"POST /api/v1/auth/verify-email", jsonBody(`{"token":"`+token+`"}`))
}

// tourMailQuiet is how long the catcher must have seen nothing, with no
// connection open, before the area's server is stopped.
const tourMailQuiet = 300 * time.Millisecond

// mailSettled waits, before the server is stopped, until the catcher has
// been quiet for tourMailQuiet with no connection open, and returns how
// many mails it holds then.
func (tr *tour) mailSettled() int {
	if tr.mail == nil {
		return 0
	}
	deadline := time.Now().Add(tourAwaitWithin)
	for {
		tr.mail.mu.Lock()
		open, last, n := tr.mail.open, tr.mail.last, len(tr.mail.msgs)
		tr.mail.mu.Unlock()
		if (open == 0 && time.Since(last) >= tourMailQuiet) || time.Now().After(deadline) {
			return n
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// checkNoLateMail fails the area when a mail came after the catcher settled:
// one the server sent late, which a run could as well have missed.
func (tr *tour) checkNoLateMail(settled int) {
	if tr.mail == nil {
		return
	}
	if got := tr.mail.messages(); len(got) > settled {
		tr.t.Errorf("%d mail(s) reached the catcher after the area's last request, so whether the golden holds them "+
			"depends on timing: await each mail the area causes (tour.awaitMail) before the area ends (the first to %s)",
			len(got)-settled, strings.Join(got[settled].to, ", "))
	}
}

// tourMailRecord is one mail, or refusal, in the golden's outbound_mail.
type tourMailRecord struct {
	From    string   `json:"envelope_from"`
	To      []string `json:"envelope_to"`
	Refused string   `json:"refused,omitempty"`
	Headers []string `json:"headers,omitempty"`
	Body    []string `json:"body,omitempty"`
}

// renderMail lists what the catcher took, normalised and sorted by its text
// (the upload list's way: sorted without numbers, numbered in that order,
// then sorted by the result, which is then the same on every run).
func (tr *tour) renderMail() []tourMailRecord {
	if tr.mail == nil {
		return nil
	}
	msgs := tr.mail.messages()
	flat := tr.norm.clone(true)
	record := func(n *tourNormaliser, m *tourMailMessage) tourMailRecord {
		rec := tourMailRecord{From: n.text(m.from), Refused: m.refused, To: []string{}}
		for _, to := range m.to {
			rec.To = append(rec.To, n.text(to))
		}
		if m.data == nil {
			return rec
		}
		text := strings.ReplaceAll(string(m.data), "\r\n", "\n")
		head, body, _ := strings.Cut(text, "\n\n")
		rec.Headers = strings.Split(n.text(head), "\n")
		rec.Body, _ = textLines(n.text(body))
		return rec
	}
	key := func(m *tourMailMessage) string {
		b, _ := json.Marshal(record(flat, m))
		return string(b)
	}
	sort.SliceStable(msgs, func(i, j int) bool { return key(msgs[i]) < key(msgs[j]) })
	out := make([]tourMailRecord, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, record(tr.norm, m))
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := json.Marshal(out[i])
		b, _ := json.Marshal(out[j])
		return string(a) < string(b)
	})
	return out
}

// TestTourMailCatcher checks the mail catcher with no database: a mail sent
// as the server sends one (net/smtp.SendMail, no auth) is taken whole, a
// refused recipient fails the send and is recorded as refused, and the
// golden's list is the same whatever order the mails came in.
func TestTourMailCatcher(t *testing.T) {
	c := startTourMailCatcher(t, &tourMailSpec{refuse: []string{"@refused.example"}})
	addr := fmt.Sprintf("127.0.0.1:%d", c.port())
	send := func(to, subject, body string) error {
		msg := "From: tour-mailer@example.com\r\nTo: " + to + "\r\nSubject: " + subject +
			"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + strings.ReplaceAll(body, "\n", "\r\n")
		return smtp.SendMail(addr, nil, "tour-mailer@example.com", []string{to}, []byte(msg))
	}
	if err := send("a@example.com", "First", "Hello,\n.a line that starts with a dot\nhttps://x/verify-email?token="+
		strings.Repeat("ab", 32)+"\n"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := send("b@refused.example", "Second", "no"); err == nil || !strings.Contains(err.Error(), "554") {
		t.Fatalf("a refused recipient: %v", err)
	}
	if err := send("c@example.com", "Third", "Bye"); err != nil {
		t.Fatalf("send: %v", err)
	}
	msgs := c.messages()
	if len(msgs) != 3 || msgs[1].refused != tourMailRefusal || !bytes.Contains(msgs[0].data, []byte("\r\n.a line")) {
		t.Fatalf("the catcher holds: %+v", msgs)
	}
	tr := &tour{t: t, norm: newTourNormaliser(), names: map[string]string{}, values: map[string]string{}, mail: c}
	if got := tr.awaitMail("a@example.com", 1)[0].capture("a.verification", `verify-email\?token=([0-9a-f]{64})`); got != strings.Repeat("ab", 32) {
		t.Errorf("the captured token: %s", got)
	}
	encode := func(v any) string {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false) // as the golden is written
		_ = enc.Encode(v)
		return strings.TrimSpace(b.String())
	}
	first := tr.renderMail()
	c.msgs[0], c.msgs[2] = c.msgs[2], c.msgs[0]
	again := tr.renderMail()
	a, b := encode(first), encode(again)
	want := `[{"envelope_from":"tour-mailer@example.com","envelope_to":["a@example.com"],"headers":["From: tour-mailer@example.com",` +
		`"To: a@example.com","Subject: First","MIME-Version: 1.0","Content-Type: text/plain; charset=UTF-8"],` +
		`"body":["Hello,",".a line that starts with a dot","https://x/verify-email?token=<a.verification>"]},` +
		`{"envelope_from":"tour-mailer@example.com","envelope_to":["b@refused.example"],"refused":"` + tourMailRefusal + `"},` +
		`{"envelope_from":"tour-mailer@example.com","envelope_to":["c@example.com"],"headers":["From: tour-mailer@example.com",` +
		`"To: c@example.com","Subject: Third","MIME-Version: 1.0","Content-Type: text/plain; charset=UTF-8"],"body":["Bye"]}]`
	if a != want || b != want {
		t.Errorf("the rendered mail:\n got %s\nthen %s\nwant %s", a, b, want)
	}
	if n := tr.mailSettled(); n != 3 {
		t.Errorf("settled with %d mails", n)
	}
}
