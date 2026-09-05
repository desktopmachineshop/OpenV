package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
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
//   - TUI logins (Claude Code's `claude setup-token`) render nothing over
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
func (w *Worker) handlePTYLogin(ctx context.Context, login *providers.LoginRequest, flow loginFlow) {
	if !ptySupported {
		w.loginProgress(login.ID, providers.LoginFailed, "",
			"this runner cannot open a terminal for the sign-in flow")
		return
	}
	cmd := exec.CommandContext(ctx, flow.command[0], flow.command[1:]...)
	cmd.Env = append(os.Environ(), flow.env...)
	// A TUI that believes it is on a dumb terminal prints nothing useful.
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")

	tty, err := startPTY(cmd)
	if err != nil {
		w.loginProgress(login.ID, providers.LoginFailed, "", "failed to open a terminal for the sign-in: "+err.Error())
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
	tail := newLineTail(40)
	urlReported := false
	nudged := false
	// sentCode is the code last typed into the terminal. A member who pastes
	// a different one is retrying, and gets another go — the CLI itself
	// offers a retry after a bad code, so refusing one here is what left the
	// flow stuck at "code sent".
	sentCode := ""
	reportedErr := ""

	pollTicker := time.NewTicker(2 * time.Second)
	defer pollTicker.Stop()
	nudge := time.NewTimer(ptyNudgeAfter)
	defer nudge.Stop()

	for {
		select {
		case err := <-done:
			w.finishLogin(ctx, login, err, ctx.Err(), tail)
			return
		case chunk, ok := <-chunks:
			if !ok {
				continue
			}
			screen.add(chunk)
			for _, line := range visibleLines(chunk) {
				tail.add(line)
			}
			if !urlReported {
				if authURL := authURLPattern.FindString(stripANSI(chunk)); authURL != "" {
					w.loginProgress(login.ID, providers.LoginAwaitingCode, authURL, pasteDetail)
					urlReported = true
				}
			}
			// Once a code has been typed, whatever the CLI says about it is
			// the member's answer: relay it instead of waiting for an exit
			// that a retry prompt will never produce.
			if sentCode != "" {
				if msg := tuiError(screen.String()); msg != "" && msg != reportedErr {
					reportedErr = msg
					log.Printf("login %s: the sign-in terminal reported: %s", login.ID, msg)
					w.loginProgress(login.ID, providers.LoginAwaitingCode, "",
						msg+" — paste the code again, in full, to retry.")
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
				w.loginProgress(login.ID, providers.LoginAwaitingCode, "",
					"The sign-in CLI has not printed a link yet. Output so far: "+tail.String())
				urlReported = true
			}
		case <-pollTicker.C:
			current, err := w.client.GetLoginFull(login.ID)
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
			reportedErr = ""
			screen.reset()
			if _, err := io.WriteString(tty, code+"\r"); err != nil {
				log.Printf("login %s: writing code to terminal failed: %v", login.ID, err)
				continue
			}
			log.Printf("login %s: code submitted to the sign-in terminal", login.ID)
			w.loginProgress(login.ID, providers.LoginAwaitingCode, "",
				"Code received — completing the sign-in on the runner…")
		case <-ctx.Done():
			killTree(cmd)
			<-done
			w.loginProgress(login.ID, providers.LoginFailed, "", "sign-in timed out after 10 minutes")
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

// finishLogin reports the terminal state of a sign-in the CLI has exited.
func (w *Worker) finishLogin(ctx context.Context, login *providers.LoginRequest, waitErr, ctxErr error, tail *lineTail) {
	if ctxErr == context.DeadlineExceeded {
		w.loginProgress(login.ID, providers.LoginFailed, "", "sign-in timed out after 10 minutes")
		return
	}
	if waitErr != nil {
		w.loginProgress(login.ID, providers.LoginFailed, "",
			"sign-in command failed: "+waitErr.Error()+" — output tail: "+tail.String())
		return
	}
	w.loginProgress(login.ID, providers.LoginCompleted, "", "Signed in successfully.")
	w.redetect(ctx, login.Provider)
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
func (w *Worker) handleLoopbackLogin(ctx context.Context, login *providers.LoginRequest, flow loginFlow) {
	cmd := exec.CommandContext(ctx, flow.command[0], flow.command[1:]...)
	cmd.Env = append(os.Environ(), flow.env...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		w.loginProgress(login.ID, providers.LoginFailed, "", "failed to open stdout: "+err.Error())
		return
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		w.loginProgress(login.ID, providers.LoginFailed, "", "failed to start login command: "+err.Error())
		return
	}

	urlCh := make(chan string, 1)
	tail := newLineTail(40)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := stripANSI(scanner.Text())
			tail.add(line)
			if u := authURLPattern.FindString(line); u != "" {
				select {
				case urlCh <- u:
				default:
				}
			}
		}
	}()

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
			w.finishLogin(ctx, login, err, ctx.Err(), tail)
			return
		case authURL := <-urlCh:
			if reported {
				continue
			}
			loopback = loopbackBaseFrom(authURL)
			w.loginProgress(login.ID, providers.LoginAwaitingCode, authURL, loopbackDetail)
			reported = true
		case <-announce.C:
			if !reported {
				w.loginProgress(login.ID, providers.LoginAwaitingCode, "",
					"The sign-in CLI has not printed a link yet. Output so far: "+tail.String())
				reported = true
			}
		case <-pollTicker.C:
			current, err := w.client.GetLoginFull(login.ID)
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
				w.loginProgress(login.ID, providers.LoginAwaitingCode, "", err.Error())
			}
		case <-ctx.Done():
			killTree(cmd)
			<-done
			w.loginProgress(login.ID, providers.LoginFailed, "", "sign-in timed out after 10 minutes")
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
