package main

import "testing"

// TestEnvParse writes cmd/server's sections of
// internal/archtest/testdata/env_parse.txt (refactor plan S8, invariant
// I13), of which there are none since refactor step X10b: the stages read
// every setting through internal/config, whose TestEnvParse writes the
// sections of its parse helpers, so this package has no env getter left and
// this writer holds that no section is its own. env_parse_helpers_test.go
// stays beside it for the stage pins, which use envParseUnset.
func TestEnvParse(t *testing.T) {
	checkEnvParse(t, "cmd/server", map[string][]string{})
}
