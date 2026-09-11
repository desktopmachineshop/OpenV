package mcp

import (
	"slices"
	"testing"
)

// toolNames is the filtered table's names, for readable assertions.
func toolNames(tools []Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

// REQ-91: OPENV_MCP_TOOLS is how an agent's allowlist reaches the OpenV tools
// themselves, for vendor CLIs whose own flags cannot express one. The variable
// is read as set-or-unset, so "set but empty" is a real answer — no tools —
// and not the same as "no filter".
func TestEnvFilteredTools(t *testing.T) {
	all := Tools()

	t.Run("unset means no filter", func(t *testing.T) {
		if got := EnvFilteredTools(all); len(got) != len(all) {
			t.Errorf("filtered %d of %d tools with %s unset", len(got), len(all), EnvToolAllowlist)
		}
	})

	t.Run("set but empty means no tools", func(t *testing.T) {
		t.Setenv(EnvToolAllowlist, "")
		if got := EnvFilteredTools(all); len(got) != 0 {
			t.Errorf("tools = %v, want none for an agent that names no OpenV tool", toolNames(got))
		}
	})

	t.Run("wildcard means every tool", func(t *testing.T) {
		for _, raw := range []string{"*", "mcp__openv__*", "Read,mcp__openv__*"} {
			t.Setenv(EnvToolAllowlist, raw)
			if got := EnvFilteredTools(all); len(got) != len(all) {
				t.Errorf("%s=%q served %d of %d tools", EnvToolAllowlist, raw, len(got), len(all))
			}
		}
	})

	t.Run("a list serves exactly those tools", func(t *testing.T) {
		// Bare and prefixed spellings both work, blanks are ignored, and a
		// tool the list does not name is not served.
		t.Setenv(EnvToolAllowlist, "get_artifact, ,mcp__openv__list_projects")
		got := toolNames(EnvFilteredTools(all))
		if !slices.Contains(got, "get_artifact") || !slices.Contains(got, "list_projects") {
			t.Errorf("tools = %v, want get_artifact and list_projects", got)
		}
		if slices.Contains(got, "create_artifact") {
			t.Errorf("tools = %v, want nothing the allowlist did not name", got)
		}
		if len(got) != 2 {
			t.Errorf("tools = %v, want exactly the two named", got)
		}
	})
}

// The filter has to reach tools/call, not just tools/list: a tool that is
// merely hidden but still callable would be no restriction at all. serve
// builds its dispatch table from the same filtered slice, so this pins that
// they cannot drift apart.
func TestServeUsesTheFilteredTable(t *testing.T) {
	t.Setenv(EnvToolAllowlist, "list_projects")
	filtered := EnvFilteredTools(Tools())

	sess := startSession(t, &Client{}, filtered)

	sess.send("1", "tools/list", nil)
	tools, _ := sess.recv().Result["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("tools/list returned %d tools, want 1", len(tools))
	}

	sess.send("2", "tools/call", map[string]interface{}{
		"name":      "create_artifact",
		"arguments": map[string]interface{}{},
	})
	if resp := sess.recv(); resp.Error == nil {
		t.Fatal("tools/call on a filtered-out tool succeeded; it must not be callable")
	}
	sess.shutdown()
}
