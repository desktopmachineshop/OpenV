// This file is shared, byte for byte, by declhash, declmove and movecheck.
// They are separate main packages, and the S1 architecture test freezes the
// module's internal import edges, so they cannot import a common package.
// declhash's TestSharedFileIsIdentical fails when the copies differ: edit
// internal/tools/declhash/decls.go, then copy it over the other two.

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// source is one Go file of a package directory: its base name and content.
type source struct {
	name string
	src  []byte
}

// goFile is a parsed file of a package directory.
type goFile struct {
	name    string
	src     []byte
	ast     *ast.File
	set     string            // "" for production, "test" or "xtest" for a _test.go file
	build   string            // the file's //go:build expression and file-name constraint
	imports map[string]string // local name -> import path, for plain and named imports
	dots    []string          // dot-imported paths
}

// decl is one package-level declaration: a function or method, or one name
// declared by a const, var or type spec.
type decl struct {
	key  string // Name; a method is (*T).Name or T.Name
	kind string // func, method, const, var or type
	file *goFile
	node ast.Decl // the *ast.FuncDecl, or the *ast.GenDecl holding the spec
	hash string   // hex sha256 of the declaration's canonical text
}

// pkgInfo is the parsed content of one package directory.
type pkgInfo struct {
	fset  *token.FileSet
	files []*goFile
	decls []*decl // in file-name order, then source order
}

// setLabel is how a manifest names the declarations of one file set.
func setLabel(label, set string) string {
	if set == "" {
		return label
	}
	return label + " (" + set + ")"
}

// manifest returns one "package<TAB>key<TAB>hash" line per declaration,
// sorted. File names are not part of it.
func (p *pkgInfo) manifest(label string) []string {
	out := make([]string, 0, len(p.decls))
	for _, d := range p.decls {
		out = append(out, setLabel(label, d.file.set)+"\t"+d.key+"\t"+d.hash)
	}
	sort.Strings(out)
	return out
}

func isGoFile(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_")
}

// readDir reads the Go files of a package directory from disk. A missing
// directory is an empty package.
func readDir(dir string) ([]source, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []source
	for _, e := range entries {
		if e.IsDir() || !isGoFile(e.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, source{e.Name(), b})
	}
	return out, nil
}

// readGitDir reads the Go files of a package directory as they are at a git
// ref, without touching the working tree. A directory absent at the ref is
// an empty package.
func readGitDir(ref, dir string) ([]source, error) {
	top, rel, err := gitLocate(dir)
	if err != nil {
		return nil, err
	}
	args := []string{"ls-tree", "-z", "--full-name", ref}
	if rel != "." {
		args = append(args, "--", rel+"/")
	}
	out, err := runGit(top, nil, args...)
	if err != nil {
		return nil, err
	}
	var names, oids []string
	for _, entry := range strings.Split(string(out), "\x00") {
		meta, p, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || fields[1] != "blob" || !isGoFile(path.Base(p)) {
			continue
		}
		names = append(names, path.Base(p))
		oids = append(oids, fields[2])
	}
	if len(oids) == 0 {
		return nil, nil
	}
	blobs, err := runGit(top, strings.NewReader(strings.Join(oids, "\n")+"\n"), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	srcs := make([]source, 0, len(oids))
	for i := range oids {
		header, rest, ok := bytes.Cut(blobs, []byte("\n"))
		fields := strings.Fields(string(header))
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("git cat-file: unexpected output for %s", names[i])
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size+1 > len(rest) {
			return nil, fmt.Errorf("git cat-file: bad size for %s", names[i])
		}
		srcs = append(srcs, source{names[i], rest[:size]})
		blobs = rest[size+1:]
	}
	return srcs, nil
}

// gitLocate returns the top of the git work tree holding dir, and dir
// relative to it, slash-separated. dir need not exist.
func gitLocate(dir string) (top, rel string, err error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	resolved := realPath(abs)
	start := resolved
	for {
		if st, err := os.Stat(start); err == nil && st.IsDir() {
			break
		}
		start = filepath.Dir(start)
	}
	out, err := runGit(start, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", err
	}
	top = realPath(strings.TrimSpace(string(out)))
	r, err := filepath.Rel(top, resolved)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%s is outside the git work tree %s", dir, top)
	}
	return top, filepath.ToSlash(r), nil
}

// realPath resolves symbolic links in the longest existing prefix of p.
func realPath(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	parent := filepath.Dir(p)
	if parent == p {
		return p
	}
	return filepath.Join(realPath(parent), filepath.Base(p))
}

func runGit(dir string, stdin *strings.Reader, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// moduleLabel names a package directory in manifests and messages: its path
// relative to the enclosing go.mod, or the cleaned argument outside a module.
func moduleLabel(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(dir))
	}
	resolved := realPath(abs)
	for root := resolved; ; {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			if r, err := filepath.Rel(root, resolved); err == nil {
				return filepath.ToSlash(r)
			}
		}
		parent := filepath.Dir(root)
		if parent == root {
			return filepath.ToSlash(filepath.Clean(dir))
		}
		root = parent
	}
}

// parsePackage parses the sources of one package directory and lists its
// declarations. _test.go files are included when tests is set.
func parsePackage(srcs []source, tests bool) (*pkgInfo, error) {
	srcs = append([]source(nil), srcs...)
	sort.Slice(srcs, func(i, j int) bool { return srcs[i].name < srcs[j].name })
	p := &pkgInfo{fset: token.NewFileSet()}
	for _, s := range srcs {
		isTest := strings.HasSuffix(s.name, "_test.go")
		if isTest && !tests {
			continue
		}
		af, err := parser.ParseFile(p.fset, s.name, s.src, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		f := &goFile{name: s.name, src: s.src, ast: af, imports: map[string]string{}}
		if isTest {
			f.set = "test"
			if strings.HasSuffix(af.Name.Name, "_test") {
				f.set = "xtest"
			}
		}
		f.build = buildConstraint(af, s.name)
		for _, is := range af.Imports {
			ip, _ := strconv.Unquote(is.Path.Value)
			name := assumedName(ip)
			if is.Name != nil {
				name = is.Name.Name
			}
			switch name {
			case "_":
			case ".":
				f.dots = append(f.dots, ip)
			default:
				f.imports[name] = ip
			}
		}
		p.files = append(p.files, f)
		p.decls = append(p.decls, fileDecls(p.fset, f)...)
	}
	return p, nil
}

// fileDecls lists a file's package-level declarations in source order.
func fileDecls(fset *token.FileSet, f *goFile) []*decl {
	var out []*decl
	for _, d := range f.ast.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			kind := "func"
			if x.Recv != nil {
				kind = "method"
			}
			out = append(out, newDecl(f, x, funcKey(x), kind, printNode(fset, f, x), qualifiers(f, x)))
		case *ast.GenDecl:
			if x.Tok != token.IMPORT {
				out = append(out, genDecls(fset, f, x)...)
			}
		}
	}
	return out
}

// genDecls splits a const, var or type declaration into one entry per
// declared name. Each const carries the type and expression it gets by
// implicit repetition, and its iota when the expression uses iota, so that
// moving a spec out of its group, or reordering the group, shows as a change.
func genDecls(fset *token.FileSet, f *goFile, gd *ast.GenDecl) []*decl {
	doc := commentText(gd.Doc)
	var out []*decl
	var lastType ast.Expr
	var lastValues []ast.Expr
	for i, s := range gd.Specs {
		switch sp := s.(type) {
		case *ast.TypeSpec:
			out = append(out, newDecl(f, gd, sp.Name.Name, "type", doc+printNode(fset, f, sp), qualifiers(f, sp)))
		case *ast.ValueSpec:
			typ, values := sp.Type, sp.Values
			if gd.Tok == token.CONST {
				if len(values) == 0 {
					typ, values = lastType, lastValues
				} else {
					lastType, lastValues = typ, values
				}
			}
			for j, id := range sp.Names {
				text, nodes := valueText(fset, f, gd.Tok, sp, j, typ, values, i)
				out = append(out, newDecl(f, gd, id.Name, gd.Tok.String(), doc+text, qualifiers(f, nodes...)))
			}
		}
	}
	return out
}

// valueText renders the j-th name of a const or var spec on its own, with
// the spec's comments, and returns the nodes it printed.
func valueText(fset *token.FileSet, f *goFile, tok token.Token, sp *ast.ValueSpec, j int, typ ast.Expr, values []ast.Expr, index int) (string, []ast.Node) {
	var b strings.Builder
	var nodes []ast.Node
	b.WriteString(commentText(sp.Doc))
	fmt.Fprintf(&b, "%s %s", tok, sp.Names[j].Name)
	if typ != nil {
		b.WriteString(" " + printNode(fset, f, typ))
		nodes = append(nodes, typ)
	}
	switch {
	case len(values) == len(sp.Names):
		b.WriteString(" = " + printNode(fset, f, values[j]))
		nodes = append(nodes, values[j])
		if tok == token.CONST && usesIota(values[j]) {
			fmt.Fprintf(&b, " // iota = %d", index)
		}
	case len(values) > 0:
		names := make([]string, len(sp.Names))
		for k, id := range sp.Names {
			names[k] = id.Name
		}
		vals := make([]string, len(values))
		for k, v := range values {
			vals[k] = printNode(fset, f, v)
			nodes = append(nodes, v)
		}
		fmt.Fprintf(&b, " // name %d of %s = %s", j, strings.Join(names, ", "), strings.Join(vals, ", "))
	}
	if sp.Comment != nil {
		b.WriteString("\n" + commentText(sp.Comment))
	}
	return b.String(), nodes
}

func usesIota(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "iota" && id.Obj == nil {
			found = true
		}
		return !found
	})
	return found
}

// newDecl hashes a declaration's canonical text: its kind and key, the
// file's build constraints, the imports its package qualifiers resolve to,
// and its go/printer rendering with comments.
func newDecl(f *goFile, node ast.Decl, key, kind, body, quals string) *decl {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", kind, key)
	if f.build != "" {
		fmt.Fprintf(&b, "build: %s\n", f.build)
	}
	if quals != "" {
		fmt.Fprintf(&b, "imports: %s\n", quals)
	}
	b.WriteString(body)
	sum := sha256.Sum256([]byte(b.String()))
	return &decl{key: key, kind: kind, file: f, node: node, hash: hex.EncodeToString(sum[:])}
}

// printNode renders a node the way gofmt would, with the comments inside it
// and its doc comment.
func printNode(fset *token.FileSet, f *goFile, n ast.Node) string {
	var b bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&b, fset, &printer.CommentedNode{Node: n, Comments: f.ast.Comments}); err != nil {
		return "!print: " + err.Error()
	}
	return b.String()
}

func commentText(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range cg.List {
		b.WriteString(c.Text + "\n")
	}
	return b.String()
}

// funcKey names a function Name, and a method (*T).Name or T.Name, without
// type parameters.
func funcKey(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	t, star := fd.Recv.List[0].Type, false
	for {
		switch x := t.(type) {
		case *ast.ParenExpr:
			t = x.X
			continue
		case *ast.StarExpr:
			t, star = x.X, true
			continue
		case *ast.IndexExpr:
			t = x.X
			continue
		case *ast.IndexListExpr:
			t = x.X
			continue
		}
		break
	}
	name := "?"
	if id, ok := t.(*ast.Ident); ok {
		name = id.Name
	}
	if star {
		return "(*" + name + ")." + fd.Name.Name
	}
	return name + "." + fd.Name.Name
}

// qualifiers lists, sorted, the import path each package qualifier used in
// the nodes resolves to in f ("name=path"), plus f's dot imports. A move to
// a file that imports another package under the same name (text/template
// for html/template) therefore changes the hash.
func qualifiers(f *goFile, nodes ...ast.Node) string {
	seen := map[string]bool{}
	for _, n := range nodes {
		ast.Inspect(n, func(x ast.Node) bool {
			if sel, ok := x.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Obj == nil {
					if ip, ok := f.imports[id.Name]; ok {
						seen[id.Name+"="+ip] = true
					}
				}
			}
			return true
		})
	}
	for _, ip := range f.dots {
		seen[".="+ip] = true
	}
	out := make([]string, 0, len(seen))
	for q := range seen {
		out = append(out, q)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

// usedQualifiers returns the local names of f's imports that the nodes use
// as package qualifiers.
func usedQualifiers(f *goFile, nodes ...ast.Node) map[string]bool {
	used := map[string]bool{}
	for _, n := range nodes {
		ast.Inspect(n, func(x ast.Node) bool {
			if sel, ok := x.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Obj == nil {
					if _, ok := f.imports[id.Name]; ok {
						used[id.Name] = true
					}
				}
			}
			return true
		})
	}
	return used
}

// assumedName is the package name an import path is assumed to declare, by
// goimports' rule: the last element, without a /vN suffix, a go- prefix or
// anything from the first character that cannot be in an identifier.
func assumedName(importPath string) string {
	base := path.Base(importPath)
	if strings.HasPrefix(base, "v") {
		if _, err := strconv.Atoi(base[1:]); err == nil {
			if dir := path.Dir(importPath); dir != "." {
				base = path.Base(dir)
			}
		}
	}
	base = strings.TrimPrefix(base, "go-")
	if i := strings.IndexFunc(base, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}); i >= 0 {
		base = base[:i]
	}
	return base
}

// buildConstraint describes what restricts the builds a file is in: its
// //go:build expression and its _GOOS/_GOARCH file-name suffixes.
func buildConstraint(af *ast.File, name string) string {
	var parts []string
	for _, cg := range af.Comments {
		if cg.Pos() > af.Package {
			break
		}
		for _, c := range cg.List {
			if !constraint.IsGoBuild(c.Text) {
				continue
			}
			if expr, err := constraint.Parse(c.Text); err == nil {
				parts = append(parts, "go:build "+expr.String())
			} else {
				parts = append(parts, c.Text)
			}
		}
	}
	if s := nameConstraint(name); s != "" {
		parts = append(parts, "file "+s)
	}
	return strings.Join(parts, "; ")
}

// nameConstraint applies go/build's file-name rule: name_GOOS_GOARCH,
// name_GOOS or name_GOARCH, optionally followed by _test.
func nameConstraint(name string) string {
	name, _, _ = strings.Cut(name, ".")
	i := strings.Index(name, "_")
	if i < 0 {
		return ""
	}
	l := strings.Split(name[i:], "_")
	if n := len(l); n > 0 && l[n-1] == "test" {
		l = l[:n-1]
	}
	n := len(l)
	if n >= 2 && knownOS[l[n-2]] && knownArch[l[n-1]] {
		return l[n-2] + "_" + l[n-1]
	}
	if n >= 1 && (knownOS[l[n-1]] || knownArch[l[n-1]]) {
		return l[n-1]
	}
	return ""
}

// knownOS and knownArch are go/build's lists (syslist.go).
var knownOS = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true, "hurd": true,
	"illumos": true, "ios": true, "js": true, "linux": true, "nacl": true, "netbsd": true, "openbsd": true,
	"plan9": true, "solaris": true, "wasip1": true, "windows": true, "zos": true,
}

var knownArch = map[string]bool{
	"386": true, "amd64": true, "amd64p32": true, "arm": true, "armbe": true, "arm64": true, "arm64be": true,
	"loong64": true, "mips": true, "mipsle": true, "mips64": true, "mips64le": true, "mips64p32": true,
	"mips64p32le": true, "ppc": true, "ppc64": true, "ppc64le": true, "riscv": true, "riscv64": true,
	"s390": true, "s390x": true, "sparc": true, "sparc64": true, "wasm": true,
}
