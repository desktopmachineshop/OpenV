package runner

import (
	"context"
	"errors"
	"os/exec"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

// Google AI Studio (provider google-api) is the Gemini API on a key made in
// AI Studio. It has no sign-in: the server sends the workspace's key with
// every claim (providers.AlwaysAPIKey), and this adapter runs the Gemini
// CLI's headless engine pinned to that key — the agent loop, the OpenV MCP
// wiring and the tool confinement are gemini-cli's, unchanged.
//
// What differs from gemini-cli is only authentication, and it is closed in
// both directions: the isolated settings file names the API-key auth type,
// which outranks the member's own settings, and the environment blanks the
// variables that would select a Google-account or Vertex AI sign-in. A run
// with no key is refused at Start rather than left to find another account.

// geminiAPIKeyAuthType is the Gemini CLI's name for API-key auth in
// security.auth.selectedAuthType.
const geminiAPIKeyAuthType = "gemini-api-key"

// aiStudioKeyEnv is the variable an AI Studio key reaches the CLI in.
const aiStudioKeyEnv = "GEMINI_API_KEY"

// aiStudioSignInEnvKeys select an auth mode other than an API key. The CLI
// reads each as == "true", so an empty value turns it off whatever the
// runner host has set.
var aiStudioSignInEnvKeys = []string{geminiOAuthEnv, "GOOGLE_GENAI_USE_VERTEXAI", "GEMINI_CLI_USE_COMPUTE_ADC"}

// errAIStudioNoKey refuses a Google AI Studio run that has no key to run on.
var errAIStudioNoKey = errors.New("Google AI Studio runs on an API key, and none reached this run: " +
	"paste a Gemini API key from AI Studio in Workspace settings → AI providers, or set GEMINI_API_KEY on the runner host")

// GoogleAIStudioAdapter runs agents on Google AI Studio through the Gemini CLI.
type GoogleAIStudioAdapter struct{}

// Name returns the provider name.
func (a *GoogleAIStudioAdapter) Name() string { return providers.ProviderGoogleAPI }

// Detect checks for the gemini binary. There is no sign-in to look for: the
// key comes with each claim (or from the host's GEMINI_API_KEY, which the
// worker reads when the claim carries none).
func (a *GoogleAIStudioAdapter) Detect(ctx context.Context) Availability {
	if _, err := exec.LookPath("gemini"); err != nil {
		return Availability{Detail: "gemini binary not found on PATH (Google AI Studio runs through the Gemini CLI)"}
	}
	version, err := runVersion(ctx, "gemini", "--version")
	if err != nil {
		return Availability{Detail: "gemini --version failed: " + err.Error()}
	}
	return Availability{Installed: true, Version: version,
		Detail: "runs on the workspace's Google AI Studio key, sent with each run"}
}

// Start launches a headless gemini run on the AI Studio key.
func (a *GoogleAIStudioAdapter) Start(ctx context.Context, spec RunSpec) (RunHandle, error) {
	if aiStudioKey(spec) == "" {
		return nil, agentPolicyError(errAIStudioNoKey.Error())
	}
	return startGemini(ctx, spec, providers.ProviderGoogleAPI, aiStudioRunEnv)
}

// aiStudioKey is the key the run carries: the workspace's, or the runner
// host's, which Worker.runEnv puts in the run's environment.
func aiStudioKey(spec RunSpec) string {
	return mergedProcEnv(spec)[aiStudioKeyEnv]
}

// aiStudioRunEnv is the environment of one Google AI Studio run: the
// isolated settings file, with every auth mode but the key off.
func aiStudioRunEnv(spec RunSpec, settingsPath string) map[string]string {
	env := mergedProcEnv(spec)
	env["GEMINI_CLI_SYSTEM_SETTINGS_PATH"] = settingsPath
	for _, key := range aiStudioSignInEnvKeys {
		env[key] = ""
	}
	return env
}
