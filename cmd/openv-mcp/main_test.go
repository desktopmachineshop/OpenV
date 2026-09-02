package main

import "testing"

func TestResolveToken(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"run token", map[string]string{"OPENV_RUN_TOKEN": "run"}, "run"},
		{"runner key", map[string]string{"OPENV_API_TOKEN": "key"}, "key"},
		{"run token wins", map[string]string{"OPENV_RUN_TOKEN": "run", "OPENV_API_TOKEN": "key"}, "run"},
		{"neither", map[string]string{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveToken(func(k string) string { return tc.env[k] })
			if got != tc.want {
				t.Errorf("resolveToken() = %q, want %q", got, tc.want)
			}
		})
	}
}
