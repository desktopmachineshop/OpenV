package config

import (
	"os"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/embeddings"
)

// The embedding provider's settings tests, which internal/domain/embeddings
// held while embeddings.ProviderFromEnv read OPENV_EMBEDDING_BASE_URL and
// OPENV_EMBEDDING_MODEL (refactor step X10b moved those reads here, to
// Embeddings).

func TestEmbeddingsDefaults(t *testing.T) {
	quietLog(t)
	t.Setenv("OPENV_EMBEDDING_API_KEY", "sk-test")
	t.Setenv("OPENV_EMBEDDING_BASE_URL", "")
	t.Setenv("OPENV_EMBEDDING_MODEL", "")
	e := Load(os.LookupEnv).Embeddings()
	if e.Model != embeddings.DefaultModel {
		t.Errorf("model = %q, want default %q", e.Model, embeddings.DefaultModel)
	}
	if e.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("base URL default = %q", e.BaseURL)
	}
}

func TestEmbeddingsOverrides(t *testing.T) {
	quietLog(t)
	t.Setenv("OPENV_EMBEDDING_BASE_URL", "http://localhost:1234/v1/")
	t.Setenv("OPENV_EMBEDDING_MODEL", "custom-model")
	e := Load(os.LookupEnv).Embeddings()
	if e.BaseURL != "http://localhost:1234/v1" {
		t.Errorf("base URL = %q, want trailing slash trimmed", e.BaseURL)
	}
	if e.Model != "custom-model" {
		t.Errorf("model = %q", e.Model)
	}
}
