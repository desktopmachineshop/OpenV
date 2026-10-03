package mcp

// readOnlyTools is the read-only set the tests in readonly_test.go range over,
// derived from each tool's ReadOnly flag in Tools(). Production code reads the
// flag where it needs it (ReadOnly, ReadOnlyToolNames) and keeps no set of its
// own: a package-level variable built from Tools() at initialisation is what
// archtest's side-effecting package variables rule refuses.
var readOnlyTools = func() map[string]bool {
	set := map[string]bool{}
	for _, t := range Tools() {
		if t.ReadOnly {
			set[t.Name] = true
		}
	}
	return set
}()
