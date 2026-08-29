package embeddings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// HTTPProvider is an OpenAI-compatible embedding backend: it POSTs
// {"model":..., "input":[...]} to <baseURL>/embeddings and reads the
// {"data":[{"embedding":[...]}]} response. It works against the OpenAI API and
// any server that speaks the same shape (Azure OpenAI, a local text-embeddings
// server, etc.). A zero apiKey disables it.
type HTTPProvider struct {
	baseURL string // no trailing slash, e.g. https://api.openai.com/v1
	apiKey  string
	model   string
	http    *http.Client
}

// ProviderFromEnv builds an HTTPProvider from the OPENV_EMBEDDING_*
// environment. With OPENV_EMBEDDING_API_KEY unset the provider is DISABLED
// (Enabled()==false) and every Embed is a no-op error-free skip — the intended
// default for dev and any deployment that has not opted into embeddings, so
// semantic-search infra costs a default install nothing. This mirrors
// notify.MailerFromEnv.
//
// Recognized variables:
//
//	OPENV_EMBEDDING_API_KEY   API key (empty => embeddings disabled)
//	OPENV_EMBEDDING_BASE_URL  API base URL (default https://api.openai.com/v1)
//	OPENV_EMBEDDING_MODEL     model id (default text-embedding-3-small)
//	OPENV_EMBEDDING_PROVIDER  informational label only (e.g. "openai");
//	                          the wire protocol is always OpenAI-compatible
func ProviderFromEnv() *HTTPProvider {
	apiKey := strings.TrimSpace(os.Getenv("OPENV_EMBEDDING_API_KEY"))
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("OPENV_EMBEDDING_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := strings.TrimSpace(os.Getenv("OPENV_EMBEDDING_MODEL"))
	if model == "" {
		model = DefaultModel
	}
	p := &HTTPProvider{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
	if apiKey == "" {
		slog.Info("embeddings: OPENV_EMBEDDING_API_KEY unset; semantic-search embedding disabled (create/update and existing search are unaffected)")
	} else {
		slog.Info("embeddings: provider enabled", "base_url", baseURL, "model", model)
	}
	return p
}

// Enabled reports whether an API key is configured.
func (p *HTTPProvider) Enabled() bool { return p != nil && p.apiKey != "" }

// Model returns the configured embedding model id.
func (p *HTTPProvider) Model() string {
	if p == nil {
		return ""
	}
	return p.model
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Embed returns one vector per input string, in request order. A disabled
// provider returns (nil, nil): callers treat that as "skip", not an error.
func (p *HTTPProvider) Embed(texts []string) ([][]float32, error) {
	if !p.Enabled() {
		return nil, nil
	}
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	payload, err := json.Marshal(embedRequest{Model: p.model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, p.baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings: provider returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed embedResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("embeddings: decode response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf("embeddings: provider error: %s", parsed.Error.Message)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings: provider returned %d vectors for %d inputs", len(parsed.Data), len(texts))
	}
	out := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		out[i] = d.Embedding
	}
	return out, nil
}
