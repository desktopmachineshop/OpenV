package postgres

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// migrationFileName matches a numbered migration's own file (refactor plan
// M10): migration_NNNN_<name>.go, where <name> is the registry entry's Name.
var migrationFileName = regexp.MustCompile(`^migration_(\d{4})_([a-z0-9_]+)\.go$`)

// registeredMigration is one registry entry as migrations.go writes it.
type registeredMigration struct {
	version int
	name    string
	run     string // the function Run names; "" for RunDB or a literal
	pos     string
}

// migrationFuncName is the function a migration's file declares: m, the
// version in four digits and the name in camel case
// (unique_personal_org_per_user is m0002UniquePersonalOrgPerUser).
func migrationFuncName(version int, name string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "m%04d", version)
	for _, part := range strings.Split(name, "_") {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + part[1:])
		}
	}
	return b.String()
}

// TestEachMigrationFileRegistersItsVersion checks the layout M10 left, with
// no database: every migration_NNNN_<name>.go declares one function,
// m<NNNN><Name>, and nothing else; exactly one registry entry runs it, the
// entry of version NNNN named <name>; and every entry from 0002 on runs the
// function of its own file. A migration added to the registry as a function
// literal fails here until `go run ./internal/tools/liftmigrations` moves
// it into its file.
func TestEachMigrationFileRegistersItsVersion(t *testing.T) {
	funcFile, fileFuncs, registry := readMigrationLayout(t)
	if len(registry) != len(migrations) {
		t.Fatalf("read %d registry entries from the source, but migrations has %d", len(registry), len(migrations))
	}

	runBy := map[string][]registeredMigration{}
	for _, m := range registry {
		if m.version == 1 {
			continue // the baseline, RunDB: InitSchema
		}
		wantFunc := migrationFuncName(m.version, m.name)
		wantFile := fmt.Sprintf("migration_%04d_%s.go", m.version, m.name)
		switch {
		case m.run == "":
			t.Errorf("%s: migration %04d (%s) must run %s, declared in %s; move its literal there with:\n  go run ./internal/tools/liftmigrations",
				m.pos, m.version, m.name, wantFunc, wantFile)
		case m.run != wantFunc || funcFile[m.run] != wantFile:
			t.Errorf("%s: migration %04d (%s) runs %s, declared in %q; want %s, declared in %s",
				m.pos, m.version, m.name, m.run, funcFile[m.run], wantFunc, wantFile)
		}
		if m.run != "" {
			runBy[m.run] = append(runBy[m.run], m)
		}
	}

	files := make([]string, 0, len(fileFuncs))
	for f := range fileFuncs {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, file := range files {
		sub := migrationFileName.FindStringSubmatch(file)
		version, _ := strconv.Atoi(sub[1])
		want := migrationFuncName(version, sub[2])
		if got := fileFuncs[file]; len(got) != 1 || got[0] != want {
			t.Errorf("%s declares %v; want the one function %s", file, got, want)
			continue
		}
		entries := runBy[want]
		if len(entries) != 1 || entries[0].version != version || entries[0].name != sub[2] {
			t.Errorf("%s: %s is run by %d registry entries %v; want exactly {Version: %d, Name: %q}",
				file, want, len(entries), entries, version, sub[2])
		}
	}
}

// readMigrationLayout parses the package's non-test files and returns the
// file that declares each package-level function, what each migration file
// declares, and the registry's entries in order.
func readMigrationLayout(t *testing.T) (map[string]string, map[string][]string, []registeredMigration) {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	funcFile := map[string]string{}
	fileFuncs := map[string][]string{}
	var registry []registeredMigration
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		isMigration := migrationFileName.MatchString(path)
		if isMigration {
			fileFuncs[path] = nil
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					funcFile[d.Name.Name] = path
				}
				if isMigration {
					fileFuncs[path] = append(fileFuncs[path], d.Name.Name)
				}
			case *ast.GenDecl:
				if isMigration && d.Tok != token.IMPORT {
					fileFuncs[path] = append(fileFuncs[path], "a "+d.Tok.String())
				}
				if r := registryEntries(t, fset, d); r != nil {
					registry = r
				}
			}
		}
	}
	return funcFile, fileFuncs, registry
}

// registryEntries reads `var migrations = []Migration{...}`, or returns nil
// for any other declaration.
func registryEntries(t *testing.T, fset *token.FileSet, d *ast.GenDecl) []registeredMigration {
	t.Helper()
	if d.Tok != token.VAR || len(d.Specs) != 1 {
		return nil
	}
	spec := d.Specs[0].(*ast.ValueSpec)
	if len(spec.Names) != 1 || spec.Names[0].Name != "migrations" || len(spec.Values) != 1 {
		return nil
	}
	list, ok := spec.Values[0].(*ast.CompositeLit)
	if !ok {
		t.Fatalf("%s: migrations is not a composite literal", fset.Position(spec.Pos()))
	}
	var out []registeredMigration
	for _, elt := range list.Elts {
		m := registeredMigration{pos: fset.Position(elt.Pos()).String()}
		entry, ok := elt.(*ast.CompositeLit)
		if !ok {
			t.Fatalf("%s: a registry entry is not a composite literal", m.pos)
		}
		for _, field := range entry.Elts {
			kv, ok := field.(*ast.KeyValueExpr)
			if !ok {
				t.Fatalf("%s: a registry entry's field has no key", m.pos)
			}
			key, _ := kv.Key.(*ast.Ident)
			lit, _ := kv.Value.(*ast.BasicLit)
			switch {
			case key == nil:
			case key.Name == "Version" && lit != nil && lit.Kind == token.INT:
				m.version, _ = strconv.Atoi(lit.Value)
			case key.Name == "Name" && lit != nil && lit.Kind == token.STRING:
				m.name, _ = strconv.Unquote(lit.Value)
			case key.Name == "Run":
				if id, ok := kv.Value.(*ast.Ident); ok {
					m.run = id.Name
				}
			}
		}
		out = append(out, m)
	}
	return out
}
