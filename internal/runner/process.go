package runner

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

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
	Command    string
	Args       []string
	Dir        string
	Env        map[string]string
	TimeoutSec int
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
			stderrMu.Lock()
			stderrLines = append(stderrLines, sc.Text())
			if len(stderrLines) > 40 {
				stderrLines = stderrLines[len(stderrLines)-40:]
			}
			stderrMu.Unlock()
		}
	}()

	// Stdout pump.
	stdoutDone := make(chan struct{})
	go func() {
		defer close(stdoutDone)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}
			parser.ParseLine(line, func(ev RunEvent) {
				select {
				case h.events <- ev:
				case <-time.After(5 * time.Second):
					// Drop rather than deadlock if nobody is draining.
				}
			})
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
			resErr = context.DeadlineExceeded
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
