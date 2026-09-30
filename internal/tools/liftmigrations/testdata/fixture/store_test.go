package store

import "testing"

// A test file is not production code: the lift reads and writes none.
func TestRegistry(t *testing.T) {
	if len(migrations) != 7 {
		t.Fatal(len(migrations))
	}
}
