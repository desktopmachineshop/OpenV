package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeExample drops a project.json into its own subdirectory, the layout
// LoadFileBasedTemplates scans.
func writeExample(t *testing.T, dir, name string, body interface{}) {
	t.Helper()
	sub := filepath.Join(dir, name)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "project.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLoadFileBasedTemplatesSkipsPlainExports locks in the picker contract:
// only real templates (key + name) are offered. A plain project export —
// examples/ also holds those for Import Project — used to parse cleanly into
// an empty TemplateData and render as a blank, unusable row.
func TestLoadFileBasedTemplatesSkipsPlainExports(t *testing.T) {
	dir := t.TempDir()

	writeExample(t, dir, "real-template", TemplateData{
		Key:  "example-thing",
		Name: "Example Thing",
	})
	// Project-export shape: no key/name fields at all.
	writeExample(t, dir, "plain-export", map[string]interface{}{
		"project_id":   "d4271c70-b970-418e-bd15-32a30dd8d722",
		"project_name": "Some Exported Project",
		"artifacts":    []interface{}{},
	})
	// Half-populated entries are not offerable either.
	writeExample(t, dir, "keyless", TemplateData{Name: "No Key"})
	writeExample(t, dir, "nameless", TemplateData{Key: "no-name"})

	got, err := LoadFileBasedTemplates(dir)
	if err != nil {
		t.Fatalf("LoadFileBasedTemplates: %v", err)
	}
	if len(got) != 1 {
		names := make([]string, len(got))
		for i, s := range got {
			names[i] = s.Key + "/" + s.Name
		}
		t.Fatalf("got %d templates %v, want only the real one", len(got), names)
	}
	if got[0].Key != "example-thing" || got[0].Name != "Example Thing" {
		t.Errorf("got %+v, want the example-thing template", got[0])
	}

	// An empty keyOrID must not resolve to a skipped export's snapshot.
	if _, err := GetFileBasedTemplateSnapshot(dir, ""); err == nil {
		t.Error("GetFileBasedTemplateSnapshot(\"\") succeeded, want not-found")
	}
}

// TestDefaultTemplatesExcludeRetiredCncMill locks in that the Desktop CNC Mill
// example is no longer seeded as a bundled default (migration 0020 removes it
// from databases seeded before it was retired).
func TestDefaultTemplatesExcludeRetiredCncMill(t *testing.T) {
	defaults, err := DefaultTemplates()
	if err != nil {
		t.Fatalf("DefaultTemplates: %v", err)
	}
	if len(defaults) == 0 {
		t.Fatal("no default templates returned")
	}
	for _, tpl := range defaults {
		if tpl.Key == RetiredCncMillTemplateKey {
			t.Errorf("retired template %q is still seeded as a default", tpl.Key)
		}
		if tpl.Key == "" || tpl.Name == "" || len(tpl.Snapshot) == 0 {
			t.Errorf("default template %+v is missing key, name, or snapshot", tpl)
		}
	}
}
