package mcp

import (
	"strings"
	"testing"
)

// The read-only set is what a least-privilege agent (the seeded interviewer,
// REQ-91) is granted, so every name in it has to be a tool that still exists.
func TestReadOnlyToolsExist(t *testing.T) {
	have := map[string]bool{}
	for _, tool := range Tools() {
		have[tool.Name] = true
	}
	for name := range readOnlyTools {
		if !have[name] {
			t.Errorf("readOnlyTools names %q, which is not in the tool table", name)
		}
	}
}

// Nothing that writes may be classified read-only. There is no way to check a
// handler's HTTP method from here, so this pins the naming convention the
// table follows: writers are named for what they change.
func TestReadOnlyToolsExcludeWriters(t *testing.T) {
	writerPrefixes := []string{"create_", "update_", "delete_", "record_", "close_", "add_", "delegate_"}
	for name := range readOnlyTools {
		for _, p := range writerPrefixes {
			if strings.HasPrefix(name, p) {
				t.Errorf("%q is classified read-only but is named like a writer", name)
			}
		}
	}
}

// ReadOnlyToolNames is an allowlist an agent definition can hold verbatim:
// prefixed as the vendor CLI wants, stable in order, and never empty.
func TestReadOnlyToolNames(t *testing.T) {
	names := ReadOnlyToolNames()
	if len(names) != len(readOnlyTools) {
		t.Fatalf("got %d names for %d read-only tools", len(names), len(readOnlyTools))
	}
	for _, n := range names {
		if !strings.HasPrefix(n, ToolPrefix) {
			t.Errorf("%q is missing the %q prefix", n, ToolPrefix)
		}
		if !ReadOnly(n) {
			t.Errorf("ReadOnly(%q) = false", n)
		}
	}
	// Stable across calls, so a seed built from it does not churn.
	again := ReadOnlyToolNames()
	for i := range names {
		if names[i] != again[i] {
			t.Fatalf("order is not stable: %v vs %v", names, again)
		}
	}
	// A writer is not read-only, prefixed or not.
	for _, w := range []string{"create_artifact", ToolPrefix + "record_candidate_need", ToolPrefix + "delegate_to_agent"} {
		if ReadOnly(w) {
			t.Errorf("ReadOnly(%q) = true, want false", w)
		}
	}
}
