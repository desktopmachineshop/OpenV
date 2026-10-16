package providers

import (
	"reflect"
	"testing"
)

// TestParseOffered: OPENV_AGENT_PROVIDERS names the providers a deployment
// offers; unset, or naming none the server knows, offers every provider.
func TestParseOffered(t *testing.T) {
	for _, tc := range []struct {
		list    string
		offered map[string]bool
		unknown []string
	}{
		{"", nil, nil},
		{"claude-code", map[string]bool{"claude-code": true}, nil},
		{" claude-code, codex-cli ,", map[string]bool{"claude-code": true, "codex-cli": true}, nil},
		{"claude-code claude_code", map[string]bool{"claude-code": true}, []string{"claude_code"}},
		{"claude", nil, []string{"claude"}},
	} {
		offered, unknown := ParseOffered(tc.list)
		if !reflect.DeepEqual(offered, tc.offered) || !reflect.DeepEqual(unknown, tc.unknown) {
			t.Errorf("ParseOffered(%q) = %v, %v; want %v, %v", tc.list, offered, unknown, tc.offered, tc.unknown)
		}
	}
}
