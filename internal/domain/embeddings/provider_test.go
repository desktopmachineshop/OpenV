package embeddings

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProviderFromEnvDisabledByDefault(t *testing.T) {
	// No API key: cmd/server passes OPENV_EMBEDDING_API_KEY's value, empty
	// when it is unset. A key of only spaces is no key either, as it always
	// was, though a key is otherwise used exactly as set.
	for _, key := range []string{"", "   ", "\n"} {
		if ProviderFromEnv(key).Enabled() {
			t.Fatalf("provider should be disabled with the API key %q", key)
		}
	}
	p := ProviderFromEnv("")
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
	t.Setenv("OPENV_EMBEDDING_BASE_URL", "")
	t.Setenv("OPENV_EMBEDDING_MODEL", "")
	p := ProviderFromEnv("sk-test")
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
	t.Setenv("OPENV_EMBEDDING_BASE_URL", "http://localhost:1234/v1/")
	t.Setenv("OPENV_EMBEDDING_MODEL", "custom-model")
	p := ProviderFromEnv("sk-test")
	if p.baseURL != "http://localhost:1234/v1" {
		t.Errorf("base URL = %q, want trailing slash trimmed", p.baseURL)
	}
	if p.Model() != "custom-model" {
		t.Errorf("model = %q", p.Model())
	}
}

// TestProviderSendsTheKeyExactlyAsSet: the API key is a credential, used
// exactly as set (#379, question 24), so a key with a space in front reaches
// the provider with it, where the provider used to trim it off.
func TestProviderSendsTheKeyExactlyAsSet(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.5]}]}`))
	}))
	defer srv.Close()
	t.Setenv("OPENV_EMBEDDING_BASE_URL", srv.URL)
	t.Setenv("OPENV_EMBEDDING_MODEL", "")
	p := ProviderFromEnv(" sk-test")
	if !p.Enabled() {
		t.Fatal("provider should be enabled with an API key")
	}
	if _, err := p.Embed([]string{"hello"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if got != "Bearer  sk-test" {
		t.Errorf("Authorization = %q, want %q: the key exactly as set", got, "Bearer  sk-test")
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
