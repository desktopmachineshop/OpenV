package runner

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// Headless sign-in: driving a vendor CLI's login on a machine with no
// console and no browser, and relaying the whole flow to the member's own
// browser through the login broker. This is what makes a transient runner
// usable — the member signs their agent in without installing anything.
//
// Two shapes of flow need help here:
//
//   - TUI logins (Claude Code's `claude auth login`, and `claude setup-token`
//     when the member asks the runner to mint a saved token) render nothing over
//     pipes. They are driven over a pseudo-terminal instead: the URL is
//     scraped out of the terminal output, and the code the member pastes
//     back is typed into the terminal.
//   - Loopback logins (Codex) hand the vendor a redirect to a local port on
//     the machine running the CLI. The member's browser cannot reach it, so
//     the member copies the failed redirect out of their address bar and the
//     runner replays it against its own loopback listener.

// ansiPattern matches the escape sequences a TUI paints its output with.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\r`)

// stripANSI reduces terminal output to the text a human would see.
func stripANSI(s string) string { return ansiPattern.ReplaceAllString(s, "") }

// visibleLines recovers the lines a human would read from raw terminal output.
// A TUI redraws with carriage returns rather than newlines, so a plain line
// scanner can sit on a full screen of text without ever seeing one — which is
// how an error message can go unnoticed while the flow waits forever.
func visibleLines(raw string) []string {
	text := stripANSI(strings.ReplaceAll(raw, "\r", "\n"))
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// tuiFailureMarkers name a line that says what went wrong; tuiRetryMarkers
// only say that something did. Both are matched case-insensitively.
var (
	tuiFailureMarkers = []string{"invalid code", "oauth error", "error:", "failed"}
	tuiRetryMarkers   = []string{"to retry", "try again"}
)

func matchesAny(lower string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// tuiError returns the message a sign-in TUI is showing, or "" when it shows
// no sign of trouble.
//
// An explanation beats an instruction. The CLI draws both — "OAuth error:
// Invalid code…" above "Press Enter to retry." — and only the first tells the
// member anything they can act on; the second is addressed to a terminal they
// do not have. A retry prompt on its own is still worth relaying, since it
// means the code did not take. Within each kind the last line wins: a TUI
// redraws, so older frames sit earlier in the buffer.
func tuiError(raw string) string {
	failure, retry := "", ""
	for _, line := range visibleLines(raw) {
		lower := strings.ToLower(line)
		switch {
		case matchesAny(lower, tuiFailureMarkers):
			failure = line
		case matchesAny(lower, tuiRetryMarkers):
			retry = line
		}
	}
	if failure != "" {
		return failure
	}
	return retry
}

// ptyNudgeAfter is how long to wait for a URL before pressing Enter for the
// member: some CLIs open with a "press Enter to continue" prompt that never
// prints a URL until it is answered.
const ptyNudgeAfter = 8 * time.Second

// handlePTYLogin drives a TUI sign-in over a pseudo-terminal, relaying its
// URL out and the member's pasted code back in.
func (b *loginBroker) handlePTYLogin(ctx context.Context, login *providers.LoginRequest, flow loginFlow) {
	if !ptySupported {
		b.loginProgress(login.ID, providers.LoginFailed, "",
			"this runner cannot open a terminal for the sign-in flow")
		return
	}
	cmd := exec.CommandContext(ctx, flow.command[0], flow.command[1:]...)
	cmd.Env = childEnv(b.cleanSlate(ctx, login, flow)...)
	// A TUI that believes it is on a dumb terminal prints nothing useful.
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")

	tty, err := startPTY(cmd)
	if err != nil {
		b.loginProgress(login.ID, providers.LoginFailed, "", "failed to open a terminal for the sign-in: "+err.Error())
		return
	}
	defer tty.Close()

	// Read raw chunks rather than lines: the sign-in TUI redraws with
	// carriage returns and can render a whole screen — an error among it —
	// without ever emitting a newline.
	chunks := make(chan string, 64)
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := tty.Read(buf)
			if n > 0 {
				chunks <- string(buf[:n])
			}
			if err != nil {
				close(chunks)
				return
			}
		}
	}()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	const pasteDetail = "Open the sign-in link, authorize, then paste the code you are given back here."

	screen := newScreenBuffer(32 * 1024)
	// minted holds what the terminal drew after the code was typed, which
	// is where `claude setup-token` prints its token; only a minting flow
	// keeps it, and it never leaves this function but as the token itself.
	minted := newScreenBuffer(32 * 1024)
	tail := newLineTail(40)
	urlReported := false
	nudged := false
	// sentCode is the code last typed into the terminal. A member who pastes
	// a different one is retrying, and gets another go — the CLI itself
	// offers a retry after a bad code, so refusing one here is what left the
	// flow stuck at "code sent".
	sentCode := ""
	reportedErr := ""
	var quiet quietWatch // what to relay when the CLI goes quiet after a code

	pollTicker := time.NewTicker(2 * time.Second)
	defer pollTicker.Stop()
	nudge := time.NewTimer(ptyNudgeAfter)
	defer nudge.Stop()

	for {
		select {
		case err := <-done:
			b.finishPTYLogin(ctx, login, flow, err, chunks, minted, tail)
			return
		case chunk, ok := <-chunks:
			if !ok {
				continue
			}
			screen.add(chunk)
			if flow.mintsToken && sentCode != "" {
				minted.add(chunk)
			}
			for _, line := range visibleLines(chunk) {
				tail.add(line)
			}
			if !urlReported {
				if authURL := authURLPattern.FindString(stripANSI(chunk)); authURL != "" {
					b.loginProgressKind(login.ID, providers.LoginAwaitingCode, authURL, pasteDetail, providers.PasteKindCode)
					urlReported = true
				}
			}
			// Once a code has been typed, whatever the CLI says about it is
			// the member's answer: relay it instead of waiting for an exit
			// that a retry prompt will never produce.
			if sentCode != "" {
				if msg := redactSecrets(tuiError(screen.String())); msg != "" && msg != reportedErr {
					reportedErr = msg
					log.Printf("login %s: the sign-in terminal reported: %s", login.ID, msg)
					b.loginProgressKind(login.ID, providers.LoginAwaitingCode, "",
						msg+" — paste the code again, in full, to retry.", providers.PasteKindCode)
				}
			}
		case <-nudge.C:
			// No URL yet: answer the "press Enter" prompt once, then tell
			// the member what is happening rather than leaving a blank card.
			if !urlReported && !nudged {
				nudged = true
				_, _ = io.WriteString(tty, "\r")
				nudge.Reset(ptyNudgeAfter)
				continue
			}
			if !urlReported {
				b.loginProgressKind(login.ID, providers.LoginAwaitingCode, "",
					"The sign-in CLI has not printed a link yet. Output so far: "+tail.String(),
					providers.PasteKindCode)
				urlReported = true
			}
		case <-pollTicker.C:
			quiet.check(b, login, sentCode, screen)
			current, err := b.client.GetLoginFull(login.ID)
			if err != nil {
				continue
			}
			if current.Status == providers.LoginCancelled {
				killTree(cmd)
				<-done
				return
			}
			code := strings.TrimSpace(current.Code)
			if code == "" || code == sentCode {
				continue
			}
			// A retry lands on the CLI's "press Enter to retry" prompt, so
			// clear that first; on the first attempt the extra Enter is
			// harmless at an empty paste field.
			if sentCode != "" {
				if _, err := io.WriteString(tty, "\r"); err != nil {
					log.Printf("login %s: clearing the retry prompt failed: %v", login.ID, err)
				}
				time.Sleep(250 * time.Millisecond)
			}
			sentCode = code
			quiet = quietWatch{codeAt: time.Now()}
			reportedErr = ""
			screen.reset()
			minted.reset()
			if _, err := io.WriteString(tty, code+"\r"); err != nil {
				log.Printf("login %s: writing code to terminal failed: %v", login.ID, err)
				continue
			}
			log.Printf("login %s: code submitted to the sign-in terminal", login.ID)
			b.loginProgressKind(login.ID, providers.LoginAwaitingCode, "",
				"Code received — completing the sign-in on the runner…", providers.PasteKindCode)
		case <-ctx.Done():
			killTree(cmd)
			<-done
			b.loginProgress(login.ID, providers.LoginFailed, "", "sign-in timed out after 10 minutes"+screenSays(screen))
			return
		}
	}
}

// screenBuffer keeps the tail of what a TUI has drawn, so a message can be
// read out of output that arrives in arbitrary chunks.
type screenBuffer struct {
	max  int
	text strings.Builder
}

func newScreenBuffer(max int) *screenBuffer { return &screenBuffer{max: max} }

func (b *screenBuffer) add(chunk string) {
	b.text.WriteString(chunk)
	if b.text.Len() <= b.max {
		return
	}
	trimmed := b.text.String()
	trimmed = trimmed[len(trimmed)-b.max:]
	b.text.Reset()
	b.text.WriteString(trimmed)
}

func (b *screenBuffer) String() string { return b.text.String() }

// reset drops what the TUI drew before the current attempt, so a stale error
// from a previous paste is never reported against a new one.
func (b *screenBuffer) reset() { b.text.Reset() }

// signOutTimeout bounds the sign-out a headless sign-in starts with.
const signOutTimeout = 20 * time.Second

// cleanSlate signs the CLI out of whatever it already holds and returns the
// entries a headless sign-in adds to childEnv: the flow's own, then its
// credential variables blanked. A sign-out that fails is logged and the
// sign-in carries on: it is a precaution, and the sign-in may still succeed.
func (b *loginBroker) cleanSlate(ctx context.Context, login *providers.LoginRequest, flow loginFlow) []string {
	extra := append([]string(nil), flow.env...)
	for _, name := range flow.clearEnv {
		extra = append(extra, name+"=") // the later entry wins: unset for the CLI
	}
	if len(flow.signOut) == 0 {
		return extra
	}
	outCtx, cancel := context.WithTimeout(ctx, signOutTimeout)
	defer cancel()
	out := exec.CommandContext(outCtx, flow.signOut[0], flow.signOut[1:]...)
	out.Env = childEnv(extra...)
	if err := out.Run(); err != nil {
		log.Printf("login %s: signing the CLI out first failed, carrying on: %v", login.ID, err)
	} else {
		log.Printf("login %s: signed the CLI out first", login.ID)
	}
	return extra
}

// quietAfter is how long a sign-in may sit silent after the member's code
// before the runner shows the member what its terminal says.
var quietAfter = 30 * time.Second

// quietWatch relays the terminal's screen once a sign-in has gone quiet
// after a code was typed, and again each time that screen changes, so a
// member is never left on "code received" with no idea what the CLI is
// waiting for.
type quietWatch struct {
	codeAt   time.Time
	reported string
}

func (q *quietWatch) check(b *loginBroker, login *providers.LoginRequest, sentCode string, screen *screenBuffer) {
	if sentCode == "" || q.codeAt.IsZero() || time.Since(q.codeAt) < quietAfter {
		return
	}
	says := screenSays(screen)
	if says == q.reported {
		return
	}
	q.reported = says
	log.Printf("login %s: still waiting after the code%s", login.ID, says)
	b.loginProgressKind(login.ID, providers.LoginAwaitingCode, "",
		"Still waiting for the sign-in to finish on the runner"+says+
			" — if it asks for something, paste the code again, or cancel and start over.", providers.PasteKindCode)
}

// screenSays is the last lines the sign-in terminal shows, credentials
// hidden, as a clause to append to a message ("" when it shows nothing).
func screenSays(screen *screenBuffer) string {
	lines := visibleLines(screen.String())
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	text := redactSecrets(strings.Join(lines, " · "))
	if len(text) > 600 {
		text = text[len(text)-600:]
	}
	return ". The runner's terminal shows: " + text
}

// finishPTYLogin reports how a terminal sign-in ended: a minting flow that
// succeeded hands its token in, and anything else is finishLogin's.
func (b *loginBroker) finishPTYLogin(ctx context.Context, login *providers.LoginRequest, flow loginFlow,
	waitErr error, chunks <-chan string, minted *screenBuffer, tail *lineTail) {
	if flow.mintsToken && waitErr == nil && ctx.Err() == nil {
		// Read out whatever is still buffered: the token is the last thing
		// the CLI draws before it exits.
		drainChunks(chunks, minted, outputDrainTimeout)
		b.saveMintedToken(login, minted.String())
		return
	}
	b.finishLogin(ctx, login, waitErr, ctx.Err(), tail)
}

// drainChunks adds to buf what the terminal still has buffered, until the
// reader closes the channel or the wait runs out (something the CLI started
// may hold the terminal open after it exits).
func drainChunks(chunks <-chan string, buf *screenBuffer, wait time.Duration) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				return
			}
			buf.add(chunk)
		case <-timer.C:
			return
		}
	}
}

// saveMintedToken hands the server the token `claude setup-token` printed,
// which completes the request there; it reports a failure itself. Neither
// the token nor the output it was read from is logged or relayed.
func (b *loginBroker) saveMintedToken(login *providers.LoginRequest, output string) {
	text := stripANSI(output)
	token := subscriptionTokenPattern.FindString(text)
	if token == "" {
		b.loginProgress(login.ID, providers.LoginFailed, "",
			"the sign-in finished but printed no token — start again, and paste the whole code")
		return
	}
	if err := b.client.SaveLoginToken(login.ID, token, mintedLifetime(text)); err != nil {
		b.loginProgress(login.ID, providers.LoginFailed, "", "the runner minted a token but could not save it: "+redactSecrets(err.Error()))
		return
	}
	log.Printf("login %s: minted a token and handed it to the server", login.ID)
}

// validForPattern finds the lifetime `claude setup-token` prints with its
// token: "Your OAuth token (valid for 1 year):".
var validForPattern = regexp.MustCompile(`(?i)valid for\s+([^)]{1,60})\)`)

// durationPartPattern is one number and unit of that lifetime.
var durationPartPattern = regexp.MustCompile(`(?i)(\d+)\s*(years?|y|months?|mo|weeks?|w|days?|d|hours?|hrs?|h|minutes?|mins?|m)\b`)

// mintedLifetime reads the lifetime the CLI printed with the token, summing
// its parts ("1 year", "30 days", "12h 30m"); 0 when it printed none the
// runner can read, which the server takes as the CLI's default.
func mintedLifetime(text string) time.Duration {
	m := validForPattern.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	var total time.Duration
	for _, part := range durationPartPattern.FindAllStringSubmatch(m[1], -1) {
		n, err := strconv.Atoi(part[1])
		if err != nil {
			return 0
		}
		unit := strings.ToLower(part[2])
		switch {
		case strings.HasPrefix(unit, "y"):
			total += time.Duration(n) * 365 * 24 * time.Hour
		case strings.HasPrefix(unit, "mo"):
			total += time.Duration(n) * 30 * 24 * time.Hour
		case strings.HasPrefix(unit, "w"):
			total += time.Duration(n) * 7 * 24 * time.Hour
		case strings.HasPrefix(unit, "d"):
			total += time.Duration(n) * 24 * time.Hour
		case strings.HasPrefix(unit, "h"):
			total += time.Duration(n) * time.Hour
		default:
			total += time.Duration(n) * time.Minute
		}
	}
	return total
}

// finishLogin reports the terminal state of a sign-in the CLI has exited.
func (b *loginBroker) finishLogin(ctx context.Context, login *providers.LoginRequest, waitErr, ctxErr error, tail *lineTail) {
	if ctxErr == context.DeadlineExceeded {
		b.loginProgress(login.ID, providers.LoginFailed, "", "sign-in timed out after 10 minutes")
		return
	}
	if waitErr != nil {
		b.loginProgress(login.ID, providers.LoginFailed, "",
			signInFailure(login.Provider,
				"sign-in command failed: "+waitErr.Error()+" — output tail: "+tail.String()))
		return
	}
	b.loginProgress(login.ID, providers.LoginCompleted, "", "Signed in successfully.")
	b.redetect(ctx, login.Provider)
	log.Printf("login %s: %s sign-in completed", login.ID, login.Provider)
}

// loopbackDetail tells the member what to do with a redirect their browser
// cannot follow.
const loopbackDetail = "Open the sign-in link and authorize. Your browser will then try to reach a page on " +
	"the runner and show a connection error — that is expected. Copy the whole address " +
	"from your browser's address bar and paste it here."

// handleLoopbackLogin drives a CLI whose OAuth redirect points at a port on
// the machine running it. The member pastes the redirect back and the runner
// replays it against its own loopback listener, which completes the flow.
func (b *loginBroker) handleLoopbackLogin(ctx context.Context, login *providers.LoginRequest, flow loginFlow) {
	cmd := exec.CommandContext(ctx, flow.command[0], flow.command[1:]...)
	cmd.Env = childEnv(flow.env...)

	out, err := pipeOutput(cmd)
	if err != nil {
		b.loginProgress(login.ID, providers.LoginFailed, "", "failed to open stdout: "+err.Error())
		return
	}
	defer out.close()
	if err := out.start(cmd); err != nil {
		b.loginProgress(login.ID, providers.LoginFailed, "", "failed to start login command: "+err.Error())
		return
	}

	urlCh := make(chan string, 1)
	tail := newLineTail(40)
	out.scan(func(line string) {
		line = stripANSI(line)
		tail.add(line)
		if u := authURLPattern.FindString(line); u != "" {
			select {
			case urlCh <- u:
			default:
			}
		}
	})

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	// The loopback port is whatever the CLI put in its redirect_uri, so it
	// is read from the auth URL rather than assumed.
	loopback := ""
	reported := false

	pollTicker := time.NewTicker(2 * time.Second)
	defer pollTicker.Stop()
	announce := time.NewTimer(20 * time.Second)
	defer announce.Stop()

	replayed := false
	for {
		select {
		case err := <-done:
			if err != nil {
				out.drain(ctx)
			}
			b.finishLogin(ctx, login, err, ctx.Err(), tail)
			return
		case authURL := <-urlCh:
			if reported {
				continue
			}
			loopback = loopbackBaseFrom(authURL)
			b.loginProgressKind(login.ID, providers.LoginAwaitingCode, authURL, loopbackDetail, providers.PasteKindURL)
			reported = true
		case <-announce.C:
			if !reported {
				b.loginProgressKind(login.ID, providers.LoginAwaitingCode, "",
					"The sign-in CLI has not printed a link yet. Output so far: "+tail.String(),
					providers.PasteKindURL)
				reported = true
			}
		case <-pollTicker.C:
			current, err := b.client.GetLoginFull(login.ID)
			if err != nil {
				continue
			}
			if current.Status == providers.LoginCancelled {
				killTree(cmd)
				<-done
				return
			}
			if replayed || strings.TrimSpace(current.Code) == "" {
				continue
			}
			replayed = true
			if err := replayLoopbackCallback(loopback, current.Code); err != nil {
				// The paste was unusable. Say so and let the member try
				// again rather than failing the whole request.
				replayed = false
				b.loginProgressKind(login.ID, providers.LoginAwaitingCode, "", err.Error(), providers.PasteKindURL)
			}
		case <-ctx.Done():
			killTree(cmd)
			<-done
			b.loginProgress(login.ID, providers.LoginFailed, "", "sign-in timed out after 10 minutes")
			return
		}
	}
}

// defaultLoopbackBase is used when the CLI's auth URL carries no usable
// redirect_uri to read a port from.
const defaultLoopbackBase = "http://127.0.0.1:1455"

// loopbackBaseFrom extracts the scheme://host:port the CLI is listening on
// from the redirect_uri embedded in its auth URL.
func loopbackBaseFrom(authURL string) string {
	parsed, err := url.Parse(authURL)
	if err != nil {
		return defaultLoopbackBase
	}
	redirect := parsed.Query().Get("redirect_uri")
	if redirect == "" {
		return defaultLoopbackBase
	}
	r, err := url.Parse(redirect)
	if err != nil || r.Host == "" {
		return defaultLoopbackBase
	}
	return "http://" + r.Host
}

// replayLoopbackCallback re-issues the member's failed redirect against the
// CLI's own loopback listener.
//
// Only the path and query of the pasted value are used, and they are always
// sent to the loopback base the CLI itself advertised: a paste that names
// some other host cannot make the runner fetch it.
func replayLoopbackCallback(loopbackBase, pasted string) error {
	if loopbackBase == "" {
		loopbackBase = defaultLoopbackBase
	}
	pasted = strings.TrimSpace(pasted)
	parsed, err := url.Parse(pasted)
	if err != nil || parsed.Path == "" || parsed.RawQuery == "" {
		return fmt.Errorf("that does not look like the redirected address. %s", loopbackDetail)
	}
	if parsed.Query().Get("code") == "" {
		return fmt.Errorf("the address you pasted carries no authorization code. %s", loopbackDetail)
	}
	target := loopbackBase + parsed.Path + "?" + parsed.RawQuery

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		return fmt.Errorf("the runner could not complete the sign-in locally: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("the sign-in service rejected that address (status %d). %s", resp.StatusCode, loopbackDetail)
	}
	return nil
}
