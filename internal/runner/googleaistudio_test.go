package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/providers"
)

// The registry serves google-api, so a Google AI Studio agent no longer
// fails "no adapter for provider".
func TestRegistryServesGoogleAIStudio(t *testing.T) {
	for _, a := range Registry() {
		if a.Name() == providers.ProviderGoogleAPI {
			return
		}
	}
	t.Fatal("no adapter in Registry() for google-api")
}

// A Google AI Studio run's settings pin API-key auth, which outranks the
// member's own settings file.
func TestAIStudioSettingsPinAPIKeyAuth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gemini-settings.json")
	spec := geminiSpec()
	if err := writeGeminiSettings(path, spec.MCP, geminiApprovalMode(spec), spec.AllowedTools, geminiAPIKeyAuthType); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Security struct {
			Auth struct {
				SelectedAuthType string `json:"selectedAuthType"`
			} `json:"auth"`
		} `json:"security"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Security.Auth.SelectedAuthType != "gemini-api-key" {
		t.Errorf("selectedAuthType = %q, want gemini-api-key", cfg.Security.Auth.SelectedAuthType)
	}

	// gemini-cli's own settings name no auth type: a member's choice stands.
	if err := writeGeminiSettings(path, spec.MCP, geminiApprovalMode(spec), spec.AllowedTools, ""); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), "selectedAuthType") {
		t.Errorf("gemini-cli settings name an auth type: %s", raw)
	}
}

// The environment keeps the run's key and turns every Google sign-in off,
// so a host set up for Code Assist or Vertex AI cannot take the run.
func TestAIStudioRunEnvTurnsSignInsOff(t *testing.T) {
	t.Setenv("GOOGLE_GENAI_USE_GCA", "true")
	t.Setenv("GOOGLE_GENAI_USE_VERTEXAI", "true")
	spec := geminiSpec()
	spec.MCP.Env["GEMINI_API_KEY"] = "ai-studio-key"
	env := aiStudioRunEnv(spec, "/work/.openv/gemini-settings.json")
	if env["GEMINI_API_KEY"] != "ai-studio-key" {
		t.Errorf("GEMINI_API_KEY = %q, want the run's key", env["GEMINI_API_KEY"])
	}
	for _, key := range aiStudioSignInEnvKeys {
		if v, ok := env[key]; !ok || v != "" {
			t.Errorf("%s = %q (set %v), want it set empty", key, v, ok)
		}
	}
	if env["GEMINI_CLI_SYSTEM_SETTINGS_PATH"] != "/work/.openv/gemini-settings.json" {
		t.Errorf("settings path = %q", env["GEMINI_CLI_SYSTEM_SETTINGS_PATH"])
	}
}

// With no key in the run or on the host, Start refuses as a policy failure
// rather than starting a CLI that would look for a sign-in.
func TestAIStudioStartRefusesWithNoKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	spec := geminiSpec()
	spec.WorkDir = t.TempDir()
	_, err := (&GoogleAIStudioAdapter{}).Start(context.Background(), spec)
	if err == nil || !errors.Is(err, ErrAgentPolicy) || !strings.Contains(err.Error(), "AI Studio") {
		t.Fatalf("Start with no key: err = %v, want an agent-policy refusal naming AI Studio", err)
	}
}

// A Google AI Studio run is on api-key auth even when the claim says
// otherwise (a server older than the provider): the host's key serves, and
// with none the run fails naming AI Studio.
func TestRunEnvPutsGoogleAIStudioOnItsKey(t *testing.T) {
	api := newFakeAPI(t, nil)
	w := NewWorker(NewClient(api.URL(), "worker-key"), Options{WorkerID: "w-env", WorkspaceBase: t.TempDir()})
	claim := func(id string, auth *RunAuth) *ClaimResponse {
		return &ClaimResponse{Run: &agentruns.Run{ID: id}, Agent: &agents.Agent{Provider: providers.ProviderGoogleAPI},
			RunToken: "rt-" + id, Auth: auth}
	}

	env, ok := w.runEnv(claim("stored", &RunAuth{Mode: "api-key", APIKeyEnv: "GEMINI_API_KEY", APIKey: "pasted"}))
	if !ok || env["GEMINI_API_KEY"] != "pasted" {
		t.Fatalf("stored key: env = %v, ok = %v", env, ok)
	}

	t.Setenv("GEMINI_API_KEY", "host-key")
	env, ok = w.runEnv(claim("old-server", &RunAuth{Mode: "user-account"}))
	if !ok || env["GEMINI_API_KEY"] != "host-key" {
		t.Fatalf("user-account claim: env = %v, ok = %v; want the host's key", env, ok)
	}

	t.Setenv("GEMINI_API_KEY", "")
	if _, ok := w.runEnv(claim("no-key", nil)); ok {
		t.Fatal("a run with no key anywhere was given an environment")
	}
	finishes := api.match(func(c apiCall) bool { return c.is("POST", "/agent-runs/no-key/finish") })
	if len(finishes) != 1 || !strings.Contains(string(finishes[0].Body), "Google AI Studio runs on an API key") {
		t.Errorf("no-key finish = %s, want one naming Google AI Studio", describeCalls(finishes))
	}
}
