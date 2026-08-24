package providers

import "testing"

func ids(models []Model) []string {
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.ID)
	}
	return out
}

func TestAvailableModelsUsesCatalog(t *testing.T) {
	got := AvailableModels(&ProviderSetting{Provider: ProviderClaudeCode})
	if len(got) == 0 {
		t.Fatal("expected catalog models for claude-code")
	}
	for _, m := range got {
		if m.Label == "" {
			t.Fatalf("model %q has no label", m.ID)
		}
	}
}

func TestAvailableModelsPrefersDetected(t *testing.T) {
	got := AvailableModels(&ProviderSetting{
		Provider: ProviderClaudeCode,
		LastDetected: map[string]interface{}{
			"models": []interface{}{
				"local-only-model",
				map[string]interface{}{"id": "sonnet", "label": "detected sonnet"},
			},
		},
	})
	if got[0].ID != "local-only-model" || got[1].ID != "sonnet" {
		t.Fatalf("detected models should lead, got %v", ids(got))
	}
	if got[1].Label != "detected sonnet" {
		t.Fatalf("detected label should win, got %q", got[1].Label)
	}
	seen := map[string]int{}
	for _, m := range got {
		seen[m.ID]++
		if seen[m.ID] > 1 {
			t.Fatalf("duplicate model %q in %v", m.ID, ids(got))
		}
	}
}

func TestAvailableModelsIncludesDefaultModel(t *testing.T) {
	got := AvailableModels(&ProviderSetting{
		Provider:     ProviderGeminiCLI,
		DefaultModel: "gemini-experimental",
	})
	last := got[len(got)-1]
	if last.ID != "gemini-experimental" || last.Label != "gemini-experimental" {
		t.Fatalf("configured default should be selectable, got %v", ids(got))
	}
}

func TestAvailableModelsUnknownProvider(t *testing.T) {
	if got := AvailableModels(&ProviderSetting{Provider: "nope"}); len(got) != 0 {
		t.Fatalf("expected no models for an unknown provider, got %v", ids(got))
	}
}

func TestCatalogModelsIsACopy(t *testing.T) {
	first := CatalogModels(ProviderClaudeCode)
	first[0] = Model{ID: "mutated"}
	if CatalogModels(ProviderClaudeCode)[0].ID == "mutated" {
		t.Fatal("CatalogModels leaked the package catalog")
	}
}
