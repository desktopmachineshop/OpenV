package archtest

import (
	"strings"
	"testing"
)

// extModules are the packages that leave this repository for the hosted
// service's own (docs/plans/hosted-edition-separation.md, Phase 3), with
// the root of each one's own tree. Another Go module cannot import this
// one's internal packages, so each may import, of this module, only ext
// and its own tree: anything else would not build once it has moved.
var extModules = map[string]string{
	"internal/billing":        "internal/billing",
	"internal/billing/stripe": "internal/billing",
	"internal/billing/boot":   "internal/billing",
}

func TestExtModulesImportOnlyExt(t *testing.T) {
	m, err := loadModule()
	if err != nil {
		t.Fatal(err)
	}
	for pkg, tree := range extModules {
		if m.pkgs[pkg] == nil {
			t.Errorf("%s is listed in extModules but is not a package of this module", pkg)
			continue
		}
		for _, to := range m.edges[pkg] {
			if to == "ext" || strings.HasPrefix(to, "ext/") || to == tree || strings.HasPrefix(to, tree+"/") {
				continue
			}
			t.Errorf("%s imports %s; a module that leaves this repository may import only ext and its own tree (%s)", pkg, to, tree)
		}
	}
}
