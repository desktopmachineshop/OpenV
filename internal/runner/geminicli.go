package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// GeminiCLIAdapter runs Google's Gemini CLI in headless mode.
type GeminiCLIAdapter struct{}

// Name returns the provider name.
func (a *GeminiCLIAdapter) Name() string { return providers.ProviderGeminiCLI }

// Detect checks for the gemini binary.
func (a *GeminiCLIAdapter) Detect(ctx context.Context) Availability {
	if _, err := exec.LookPath("gemini"); err != nil {
		return Availability{Detail: "gemini binary not found on PATH"}
	}
	version, err := runVersion(ctx, "gemini", "--version")
	if err != nil {
		return Availability{Detail: "gemini --version failed: " + err.Error()}
	}
	return Availability{
		Installed: true,
		Version:   version,
		LoggedIn:  true,
		Detail:    "version check succeeded; login not verified",
	}
}

// Start launches a headless gemini run. Gemini's json output arrives as one
// final object, so events stream only at the end.
func (a *GeminiCLIAdapter) Start(ctx context.Context, spec RunSpec) (RunHandle, error) {
	settingsPath := filepath.Join(spec.WorkDir, ".gemini", "settings.json")
	if err := writeMCPConfig(settingsPath, spec.MCP); err != nil {
		return nil, err
	}

	prompt := spec.Prompt
	if spec.SystemPrompt != "" {
		// Headless gemini has no append-system-prompt flag; prefix instead.
		prompt = "System instructions:\n" + spec.SystemPrompt + "\n\nTask:\n" + spec.Prompt
	}

	// The prompt travels over stdin (piped input is the headless prompt) —
	// prompts can exceed the ~32K Windows command-line limit.
	args := []string{
		"--output-format", "json",
		"--approval-mode", "yolo",
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}

	return startProc(ctx, procConfig{
		Command:    "gemini",
		Args:       args,
		Dir:        spec.WorkDir,
		Env:        spec.Env,
		Stdin:      prompt,
		TimeoutSec: spec.TimeoutSec,
	}, &geminiParser{})
}

// geminiParser accumulates stdout and parses the single JSON object at exit.
type geminiParser struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (p *geminiParser) ParseLine(line string, emit func(RunEvent)) {
	p.mu.Lock()
	p.buf.WriteString(line)
	p.buf.WriteString("\n")
	p.mu.Unlock()
}

func (p *geminiParser) Result(exitCode int, stderrTail string) (Result, error) {
	p.mu.Lock()
	raw := strings.TrimSpace(p.buf.String())
	p.mu.Unlock()

	res := Result{ExitCode: exitCode}

	var out struct {
		Response string                 `json:"response"`
		Stats    map[string]interface{} `json:"stats"`
		Error    map[string]interface{} `json:"error"`
	}
	if raw != "" && json.Unmarshal([]byte(raw), &out) == nil {
		res.FinalText = out.Response
		res.TokensIn, res.TokensOut = geminiTokens(out.Stats)
		if out.Error != nil {
			msg, _ := out.Error["message"].(string)
			if msg == "" {
				msg = "gemini reported an error"
			}
			return res, errors.New(msg)
		}
	} else if raw != "" {
		// Not JSON; treat the raw output as the response.
		res.FinalText = raw
	}

	if exitCode != 0 {
		detail := strings.TrimSpace(stderrTail)
		if detail == "" {
			detail = "gemini exited with code " + strconv.Itoa(exitCode)
		}
		return res, errors.New(detail)
	}
	return res, nil
}

// geminiTokens sums prompt/candidate token counts across models in the
// stats blob, best-effort.
func geminiTokens(stats map[string]interface{}) (int64, int64) {
	models, _ := stats["models"].(map[string]interface{})
	var in, out int64
	for _, mAny := range models {
		m, _ := mAny.(map[string]interface{})
		tokens, _ := m["tokens"].(map[string]interface{})
		in += asInt64(tokens["prompt"])
		out += asInt64(tokens["candidates"])
	}
	return in, out
}
