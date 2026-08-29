package embeddings

import (
	"testing"
)

func TestProviderFromEnvDisabledByDefault(t *testing.T) {
	// No OPENV_EMBEDDING_* set (t.Setenv restores, but we assert the unset
	// default explicitly by clearing).
	t.Setenv("OPENV_EMBEDDING_API_KEY", "")
	p := ProviderFromEnv()
	if p.Enabled() {
		t.Fatal("provider should be disabled with no API key")
	}
	// A disabled provider embeds nothing without error.
	vecs, err := p.Embed([]string{"hello"})
	if err != nil {
		t.Fatalf("disabled Embed returned error: %v", err)
	}
	if vecs != nil {
		t.Errorf("disabled Embed returned %v, want nil", vecs)
	}
}

func TestProviderFromEnvDefaults(t *testing.T) {
	t.Setenv("OPENV_EMBEDDING_API_KEY", "sk-test")
	t.Setenv("OPENV_EMBEDDING_BASE_URL", "")
	t.Setenv("OPENV_EMBEDDING_MODEL", "")
	p := ProviderFromEnv()
	if !p.Enabled() {
		t.Fatal("provider should be enabled with an API key")
	}
	if p.Model() != DefaultModel {
		t.Errorf("model = %q, want default %q", p.Model(), DefaultModel)
	}
	if p.baseURL != "https://api.openai.com/v1" {
		t.Errorf("base URL default = %q", p.baseURL)
	}
}

func TestProviderFromEnvOverrides(t *testing.T) {
	t.Setenv("OPENV_EMBEDDING_API_KEY", "sk-test")
	t.Setenv("OPENV_EMBEDDING_BASE_URL", "http://localhost:1234/v1/")
	t.Setenv("OPENV_EMBEDDING_MODEL", "custom-model")
	p := ProviderFromEnv()
	if p.baseURL != "http://localhost:1234/v1" {
		t.Errorf("base URL = %q, want trailing slash trimmed", p.baseURL)
	}
	if p.Model() != "custom-model" {
		t.Errorf("model = %q", p.Model())
	}
}

func TestEmbeddableText(t *testing.T) {
	if got := EmbeddableText("T", "B"); got != "T\n\nB" {
		t.Errorf("EmbeddableText = %q", got)
	}
	if got := EmbeddableText("", "B"); got != "B" {
		t.Errorf("title-only fallback = %q", got)
	}
	if got := EmbeddableText("T", ""); got != "T" {
		t.Errorf("body-only fallback = %q", got)
	}
}
