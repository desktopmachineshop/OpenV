package vv

import "testing"

func TestExecutionMethod(t *testing.T) {
	cases := []struct {
		name  string
		attrs map[string]interface{}
		want  string
	}{
		{"unset defaults to automated", map[string]interface{}{}, ExecutionAutomated},
		{"nil attributes default to automated", nil, ExecutionAutomated},
		{"explicit automated", map[string]interface{}{ExecutionMethodAttr: "automated"}, ExecutionAutomated},
		{"manual", map[string]interface{}{ExecutionMethodAttr: "manual"}, ExecutionManual},
		{"physical", map[string]interface{}{ExecutionMethodAttr: "physical"}, ExecutionPhysical},
		{"case and space insensitive", map[string]interface{}{ExecutionMethodAttr: "  Physical "}, ExecutionPhysical},
		{"unrecognized value falls back to automated", map[string]interface{}{ExecutionMethodAttr: "sometimes"}, ExecutionAutomated},
		{"non-string value falls back to automated", map[string]interface{}{ExecutionMethodAttr: 42}, ExecutionAutomated},
		{"other attributes are ignored", map[string]interface{}{"verification_method": "test"}, ExecutionAutomated},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExecutionMethod(tc.attrs); got != tc.want {
				t.Errorf("ExecutionMethod(%v) = %q, want %q", tc.attrs, got, tc.want)
			}
		})
	}
}

func TestAgentExecutable(t *testing.T) {
	cases := []struct {
		name  string
		attrs map[string]interface{}
		want  bool
	}{
		{"unflagged cases are agent-executable", map[string]interface{}{}, true},
		{"automated is agent-executable", map[string]interface{}{ExecutionMethodAttr: ExecutionAutomated}, true},
		{"manual stays with people", map[string]interface{}{ExecutionMethodAttr: ExecutionManual}, false},
		{"physical stays with people", map[string]interface{}{ExecutionMethodAttr: ExecutionPhysical}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AgentExecutable(tc.attrs); got != tc.want {
				t.Errorf("AgentExecutable(%v) = %v, want %v", tc.attrs, got, tc.want)
			}
		})
	}
}
