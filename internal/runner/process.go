package runner

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// timeoutError marks a run the watchdog killed for exceeding its deadline
// while preserving the stream parser's underlying error for diagnostics. It
// unwraps to context.DeadlineExceeded so callers can still errors.Is on the
// timeout, and exposes detail for the human-readable reason.
type timeoutError struct{ detail error }

func (e *timeoutError) Error() string {
	if e.detail != nil {
		return "run timed out: " + e.detail.Error()
	}
	return "run timed out"
}

func (e *timeoutError) Unwrap() error { return context.DeadlineExceeded }

// eventsDroppedMarker is a synthetic event surfacing that the stdout pump
// stalled and dropped n parsed events, so the truncation is visible in the
// run log instead of silently vanishing.
func eventsDroppedMarker(n int) RunEvent {
	return RunEvent{
		Kind: agentruns.LogMarker,
		Payload: map[string]interface{}{
			"marker":  "events_dropped",
			"dropped": n,
			"message": fmt.Sprintf("%d log event(s) dropped: the worker's stdout pump stalled", n),
		},
	}
}

// streamParser turns a subprocess's stdout lines into run events and, once
// the process exits, into a Result.
type streamParser interface {
	// ParseLine handles one stdout line, emitting zero or more events.
	ParseLine(line string, emit func(RunEvent))
	// Result builds the terminal result after the process exited.
	Result(exitCode int, stderrTail string) (Result, error)
}

// procConfig describes the subprocess to launch.
type procConfig struct {
	Command string
	Args    []string
	Dir     string
	Env     map[string]string
	// Stdin, when non-empty, is piped to the process. Large payloads (like
	// run prompts) must travel this way: Windows caps a command line at
	// ~32K characters, so big argv values fail with "filename or extension
	// is too long".
	Stdin      string
	TimeoutSec int
	// EmitStderr, when true, also surfaces each non-empty stderr line as a log
	// event (in addition to keeping the diagnostic tail). Adapters whose stdout
	// streams nothing until the end (gemini) use this so a run isn't silent and
	// failures stay visible in the live log.
	EmitStderr bool
}

// procHandle is the shared RunHandle implementation for CLI adapters.
type procHandle struct {
	cmd    *exec.Cmd
	events chan RunEvent
	done   chan struct{}

	mu       sync.Mutex
	result   Result
	err      error
	timedOut bool

	cancelOnce sync.Once
	cancelCh   chan struct{}
}

// startProc launches the subprocess and wires its stdout through the parser.
func startProc(ctx context.Context, cfg procConfig, parser streamParser) (*procHandle, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Dir = cfg.Dir
	env := os.Environ()
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	if cfg.Stdin != "" {
		cmd.Stdin = strings.NewReader(cfg.Stdin)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	h := &procHandle{
		cmd:      cmd,
		events:   make(chan RunEvent, 256),
		done:     make(chan struct{}),
		cancelCh: make(chan struct{}),
	}

	// Stderr tail for diagnostics.
	var stderrMu sync.Mutex
	var stderrLines []string
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			stderrMu.Lock()
			stderrLines = append(stderrLines, line)
			if len(stderrLines) > 40 {
				stderrLines = stderrLines[len(stderrLines)-40:]
			}
			stderrMu.Unlock()
			if cfg.EmitStderr && strings.TrimSpace(line) != "" {
				ev := RunEvent{Kind: agentruns.LogText, Payload: map[string]interface{}{
					"text":   line,
					"stderr": true,
				}}
				select {
				case h.events <- ev:
				case <-time.After(5 * time.Second):
					// Drop rather than deadlock if nobody is draining.
				}
			}
		}
	}()

	// Stdout pump.
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
		dropped := 0
		emit := func(ev RunEvent) {
			// If earlier events were dropped, surface a marker as soon as the
			// channel drains again so the gap is visible downstream.
			if dropped > 0 {
				select {
				case h.events <- eventsDroppedMarker(dropped):
					dropped = 0
				default:
				}
			}
			select {
			case h.events <- ev:
			case <-time.After(5 * time.Second):
				// Drop rather than deadlock if nobody is draining, but count
				// it so the truncation is reported, not silent.
				dropped++
				log.Printf("stdout pump stalled, dropped event (kind=%s, dropped so far=%d)", ev.Kind, dropped)
			}
		}
		for sc.Scan() {
			line := sc.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}
			parser.ParseLine(line, emit)
		}
		// Stream ended while still behind: try once more to record the gap.
		if dropped > 0 {
			select {
			case h.events <- eventsDroppedMarker(dropped):
			case <-time.After(5 * time.Second):
				log.Printf("%d dropped event(s) could not be reported", dropped)
			}
		}
	}()

	// Watchdog: cancel, ctx, timeout.
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		var timeout <-chan time.Time
		if cfg.TimeoutSec > 0 {
			t := time.NewTimer(time.Duration(cfg.TimeoutSec) * time.Second)
			defer t.Stop()
			timeout = t.C
		}
		select {
		case <-h.done:
		case <-h.cancelCh:
			killTree(cmd)
		case <-ctx.Done():
			h.mu.Lock()
			h.timedOut = ctx.Err() == context.DeadlineExceeded
			h.mu.Unlock()
			killTree(cmd)
		case <-timeout:
			h.mu.Lock()
			h.timedOut = true
			h.mu.Unlock()
			killTree(cmd)
		}
	}()

	go func() {
		<-stdoutDone
		<-stderrDone
		waitErr := cmd.Wait()
		exitCode := 0
		if waitErr != nil {
			if ee, ok := waitErr.(*exec.ExitError); ok {
				exitCode = ee.ExitCode()
			} else {
				exitCode = -1
			}
		}
		stderrMu.Lock()
		tail := strings.Join(stderrLines, "\n")
		stderrMu.Unlock()

		result, resErr := parser.Result(exitCode, tail)
		result.ExitCode = exitCode

		h.mu.Lock()
		if h.timedOut {
			// Wrap rather than replace: the run timed out, but keep the
			// parser's underlying error (if any) for diagnostics. timeoutError
			// still unwraps to context.DeadlineExceeded.
			resErr = &timeoutError{detail: resErr}
		}
		h.result = result
		h.err = resErr
		h.mu.Unlock()

		close(h.events)
		close(h.done)
	}()

	return h, nil
}

func (h *procHandle) Events() <-chan RunEvent { return h.events }

func (h *procHandle) Wait() (Result, error) {
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.result, h.err
}

func (h *procHandle) Cancel() {
	h.cancelOnce.Do(func() { close(h.cancelCh) })
}

// killTree terminates the process and its children. Windows uses taskkill;
// elsewhere we signal the process and hard-kill after a grace period.
func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	go func() {
		time.Sleep(5 * time.Second)
		_ = cmd.Process.Kill()
	}()
}

// runVersion runs a `<bin> --version`-style command with a short timeout and
// returns its trimmed combined output.
func runVersion(ctx context.Context, bin string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, bin, args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text != "" {
			return text, fmt.Errorf("%s %s: %v: %s", bin, strings.Join(args, " "), err, text)
		}
		return "", err
	}
	return text, nil
}
