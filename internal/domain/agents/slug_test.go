package agents

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidSlug(t *testing.T) {
	valid := []string{
		"requirements-copilot", // seeded convention
		"chief-of-staff",
		"a",
		"x9",
		"agent-2",
		"007",
		strings.Repeat("a", 128), // at the length cap
	}
	for _, s := range valid {
		if !ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = false, want true", s)
		}
	}

	invalid := []string{
		"",
		"..",
		"../evil",
		"..\\evil",
		"foo/bar",
		"foo\\bar",
		"/etc/passwd",
		"foo.md",
		".hidden",
		"-leading-hyphen",
		"Foo",        // uppercase
		"foo_bar",    // underscore
		"foo bar",    // space
		"café",       // unicode letter
		"агент",      // cyrillic
		"foo\x00bar", // NUL
		"foo\nbar",
		strings.Repeat("a", 129), // over the length cap
	}
	for _, s := range invalid {
		if ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = true, want false", s)
		}
	}
}

func TestValidateRejectsBadSlug(t *testing.T) {
	def := &Definition{Slug: "../escape", Name: "Evil", Provider: "claude-code"}
	if err := def.Validate(); err == nil {
		t.Error("Validate accepted a path-traversal slug")
	}
	def.Slug = "requirements-copilot"
	if err := def.Validate(); err != nil {
		t.Errorf("Validate rejected a conforming slug: %v", err)
	}
}

func TestFilePathStaysUnderOrgDir(t *testing.T) {
	svc, err := NewFileService(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewFileService failed: %v", err)
	}

	path, err := svc.filePath("org1", "requirements-copilot")
	if err != nil {
		t.Fatalf("filePath rejected a valid slug: %v", err)
	}
	wantDir := filepath.Clean(svc.orgDir("org1"))
	if filepath.Dir(path) != wantDir {
		t.Errorf("filePath escaped org dir: got %q, want parent %q", path, wantDir)
	}

	for _, slug := range []string{"../../etc/cron.d/evil", "..", "a/b", "a\\b", ".trash", ""} {
		if p, err := svc.filePath("org1", slug); err == nil {
			t.Errorf("filePath(%q) = %q, want error", slug, p)
		}
	}
}
