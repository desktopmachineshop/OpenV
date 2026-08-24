package providers

// Model is one selectable model for a provider. ID is what gets passed to
// the CLI/SDK (`--model`); Label is what the UI shows.
type Model struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// modelCatalog is the built-in, offline model list per provider — the
// starting point the agent and provider settings forms populate from. Vendor
// line-ups move faster than releases do, so the UI always allows a custom
// model id, and anything a worker reports under last_detected["models"] is
// merged in ahead of the catalog (see AvailableModels).
var modelCatalog = map[string][]Model{
	// Claude Code accepts the short aliases as well as full model ids.
	ProviderClaudeCode: {
		{ID: "opus", Label: "opus (latest Opus)"},
		{ID: "sonnet", Label: "sonnet (latest Sonnet)"},
		{ID: "haiku", Label: "haiku (latest Haiku)"},
		{ID: "claude-opus-5", Label: "Claude Opus 5"},
		{ID: "claude-opus-4-8", Label: "Claude Opus 4.8"},
		{ID: "claude-sonnet-5", Label: "Claude Sonnet 5"},
		{ID: "claude-sonnet-4-6", Label: "Claude Sonnet 4.6"},
		{ID: "claude-haiku-4-5", Label: "Claude Haiku 4.5"},
	},
	ProviderAnthropicAPI: {
		{ID: "claude-opus-5", Label: "Claude Opus 5"},
		{ID: "claude-opus-4-8", Label: "Claude Opus 4.8"},
		{ID: "claude-opus-4-7", Label: "Claude Opus 4.7"},
		{ID: "claude-sonnet-5", Label: "Claude Sonnet 5"},
		{ID: "claude-sonnet-4-6", Label: "Claude Sonnet 4.6"},
		{ID: "claude-haiku-4-5", Label: "Claude Haiku 4.5"},
		{ID: "claude-fable-5", Label: "Claude Fable 5"},
	},
	ProviderCodexCLI: {
		{ID: "gpt-5.1-codex", Label: "GPT-5.1 Codex"},
		{ID: "gpt-5-codex", Label: "GPT-5 Codex"},
		{ID: "gpt-5.1", Label: "GPT-5.1"},
		{ID: "gpt-5", Label: "GPT-5"},
		{ID: "o3", Label: "o3"},
	},
	ProviderOpenAIAPI: {
		{ID: "gpt-5.1", Label: "GPT-5.1"},
		{ID: "gpt-5", Label: "GPT-5"},
		{ID: "gpt-5-mini", Label: "GPT-5 mini"},
		{ID: "gpt-4.1", Label: "GPT-4.1"},
		{ID: "o3", Label: "o3"},
	},
	ProviderGeminiCLI: {
		{ID: "gemini-2.5-pro", Label: "Gemini 2.5 Pro"},
		{ID: "gemini-2.5-flash", Label: "Gemini 2.5 Flash"},
		{ID: "gemini-2.0-flash", Label: "Gemini 2.0 Flash"},
	},
	ProviderGoogleAPI: {
		{ID: "gemini-2.5-pro", Label: "Gemini 2.5 Pro"},
		{ID: "gemini-2.5-flash", Label: "Gemini 2.5 Flash"},
		{ID: "gemini-2.5-flash-lite", Label: "Gemini 2.5 Flash Lite"},
		{ID: "gemini-2.0-flash", Label: "Gemini 2.0 Flash"},
	},
}

// CatalogModels returns the built-in model list for a provider, or nil for
// an unknown one.
func CatalogModels(provider string) []Model {
	catalog := modelCatalog[provider]
	out := make([]Model, len(catalog))
	copy(out, catalog)
	return out
}

// AvailableModels returns the models a setting's provider can be pointed at:
// anything the worker detected first, then the built-in catalog, with the
// configured default model added if it is not already in the list. IDs are
// de-duplicated, keeping the first (most specific) entry.
func AvailableModels(p *ProviderSetting) []Model {
	if p == nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]Model, 0, 12)
	add := func(m Model) {
		if m.ID == "" || seen[m.ID] {
			return
		}
		seen[m.ID] = true
		if m.Label == "" {
			m.Label = m.ID
		}
		out = append(out, m)
	}
	for _, m := range detectedModels(p.LastDetected) {
		add(m)
	}
	for _, m := range CatalogModels(p.Provider) {
		add(m)
	}
	add(Model{ID: p.DefaultModel})
	return out
}

// detectedModels reads the optional models entry off a detection blob. It
// accepts a list of ids ("claude-opus-5") or of objects ({"id":…,"label":…}),
// so a worker can report whatever its CLI can enumerate.
func detectedModels(detected map[string]interface{}) []Model {
	raw, _ := detected["models"].([]interface{})
	out := make([]Model, 0, len(raw))
	for _, entry := range raw {
		switch v := entry.(type) {
		case string:
			out = append(out, Model{ID: v, Label: v})
		case map[string]interface{}:
			id, _ := v["id"].(string)
			label, _ := v["label"].(string)
			out = append(out, Model{ID: id, Label: label})
		}
	}
	return out
}
