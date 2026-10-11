package archtest

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The architecture doc rule (refactor plan step D2). docs/architecture.md
// describes the package graph; the rule keeps it complete. Every package
// under cmd/ and internal/ must be named in the doc as a code span holding
// its module-relative directory (`internal/domain/links`), so a new package
// cannot ship without a line saying what it is. The root package is named by
// its file in the Programs table and is not checked.

const architectureDocFile = "docs/architecture.md"

// codeSpanRe is a Markdown inline code span on one line.
var codeSpanRe = regexp.MustCompile("`([^`\n]+)`")

// checkArchitectureDoc fails for each package the doc does not name in a
// code span. A package that is a prefix of another (internal/domain/reports
// and internal/domain/reports/doc) is named only by a span of its own.
func checkArchitectureDoc(c *check) {
	doc, err := os.ReadFile(filepath.Join(c.m.root, filepath.FromSlash(architectureDocFile)))
	if err != nil {
		c.violation("cannot read the architecture doc: %v", err)
		return
	}
	named := map[string]bool{}
	for _, m := range codeSpanRe.FindAllStringSubmatch(string(doc), -1) {
		named[m[1]] = true
	}
	var missing []string
	checked := 0
	for _, name := range c.m.names {
		if name == rootName {
			continue
		}
		checked++
		if !named[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	c.baseline("%d of the %d packages under cmd/ and internal/ are not named in %s", len(missing), checked, architectureDocFile)
	if len(missing) > 0 {
		c.violation("%d packages are not named in %s. Add each to the doc's package inventory (or the Programs table for a "+
			"program) as a code span holding its directory, with a line saying what it holds:\n%s",
			len(missing), architectureDocFile, strings.Join(missing, "\n"))
	}
}

// TestArchitectureDocRule proves the rule on a fixture: a package named in a
// code span passes, one named only in prose, as part of a longer path or in a
// span with other text does not, and the root package is exempt.
func TestArchitectureDocRule(t *testing.T) {
	root := writeFixture(t, map[string]string{
		architectureDocFile: "# Architecture\n\n" +
			"| Package | Holds |\n|---|---|\n" +
			"| `cmd/server` | the API server |\n" +
			"| `internal/domain/reports` | reports |\n" +
			"Prose naming internal/api without a span, and `go test ./internal/events` with other text.\n" +
			"| `internal/domain/reports/doc` | the document model |\n",
	})
	m := &module{root: root, names: []string{
		rootName, "cmd/server", "internal/api", "internal/domain/reports", "internal/domain/reports/doc",
		"internal/domain/reports/docx", "internal/events",
	}}
	got := runRule(t, m, &ratchets{}, checkArchitectureDoc)
	want := "3 packages are not named in " + architectureDocFile + "."
	if len(got) != 1 || !strings.HasPrefix(got[0], want) ||
		!strings.HasSuffix(got[0], ":\ninternal/api\ninternal/domain/reports/docx\ninternal/events") {
		t.Errorf("violations = %q, want %q naming internal/api, internal/domain/reports/docx and internal/events", got, want)
	}
	m.names = []string{rootName, "cmd/server", "internal/domain/reports", "internal/domain/reports/doc"}
	if got := runRule(t, m, &ratchets{}, checkArchitectureDoc); len(got) != 0 {
		t.Errorf("violations = %q, want none", got)
	}
	if got := runRule(t, &module{root: t.TempDir()}, &ratchets{}, checkArchitectureDoc); len(got) != 1 ||
		!strings.HasPrefix(got[0], "cannot read the architecture doc") {
		t.Errorf("violations without a doc = %q, want one naming the missing file", got)
	}
}
