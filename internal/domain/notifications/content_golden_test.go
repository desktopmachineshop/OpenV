package notifications

// Refactor plan step S10 (docs/plans/codebase-refactor.md §6.4): every
// notification type this package declares has the content goldens
// internal/notify's TestNotificationContent writes, one directory per type
// under internal/notify/testdata/notifications/ holding its stored row, SSE
// frame, email and web push. A type added here with no goldens fails, so a
// new type cannot ship with its delivery unpinned, and a golden directory no
// constant owns fails too, so a renamed or dropped type cannot leave stale
// goldens behind. The constants are read from this package's source, so
// adding one needs no edit to this test.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// contentGoldenDir is internal/notify/testdata/notifications, from here.
const contentGoldenDir = "../../notify/testdata/notifications"

// contentGoldenFiles are the four goldens of each type (internal/notify's
// ncGoldenFiles).
var contentGoldenFiles = []string{"row.json", "sse.txt", "email.txt", "push.json"}

const contentRegenerate = "UPDATE_GOLDEN=1 go test ./internal/notify -count=1 -run '^TestNotificationContent$'"

// typeConstants parses this package's production files for its exported
// Type* string constants: name -> value.
func typeConstants(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	out := map[string]string{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, id := range vs.Names {
					if !strings.HasPrefix(id.Name, "Type") || !id.IsExported() || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					v, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("%s: %s: %v", name, id.Name, err)
					}
					out[id.Name] = v
				}
			}
		}
	}
	return out
}

func TestEveryNotificationTypeHasAContentGolden(t *testing.T) {
	constants := typeConstants(t)
	// The twelve of today. The count is not the guard (the goldens are); it
	// only makes a parser that silently finds nothing fail here.
	if len(constants) < 12 {
		t.Fatalf("found %d Type* constants in this package, want at least the 12 of refactor plan step S10: %v",
			len(constants), constants)
	}
	names := make([]string, 0, len(constants))
	values := map[string]string{}
	for name, value := range constants {
		names = append(names, name)
		if other, dup := values[value]; dup {
			t.Errorf("%s and %s both declare the type %q", other, name, value)
		}
		values[value] = name
	}
	sort.Strings(names)
	for _, name := range names {
		value := constants[name]
		var missing []string
		for _, f := range contentGoldenFiles {
			if _, err := os.Stat(filepath.Join(contentGoldenDir, value, f)); err != nil {
				missing = append(missing, f)
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s (%q) has no content golden %s under internal/notify/testdata/notifications/%s/: "+
				"add a scenario that delivers it to ncScenarios in internal/notify/notification_content_test.go, "+
				"then create its goldens with:\n  %s", name, value, strings.Join(missing, ", "), value, contentRegenerate)
		}
	}

	entries, err := os.ReadDir(contentGoldenDir)
	if err != nil {
		t.Fatalf("read the content goldens: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			if _, ok := values[e.Name()]; !ok {
				t.Errorf("internal/notify/testdata/notifications/%s/ belongs to no Type* constant of this package: "+
					"remove it with the type it pinned", e.Name())
			}
		}
	}
}
