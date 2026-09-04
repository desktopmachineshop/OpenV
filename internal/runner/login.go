package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// loginTimeout bounds one CLI sign-in attempt.
const loginTimeout = 10 * time.Minute

var authURLPattern = regexp.MustCompile(`https://[^\s"'\)\]]+`)

// loginFlow describes how to drive one provider CLI's sign-in.
type loginFlow struct {
	command []string
	env     []string // extra KEY=VALUE entries
	// pasteBack is true when the CLI prints a URL and waits for an
	// authorization code on stdin.
	pasteBack bool
	// interactive is true for TUI CLIs that need a real terminal: the
	// worker opens them in a visible console window on its host instead
	// of driving them over pipes. On a headless runner there is no console,
	// so the same flow runs over a pseudo-terminal instead.
	interactive bool
	// loopback is true for CLIs that complete their OAuth against a local
	// port on the machine running them. On a headless runner the member's
	// browser cannot reach that port, so the redirect is relayed back and
	// replayed locally.
	loopback bool
	// browserDetail is shown when the CLI opens the browser itself.
	browserDetail string
}

func flowFor(provider string) (loginFlow, bool) {
	switch provider {
	case providers.ProviderClaudeCode:
		// claude's login is an Ink TUI: it renders nothing over pipes, so
		// it must run in a real terminal on the worker host.
		return loginFlow{
			command:     []string{"claude", "setup-token"},
			interactive: true,
		}, true
	case providers.ProviderCodexCLI:
		return loginFlow{
			command:       []string{"codex", "login"},
			loopback:      true,
			browserDetail: "A sign-in page should have opened in a browser on the machine running agentd. Complete sign-in there; this page updates automatically.",
		}, true
	case providers.ProviderGeminiCLI:
		return loginFlow{
			command:   []string{"gemini", "-p", "Reply with OK."},
			env:       []string{"NO_BROWSER=1"},
			pasteBack: true,
		}, true
	}
	return loginFlow{}, false
}

// loginLoop polls for pending sign-in requests and executes them one at a
// time (CLI credential stores don't like concurrent logins).
func (w *Worker) loginLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			login, err := w.client.ClaimLogin()
			if err != nil {
				log.Printf("login claim failed: %v", err)
				continue
			}
			if login == nil {
				continue
			}
			w.handleLogin(ctx, login)
		}
	}
}

func (w *Worker) handleLogin(ctx context.Context, login *providers.LoginRequest) {
	log.Printf("login %s: starting %s sign-in", login.ID, login.Provider)
	flow, ok := flowFor(login.Provider)
	if !ok {
		w.loginProgress(login.ID, providers.LoginFailed, "", "provider does not support CLI sign-in")
		return
	}
	if _, err := exec.LookPath(flow.command[0]); err != nil {
		w.loginProgress(login.ID, providers.LoginFailed, "",
			fmt.Sprintf("the %q CLI is not installed on the worker host — install it first, then retry", flow.command[0]))
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	// A headless runner (a transient runner in a container) has neither a
	// console to open nor a browser to catch a redirect, so both of those
	// flows are relayed to the member's own browser instead.
	if flow.interactive {
		if w.headless {
			w.handlePTYLogin(runCtx, login, flow)
		} else {
			w.handleInteractiveLogin(runCtx, login, flow)
		}
		return
	}
	if flow.loopback && w.headless {
		w.handleLoopbackLogin(runCtx, login, flow)
		return
	}

	cmd := exec.CommandContext(runCtx, flow.command[0], flow.command[1:]...)
	cmd.Env = append(os.Environ(), flow.env...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		w.loginProgress(login.ID, providers.LoginFailed, "", "failed to open stdin: "+err.Error())
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		w.loginProgress(login.ID, providers.LoginFailed, "", "failed to open stdout: "+err.Error())
		return
	}
	cmd.Stderr = cmd.Stdout // interleave; we only scan for URLs and keep a tail

	if err := cmd.Start(); err != nil {
		w.loginProgress(login.ID, providers.LoginFailed, "", "failed to start login command: "+err.Error())
		return
	}

	// Scan output for the auth URL; keep a tail for diagnostics.
	urlCh := make(chan string, 1)
	tail := newLineTail(30)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			tail.add(line)
			if url := authURLPattern.FindString(line); url != "" {
				select {
				case urlCh <- url:
				default:
				}
			}
		}
	}()

	status := providers.LoginURLReady
	detail := flow.browserDetail
	if flow.pasteBack {
		status = providers.LoginAwaitingCode
		detail = "Open the sign-in link, authorize, then paste the code you receive back here."
	}

	// Relay the URL when it appears (codex may not print one — the browser
	// detail still tells the user what's happening).
	urlReported := false
	select {
	case url := <-urlCh:
		w.loginProgress(login.ID, status, url, detail)
		urlReported = true
	case <-time.After(20 * time.Second):
		w.loginProgress(login.ID, status, "", detail)
	case <-runCtx.Done():
	}

	// Poll for a pasted code / cancellation while the process runs.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	codeSent := false
	pollTicker := time.NewTicker(2 * time.Second)
	defer pollTicker.Stop()
	for {
		select {
		case err := <-done:
			_ = stdin.Close()
			if runCtx.Err() == context.DeadlineExceeded {
				w.loginProgress(login.ID, providers.LoginFailed, "", "sign-in timed out after 10 minutes")
				return
			}
			if err != nil {
				w.loginProgress(login.ID, providers.LoginFailed, "",
					"sign-in command failed: "+err.Error()+" — output tail: "+tail.String())
				return
			}
			w.loginProgress(login.ID, providers.LoginCompleted, "", "Signed in successfully.")
			w.redetect(ctx, login.Provider)
			log.Printf("login %s: %s sign-in completed", login.ID, login.Provider)
			return
		case url := <-urlCh:
			if !urlReported {
				w.loginProgress(login.ID, status, url, detail)
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
			if flow.pasteBack && !codeSent && strings.TrimSpace(current.Code) != "" {
				codeSent = true
				if _, err := io.WriteString(stdin, strings.TrimSpace(current.Code)+"\n"); err != nil {
					log.Printf("login %s: writing code failed: %v", login.ID, err)
				}
				_ = stdin.Close()
			}
		case <-runCtx.Done():
			killTree(cmd)
			<-done
			w.loginProgress(login.ID, providers.LoginFailed, "", "sign-in timed out after 10 minutes")
			return
		}
	}
}

// handleInteractiveLogin runs a TUI CLI's sign-in in a visible console
// window on the worker host and reports completion when it exits.
func (w *Worker) handleInteractiveLogin(ctx context.Context, login *providers.LoginRequest, flow loginFlow) {
	cmd := exec.Command(flow.command[0], flow.command[1:]...)
	cmd.Env = append(os.Environ(), flow.env...)
	configureInteractiveConsole(cmd)

	if err := cmd.Start(); err != nil {
		w.loginProgress(login.ID, providers.LoginFailed, "", "failed to open the sign-in terminal: "+err.Error())
		return
	}
	w.loginProgress(login.ID, providers.LoginURLReady, "",
		"A sign-in terminal has opened on the machine running agentd — complete the sign-in there. This page updates automatically when it finishes.")

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	pollTicker := time.NewTicker(2 * time.Second)
	defer pollTicker.Stop()
	for {
		select {
		case err := <-done:
			if err != nil {
				w.loginProgress(login.ID, providers.LoginFailed, "",
					"the sign-in terminal closed without completing: "+err.Error())
				return
			}
			w.loginProgress(login.ID, providers.LoginCompleted, "", "Signed in successfully.")
			w.redetect(ctx, login.Provider)
			log.Printf("login %s: %s interactive sign-in completed", login.ID, login.Provider)
			return
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
		case <-ctx.Done():
			killTree(cmd)
			<-done
			w.loginProgress(login.ID, providers.LoginFailed, "", "sign-in timed out after 10 minutes")
			return
		}
	}
}

// redetect refreshes and reports the provider's availability after a login.
func (w *Worker) redetect(ctx context.Context, provider string) {
	adapter, ok := w.adapters[provider]
	if !ok {
		return
	}
	av := adapter.Detect(ctx)
	report := map[string]map[string]interface{}{
		provider: {
			"installed": av.Installed,
			"version":   av.Version,
			"logged_in": av.LoggedIn,
			"detail":    av.Detail,
		},
	}
	if err := w.client.ReportDetection(report); err != nil {
		log.Printf("post-login detection report failed: %v", err)
	}
	if av.Installed {
		w.addProvider(provider)
	}
}

func (w *Worker) loginProgress(id, status, authURL, detail string) {
	if err := w.client.LoginProgress(id, status, authURL, detail); err != nil {
		log.Printf("login %s: progress update failed: %v", id, err)
	}
}

// lineTail keeps the last n lines of output for diagnostics.
type lineTail struct {
	lines []string
	max   int
}

func newLineTail(max int) *lineTail {
	return &lineTail{max: max}
}

func (t *lineTail) add(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[len(t.lines)-t.max:]
	}
}

func (t *lineTail) String() string {
	return strings.Join(t.lines, " | ")
}
