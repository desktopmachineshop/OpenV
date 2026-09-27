package postgres

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/build"
	"go/constant"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// This file is the stored-data freeze (refactor plan step S3, invariant I16,
// OpenV REQ-89 "forward-only migration"), parts (a) and (b), plus the golden
// harness the four parts share:
//
//   - (a) TestMigrationFreeze: every shipped migration's version, name and
//     body, and every package-level declaration the body reaches, hashed and
//     append-only (testdata/freeze/migrations.txt).
//   - (b) TestEveryBootFreeze: the code every boot runs besides the numbered
//     migrations — the 0001 baseline (InitSchema and the schema_*.go chain),
//     the ledger, the runner, the extension reconcile and the org backfill —
//     hashed per declaration (testdata/freeze/every_boot.txt).
//   - (c) TestSchemaGolden (migration_freeze_schema_test.go): the schema after
//     Migrate and after MigrateAndBackfill with a seeded user.
//   - (d) TestPurgeCatalog (migration_freeze_purge_test.go): what PurgeOrg
//     deletes, and how every org-, project- or artifact-scoped table is reached.
//
// Parts (a) and (b) read the source, not a database, so they run everywhere.
// A body is hashed as go/printer renders it from the syntax tree with every
// comment dropped and no source positions, so comments, blank lines, line
// wrapping and the indentation of code are not part of it: moving a closure
// into a named function in its own file (M10), or a helper or the runner into
// another file of the package (M10, M12), leaves every hash as it was, while
// editing one character of code or of an SQL literal changes it. A literal is
// hashed byte for byte, so the indentation inside a multi-line raw string
// (most migration SQL) is part of it: gofmt never reindents one, and a move
// must not either.

// s3UpdateEnv set to exactly 1 rewrites the S3 goldens from the current code
// instead of comparing against them; any other value compares. Only a
// deliberate behavior change regenerates one, and part (a) never rewrites a
// shipped migration even then.
const s3UpdateEnv = "UPDATE_GOLDEN"

func s3Updating() bool { return os.Getenv(s3UpdateEnv) == "1" }

// s3PkgDir is how failure messages name this package's files.
const s3PkgDir = "internal/persistence/postgres"

// s3AllTests is the -run pattern of the four S3 tests: one command
// regenerates every S3 golden (appending new migrations to part (a)).
const s3AllTests = "^(TestMigrationFreeze|TestEveryBootFreeze|TestSchemaGolden|TestPurgeCatalog)$"

// s3RegenerateAll is the command that regenerates every S3 golden at once.
const s3RegenerateAll = s3UpdateEnv + "=1 go test ./" + s3PkgDir + " -count=1 -run '" + s3AllTests + "'"

// s3Regenerate is the command that regenerates the golden a test owns. The
// DB-gated parts record their goldens on a server that offers every optional
// extension the migrations install (see schemaGoldenExtensions), and lead with
// the command for all four: what changes a schema or purge golden is usually
// a new migration, whose body part (a) must freeze in the same change.
func s3Regenerate(test string, needsDB bool) string {
	one := s3UpdateEnv + "=1 go test ./" + s3PkgDir + " -count=1 -run '^" + test + "$'"
	compares := "only " + s3UpdateEnv + "=1 regenerates, any other value compares"
	if !needsDB {
		return one + "\n  (every S3 golden at once: -run '" + s3AllTests + "'; " + compares + ")"
	}
	return s3RegenerateAll +
		"\n  with " + TestDatabaseURLEnv + " pointing at a server that has the vector and pg_trgm extensions" +
		" (CI's pgvector leg runs pgvector/pgvector:pg15); this also appends a new migration to part (a)" +
		"\n  (this golden alone: -run '^" + test + "$'; " + compares + ")"
}

// s3Shown is how a failure message names a golden (paths are relative to this
// package directory, where go test runs).
func s3Shown(path string) string { return filepath.ToSlash(filepath.Join(s3PkgDir, path)) }

// s3WriteGolden writes a golden file, creating its directory.
func s3WriteGolden(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the directory for %s: %v", s3Shown(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write golden %s: %v", s3Shown(path), err)
	}
	t.Logf("regenerated %s", s3Shown(path))
}

// s3ReadGolden reads a golden file, failing with the command that creates it.
func s3ReadGolden(t *testing.T, path, create string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nCreate it with:\n  %s", s3Shown(path), err, create)
	}
	return string(b)
}

// s3Diff is a line diff of want against got: "-" lines only in want, "+"
// lines only in got, with up to two lines of context around each change.
func s3Diff(want, got string) string {
	a := strings.Split(strings.TrimSuffix(want, "\n"), "\n")
	b := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	// Longest common subsequence, by dynamic programming from the end.
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	type line struct {
		op   byte
		text string
	}
	var ops []line
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			ops = append(ops, line{' ', a[i]})
			i++
			j++
		case i < len(a) && (j == len(b) || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, line{'-', a[i]})
			i++
		default:
			ops = append(ops, line{'+', b[j]})
			j++
		}
	}
	const context = 2
	keep := make([]bool, len(ops))
	for k, op := range ops {
		if op.op == ' ' {
			continue
		}
		for n := k - context; n <= k+context; n++ {
			if n >= 0 && n < len(ops) {
				keep[n] = true
			}
		}
	}
	var out strings.Builder
	prev := -1
	for k, op := range ops {
		if !keep[k] {
			continue
		}
		if k != prev+1 {
			out.WriteString("  ...\n")
		}
		fmt.Fprintf(&out, "%c %s\n", op.op, op.text)
		prev = k
	}
	if prev >= 0 && prev < len(ops)-1 {
		out.WriteString("  ...\n")
	}
	if out.Len() == 0 {
		return "(no line differs; the files differ in whitespace or line endings)\n"
	}
	return out.String()
}

// ---------------------------------------------------------------------------
// Reading this package's source.

// freezeSource is this package's production code, parsed without comments and
// type-checked against empty stand-ins for its imports: enough to know which
// package-level declaration each identifier names, which is all the freeze
// needs, and fast because no dependency is loaded.
type freezeSource struct {
	fset  *token.FileSet
	pkg   *types.Package
	info  *types.Info
	decls map[types.Object]*freezeDecl
	byKey map[string]*freezeDecl
}

// freezeDecl is one package-level declaration: a function or method, or one
// name of a const, var or type spec.
type freezeDecl struct {
	key  string // Name; a method is (*T).Name or T.Name
	pos  token.Pos
	node ast.Node // the *ast.FuncDecl, the spec, or a const group whose specs share values
	kw   string   // "const", "var" or "type" before a spec; "" otherwise
}

// stubImporter hands the type checker an empty package for every import, named
// as the go command would name it. Selectors into it fail to resolve, which
// the freeze ignores: it only follows identifiers of this package.
type stubImporter map[string]*types.Package

func (s stubImporter) Import(path string) (*types.Package, error) {
	if p, ok := s[path]; ok {
		return p, nil
	}
	name := path[strings.LastIndex(path, "/")+1:]
	if i := strings.Index(name, "."); i > 0 { // gopkg.in/yaml.v3
		name = name[:i]
	}
	if len(name) > 1 && name[0] == 'v' && strings.Trim(name[1:], "0123456789") == "" { // example.com/x/v2
		trimmed := strings.TrimSuffix(path, "/"+name)
		name = trimmed[strings.LastIndex(trimmed, "/")+1:]
	}
	name = strings.NewReplacer("-", "_", ".", "_").Replace(name)
	p := types.NewPackage(path, name)
	p.MarkComplete()
	s[path] = p
	return p, nil
}

// loadFreezeSource parses and resolves the package's production files (every
// non-test .go file the default build context selects) in the current
// directory, which go test sets to the package directory.
func loadFreezeSource(t *testing.T) *freezeSource {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	s := &freezeSource{
		fset: token.NewFileSet(),
		info: &types.Info{
			Defs:  map[*ast.Ident]types.Object{},
			Uses:  map[*ast.Ident]types.Object{},
			Types: map[ast.Expr]types.TypeAndValue{},
		},
		decls: map[types.Object]*freezeDecl{},
		byKey: map[string]*freezeDecl{},
	}
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if ok, err := build.Default.MatchFile(".", name); err != nil {
			t.Fatalf("match %s: %v", name, err)
		} else if !ok {
			continue
		}
		// No parser.ParseComments: the freeze hashes code, not comments.
		f, err := parser.ParseFile(s.fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		t.Fatal("no production Go files in the package directory")
	}
	conf := types.Config{Importer: stubImporter{}, Error: func(error) {}}
	s.pkg, _ = conf.Check("postgres", s.fset, files, s.info) // errors come from the stub imports only

	add := func(id *ast.Ident, d *freezeDecl) {
		obj := s.info.Defs[id]
		if obj == nil || id.Name == "_" {
			return
		}
		s.decls[obj] = d
		if _, dup := s.byKey[d.key]; !dup {
			s.byKey[d.key] = d
		}
	}
	for _, f := range files {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				key := d.Name.Name
				if d.Recv != nil && len(d.Recv.List) == 1 {
					key = receiverKey(d.Recv.List[0].Type) + "." + key
				}
				add(d.Name, &freezeDecl{key: key, pos: d.Pos(), node: d})
			case *ast.GenDecl:
				grouped := d.Tok == token.CONST && constGroupSharesValues(d)
				for _, spec := range d.Specs {
					switch sp := spec.(type) {
					case *ast.ValueSpec:
						for _, n := range sp.Names {
							fd := &freezeDecl{key: n.Name, pos: n.Pos(), node: sp, kw: d.Tok.String()}
							if grouped {
								fd.node, fd.kw = d, ""
							}
							add(n, fd)
						}
					case *ast.TypeSpec:
						add(sp.Name, &freezeDecl{key: sp.Name.Name, pos: sp.Pos(), node: sp, kw: "type"})
					}
				}
			}
		}
	}
	return s
}

// receiverKey renders a method receiver type as declhash does: *T or T.
func receiverKey(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return "(*" + receiverKey(x.X) + ")"
	case *ast.IndexExpr:
		return receiverKey(x.X)
	case *ast.IndexListExpr:
		return receiverKey(x.X)
	case *ast.Ident:
		return x.Name
	}
	return "?"
}

// constGroupSharesValues reports whether a const group's specs depend on
// their position (iota) or repeat an earlier spec's values: then a name's
// value is only defined by the whole group, which is what gets hashed.
func constGroupSharesValues(d *ast.GenDecl) bool {
	if len(d.Specs) < 2 {
		return false
	}
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if len(vs.Values) == 0 {
			return true
		}
		iota := false
		for _, v := range vs.Values {
			ast.Inspect(v, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "iota" {
					iota = true
				}
				return !iota
			})
		}
		if iota {
			return true
		}
	}
	return false
}

// canonical renders a node as go/printer does, from the syntax tree alone:
// printing against an empty file set hides every source position, so blank
// lines, line wrapping and the indentation of code cannot reach the text, and
// the parse kept no comments. What is left is the code, and literals byte for
// byte, a raw string's inner line breaks and indentation included.
func canonical(n ast.Node) string {
	var b bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&b, token.NewFileSet(), n); err != nil {
		panic(fmt.Sprintf("print %T: %v", n, err))
	}
	return b.String()
}

// text is the declaration's canonical text.
func (d *freezeDecl) text() string {
	if d.kw != "" {
		return d.kw + " " + canonical(d.node)
	}
	return canonical(d.node)
}

// hash is the sha256 of the declaration's canonical text.
func (d *freezeDecl) hash() string { return sha256Hex(d.text()) }

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// where names a position as file:line.
func (s *freezeSource) where(pos token.Pos) string {
	p := s.fset.Position(pos)
	return fmt.Sprintf("%s:%d", filepath.Base(p.Filename), p.Line)
}

// refs lists the package-level declarations the nodes name directly, by key.
// A method of an instantiated generic type resolves to its own object, so it
// is followed to the generic method it instantiates, the one declared.
func (s *freezeSource) refs(nodes ...ast.Node) []*freezeDecl {
	seen := map[*freezeDecl]bool{}
	var out []*freezeDecl
	for _, n := range nodes {
		ast.Inspect(n, func(x ast.Node) bool {
			if id, ok := x.(*ast.Ident); ok {
				obj := s.info.Uses[id]
				if f, ok := obj.(*types.Func); ok {
					obj = f.Origin()
				}
				if d := s.decls[obj]; d != nil && !seen[d] {
					seen[d] = true
					out = append(out, d)
				}
			}
			return true
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// reach returns every package-level declaration reachable from the nodes,
// transitively, sorted by key. A declaration in stop is neither returned nor
// followed.
func (s *freezeSource) reach(stop map[*freezeDecl]bool, nodes ...ast.Node) []*freezeDecl {
	seen := map[*freezeDecl]bool{}
	var out []*freezeDecl
	queue := s.refs(nodes...)
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if seen[d] || stop[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
		queue = append(queue, s.refs(d.node)...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// ---------------------------------------------------------------------------
// Part (a): the migration registry.

const migrationsFreezeGolden = "testdata/freeze/migrations.txt"

const migrationsFreezeHeader = `# Stored-data freeze, part (a) (refactor plan S3, invariant I16; OpenV REQ-89, forward-only
# migration). Every shipped migration, in registry order: version, name, the field holding its
# body (Run runs once in a transaction, RunDB on every boot) and the sha256 of the version, name,
# field and body; then, indented, each package-level declaration the body reaches (transitively)
# with the sha256 of its text. Code is rendered by go/printer without comments or layout, so a
# comment edit, reformatting or a move to another file of the package changes nothing here.
# A shipped migration is never edited, reordered or renumbered: this file only grows.
# Append new migrations with: UPDATE_GOLDEN=1 go test ./internal/persistence/postgres -count=1 -run '^TestMigrationFreeze$'
`

// frozenMigration is one registry entry as the freeze sees it.
type frozenMigration struct {
	version int64
	name    string
	field   string // Run or RunDB
	hash    string
	refs    []frozenRef
	pos     string // file:line of the entry (empty for one read from the golden)
	bodyPos string // file:line of the function literal or the named function
}

// frozenRef is a package-level declaration a migration body reaches.
type frozenRef struct {
	key, hash, pos string
}

func (m frozenMigration) label() string { return fmt.Sprintf("%04d %s", m.version, m.name) }

// registryMigrations reads the migrations registry from the source: the
// composite literal that initialises the package-level var migrations, one
// entry per element, keyed or positional.
func (s *freezeSource) registryMigrations(t *testing.T) []frozenMigration {
	t.Helper()
	reg := s.byKey["migrations"]
	var vs *ast.ValueSpec
	if reg != nil {
		vs, _ = reg.node.(*ast.ValueSpec)
	}
	if vs == nil || len(vs.Values) != 1 {
		t.Fatal("cannot find `var migrations = []Migration{...}`: the S3 freeze reads the registry from that literal; " +
			"if the registry changed shape, teach registryMigrations the new one in a class C commit")
	}
	lit, ok := ast.Unparen(vs.Values[0]).(*ast.CompositeLit)
	if !ok {
		t.Fatalf("%s: the migrations registry is not a composite literal; the S3 freeze reads it from one", s.where(reg.pos))
	}
	var fields []string
	if tn, ok := s.pkg.Scope().Lookup("Migration").(*types.TypeName); ok {
		if st, ok := tn.Type().Underlying().(*types.Struct); ok {
			for i := 0; i < st.NumFields(); i++ {
				fields = append(fields, st.Field(i).Name())
			}
		}
	}
	stop := map[*freezeDecl]bool{reg: true}
	var out []frozenMigration
	for _, elt := range lit.Elts {
		entry, ok := ast.Unparen(elt).(*ast.CompositeLit)
		if !ok {
			t.Fatalf("%s: a registry entry is not a Migration literal", s.where(elt.Pos()))
		}
		values := map[string]ast.Expr{}
		for i, e := range entry.Elts {
			if kv, ok := e.(*ast.KeyValueExpr); ok {
				values[kv.Key.(*ast.Ident).Name] = kv.Value
			} else if i < len(fields) {
				values[fields[i]] = e
			}
		}
		m := frozenMigration{pos: s.where(entry.Pos())}
		if v := s.info.Types[values["Version"]].Value; v != nil && v.Kind() == constant.Int {
			m.version, _ = constant.Int64Val(v)
		} else {
			t.Fatalf("%s: a migration's Version is not an integer constant", m.pos)
		}
		if v := s.info.Types[values["Name"]].Value; v != nil && v.Kind() == constant.String {
			m.name = constant.StringVal(v)
		} else {
			t.Fatalf("%s: migration %04d's Name is not a string constant", m.pos, m.version)
		}
		if strings.ContainsAny(m.name, " \t\r\n") || m.name == "" {
			t.Fatalf("%s: migration %04d's Name %q is empty or has white space, which the freeze file cannot hold", m.pos, m.version, m.name)
		}
		var text strings.Builder
		fmt.Fprintf(&text, "version %d\nname %s\n", m.version, strconv.Quote(m.name))
		var reached []ast.Node
		var extra []string
		for key, value := range values {
			switch key {
			case "Version", "Name":
				continue
			case "Run", "RunDB":
				if id, ok := ast.Unparen(value).(*ast.Ident); ok && id.Name == "nil" && s.info.Uses[id] == types.Universe.Lookup("nil") {
					continue
				}
				if m.field != "" {
					t.Fatalf("%s: migration %s sets both Run and RunDB", m.pos, m.label())
				}
				m.field = key
				ftype, body, from := s.migrationBody(value)
				if body != nil {
					m.bodyPos = s.where(ftype.Pos())
				}
				if body == nil {
					t.Fatalf("%s: migration %s's %s is %s, not a function literal or the name of a package-level function; "+
						"the S3 freeze hashes one of those", m.pos, m.label(), key, canonical(value))
				}
				fmt.Fprintf(&text, "%s: %s %s\n", key, canonical(ftype), canonical(body))
				reached = append(reached, from...)
			default:
				extra = append(extra, key+": "+canonical(value)+"\n")
				reached = append(reached, value)
			}
		}
		if m.field == "" {
			t.Fatalf("%s: migration %s sets neither Run nor RunDB", m.pos, m.label())
		}
		sort.Strings(extra)
		text.WriteString(strings.Join(extra, ""))
		m.hash = sha256Hex(text.String())
		for _, d := range s.reach(stop, reached...) {
			m.refs = append(m.refs, frozenRef{key: d.key, hash: d.hash(), pos: s.where(d.pos)})
		}
		out = append(out, m)
	}
	return out
}

// migrationBody resolves a Run or RunDB value to the function it runs: a
// function literal, or the name of a package-level function (the shape M10
// gives every migration). It returns the signature and body to hash and the
// nodes whose references the body reaches; the function's own name is not
// part of it.
func (s *freezeSource) migrationBody(e ast.Expr) (*ast.FuncType, *ast.BlockStmt, []ast.Node) {
	switch x := ast.Unparen(e).(type) {
	case *ast.FuncLit:
		return x.Type, x.Body, []ast.Node{x.Type, x.Body}
	case *ast.Ident:
		if d := s.decls[s.info.Uses[x]]; d != nil {
			if fd, ok := d.node.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Body != nil {
				return fd.Type, fd.Body, []ast.Node{fd.Type, fd.Body}
			}
		}
	}
	return nil, nil, nil
}

// renderMigrationsFreeze writes the part (a) golden for the given entries.
func renderMigrationsFreeze(ms []frozenMigration) string {
	var b strings.Builder
	b.WriteString(migrationsFreezeHeader)
	for _, m := range ms {
		fmt.Fprintf(&b, "%04d %s %s %s\n", m.version, m.name, m.field, m.hash)
		for _, r := range m.refs {
			fmt.Fprintf(&b, "  %s %s\n", r.key, r.hash)
		}
	}
	return b.String()
}

// parseMigrationsFreeze reads the part (a) golden.
func parseMigrationsFreeze(t *testing.T, content string) []frozenMigration {
	t.Helper()
	var out []frozenMigration
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if strings.HasPrefix(line, "  ") && len(f) == 2 && len(out) > 0 {
			out[len(out)-1].refs = append(out[len(out)-1].refs, frozenRef{key: f[0], hash: f[1]})
			continue
		}
		if len(f) != 4 {
			t.Fatalf("%s:%d: malformed line %q", s3Shown(migrationsFreezeGolden), n, line)
		}
		v, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			t.Fatalf("%s:%d: bad version %q", s3Shown(migrationsFreezeGolden), n, f[0])
		}
		out = append(out, frozenMigration{version: v, name: f[1], field: f[2], hash: f[3]})
	}
	return out
}

// compareShipped lists how the registry departs from the shipped (golden)
// migrations. Each shipped entry must sit at the same position, unchanged.
func compareShipped(shipped, current []frozenMigration) []string {
	var problems []string
	for i, want := range shipped {
		if i >= len(current) {
			problems = append(problems, fmt.Sprintf("shipped migration %s is gone: the registry has only %d entries", want.label(), len(current)))
			continue
		}
		got := current[i]
		if got.version != want.version || got.name != want.name {
			problems = append(problems, fmt.Sprintf("registry position %d (%s) holds %s, but the shipped migration there is %s: "+
				"shipped migrations are never removed, reordered, renumbered or renamed", i+1, got.pos, got.label(), want.label()))
			continue
		}
		if got.field != want.field {
			problems = append(problems, fmt.Sprintf("shipped migration %s (%s) moved its body from %s to %s, which changes when it runs",
				want.label(), got.pos, want.field, got.field))
		}
		if got.hash != want.hash {
			problems = append(problems, fmt.Sprintf("the body of shipped migration %s (%s) changed", want.label(), got.bodyPos))
		}
		wantRefs := map[string]string{}
		for _, r := range want.refs {
			wantRefs[r.key] = r.hash
		}
		gotRefs := map[string]frozenRef{}
		for _, r := range got.refs {
			gotRefs[r.key] = r
			h, ok := wantRefs[r.key]
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("shipped migration %s (%s) now reaches %s (%s)", want.label(), got.pos, r.key, r.pos))
			case h != r.hash:
				problems = append(problems, fmt.Sprintf("%s (%s), which shipped migration %s reaches, changed", r.key, r.pos, want.label()))
			}
		}
		for _, r := range want.refs {
			if _, ok := gotRefs[r.key]; !ok {
				problems = append(problems, fmt.Sprintf("shipped migration %s (%s) no longer reaches %s", want.label(), got.pos, r.key))
			}
		}
	}
	return problems
}

// TestMigrationFreeze is part (a): the numbered migrations are forward-only.
// Every entry of testdata/freeze/migrations.txt must still be in the registry
// at the same position with the same version, name, body and reached
// declarations. Entries appended after the golden's last one pass (they have
// not shipped); UPDATE_GOLDEN=1 appends them, and refuses to rewrite a
// shipped one. An appended entry that the schema golden's ledger already
// records fails: its schema is recorded, so it is about to ship, and its body
// is frozen in the same change, or no later edit to it would fail here.
func TestMigrationFreeze(t *testing.T) {
	src := loadFreezeSource(t)
	current := src.registryMigrations(t)

	// The registry the source declares is the one the package runs.
	if len(current) != len(migrations) {
		t.Fatalf("read %d migrations from the source, but the registry holds %d", len(current), len(migrations))
	}
	for i, m := range migrations {
		if int64(m.Version) != current[i].version || m.Name != current[i].name {
			t.Fatalf("registry entry %d is %04d %s, but the source reads %s", i+1, m.Version, m.Name, current[i].label())
		}
	}

	regenerate := s3Regenerate("TestMigrationFreeze", false)
	content, err := os.ReadFile(migrationsFreezeGolden)
	if err != nil && !(os.IsNotExist(err) && s3Updating()) {
		t.Fatalf("read golden %s: %v\nCreate it with:\n  %s", s3Shown(migrationsFreezeGolden), err, regenerate)
	}
	shipped := parseMigrationsFreeze(t, string(content))
	if problems := compareShipped(shipped, current); len(problems) > 0 {
		t.Fatalf("shipped migrations changed (%s):\n  - %s\n\n"+
			"Migrations are forward-only (REQ-89): a shipped migration is never edited, reordered or renumbered, and "+
			"neither is anything its body reaches; write a new migration that makes the change instead. Comments and "+
			"layout are not hashed. %s=1 appends new migrations but never rewrites a shipped one: only if this "+
			"migration is new on this branch and has not reached master, delete its lines from %s by hand and run:\n  %s",
			s3Shown(migrationsFreezeGolden), strings.Join(problems, "\n  - "), s3UpdateEnv, s3Shown(migrationsFreezeGolden), regenerate)
	}
	if s3Updating() {
		if rendered := renderMigrationsFreeze(current); rendered != string(content) {
			s3WriteGolden(t, migrationsFreezeGolden, rendered)
		}
		return
	}
	// A migration whose schema the part (c) golden records (its ledger line)
	// ships with that golden, so its body is frozen in the same change.
	recorded := map[string]bool{}
	if schema, err := os.ReadFile(schemaGoldenMigrate); err == nil {
		for _, line := range strings.Split(string(schema), "\n") {
			if f := strings.Fields(line); len(f) == 3 && f[0] == "ledger" {
				recorded[f[1]+" "+f[2]] = true
			}
		}
	}
	var unfrozen []string
	for _, m := range current[len(shipped):] {
		if recorded[m.label()] {
			unfrozen = append(unfrozen, fmt.Sprintf("%s (%s)", m.label(), m.pos))
			continue
		}
		t.Logf("migration %s (%s) is not frozen yet; it is appended to %s by:\n  %s",
			m.label(), m.pos, s3Shown(migrationsFreezeGolden), regenerate)
	}
	if len(unfrozen) > 0 {
		t.Fatalf("%s records migration(s) %s in its ledger, but %s does not freeze them: a new migration's body is "+
			"frozen in the same change that records its schema, or a later edit to it would pass every S3 test. Append it with:\n  %s",
			s3Shown(schemaGoldenMigrate), strings.Join(unfrozen, ", "), s3Shown(migrationsFreezeGolden), regenerate)
	}
	// The file is exactly what the shipped entries render to, so a hand edit
	// that the parser tolerates (a reordered ref line, a stray field) fails too.
	if want := renderMigrationsFreeze(current[:len(shipped)]); want != string(content) {
		t.Fatalf("%s is not in the form this test writes:\n%s\nRewrite it with:\n  %s",
			s3Shown(migrationsFreezeGolden), s3Diff(string(content), want), regenerate)
	}
}

// ---------------------------------------------------------------------------
// Part (b): what every boot runs.

const everyBootFreezeGolden = "testdata/freeze/every_boot.txt"

// everyBootRoots are the entry points of the boot-time schema path: the two
// that cmd/server and the tests call, and the 0001 baseline that runs on every
// boot. Everything they reach is frozen with them, except the registry of
// numbered migrations, which part (a) freezes entry by entry.
var everyBootRoots = []string{"Migrate", "MigrateAndBackfill", "InitSchema"}

const everyBootFreezeHeader = `# Stored-data freeze, part (b) (refactor plan S3, invariant I16; OpenV REQ-89). The code every boot
# runs besides the numbered migrations: the 0001 baseline (InitSchema and the schema_*.go chain), the
# ledger, the migration runner, the extension reconcile and the org backfill. One line per
# package-level declaration reachable from Migrate, MigrateAndBackfill and InitSchema (the numbered
# migrations are part (a)'s), sorted, with the sha256 of its text as go/printer renders it without
# comments or layout. Moving a declaration to another file of the package changes nothing here.
# Regenerate (a behavior change only): UPDATE_GOLDEN=1 go test ./internal/persistence/postgres -count=1 -run '^TestEveryBootFreeze$'
`

// TestEveryBootFreeze is part (b): a hash of the every-boot SQL and the
// runner, with everything they reach.
func TestEveryBootFreeze(t *testing.T) {
	src := loadFreezeSource(t)
	var roots []ast.Node
	stop := map[*freezeDecl]bool{}
	if reg := src.byKey["migrations"]; reg != nil {
		stop[reg] = true
	}
	var rootDecls []*freezeDecl
	for _, key := range everyBootRoots {
		d := src.byKey[key]
		if d == nil {
			t.Fatalf("every-boot entry point %s is gone; the S3 freeze starts from %v", key, everyBootRoots)
		}
		roots = append(roots, d.node)
		rootDecls = append(rootDecls, d)
	}
	reached := map[*freezeDecl]bool{}
	for _, d := range append(rootDecls, src.reach(stop, roots...)...) {
		reached[d] = true
	}
	var decls []*freezeDecl
	for d := range reached {
		decls = append(decls, d)
	}
	sort.Slice(decls, func(i, j int) bool { return decls[i].key < decls[j].key })

	var b strings.Builder
	b.WriteString(everyBootFreezeHeader)
	current := map[string]*freezeDecl{}
	for _, d := range decls {
		fmt.Fprintf(&b, "%s %s\n", d.key, d.hash())
		current[d.key] = d
	}
	got := b.String()
	regenerate := s3Regenerate("TestEveryBootFreeze", false)
	if s3Updating() {
		if content, err := os.ReadFile(everyBootFreezeGolden); err != nil || string(content) != got {
			s3WriteGolden(t, everyBootFreezeGolden, got)
		}
		return
	}
	want := s3ReadGolden(t, everyBootFreezeGolden, regenerate)
	if want == got {
		return
	}
	var problems []string
	wantHashes := map[string]string{}
	for _, line := range strings.Split(want, "\n") {
		if f := strings.Fields(line); len(f) == 2 && !strings.HasPrefix(line, "#") {
			wantHashes[f[0]] = f[1]
		}
	}
	for _, d := range decls {
		h, ok := wantHashes[d.key]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("the boot path now reaches %s (%s)", d.key, src.where(d.pos)))
		case h != d.hash():
			problems = append(problems, fmt.Sprintf("%s (%s) changed", d.key, src.where(d.pos)))
		}
	}
	var gone []string
	for key := range wantHashes {
		if current[key] == nil {
			gone = append(gone, key)
		}
	}
	sort.Strings(gone)
	for _, key := range gone {
		problems = append(problems, fmt.Sprintf("the boot path no longer reaches %s", key))
	}
	t.Fatalf("the every-boot schema path changed (%s):\n  - %s\n%s\n"+
		"The baseline, the ledger, the runner, the reconcile and the backfill run on every boot against every "+
		"deployed database, so this is a behavior change, never a refactor: comments, layout and moves between "+
		"files are not hashed. If it is deliberate, regenerate with:\n  %s",
		s3Shown(everyBootFreezeGolden), strings.Join(problems, "\n  - "), s3Diff(want, got), regenerate)
}
