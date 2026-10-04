package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// goFile is one parsed Go source file of the repository.
type goFile struct {
	path string // relative to the root, with forward slashes
	src  []byte
	fset *token.FileSet
	ast  *ast.File
}

func parseGo(root, path string) (*goFile, error) {
	src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	return &goFile{path: path, src: src, fset: fset, ast: f}, nil
}

// parseDir parses every Go file of dir, test files included, by name.
func parseDir(root, dir string) ([]*goFile, error) {
	paths, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(dir), "*.go"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var out []*goFile
	for _, p := range paths {
		f, err := parseGo(root, dir+"/"+filepath.Base(p))
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// declared maps every name declared at the top level of files to the file
// that declares it; a method counts by its bare name. Test files share the
// package's scope, so they count too.
func declared(files []*goFile) map[string]string {
	out := map[string]string{}
	add := func(id *ast.Ident, f *goFile) {
		if id != nil && id.Name != "_" {
			out[id.Name] = filepath.Base(f.path)
		}
	}
	for _, f := range files {
		for _, d := range f.ast.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				add(d.Name, f)
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						add(s.Name, f)
					case *ast.ValueSpec:
						for _, id := range s.Names {
							add(id, f)
						}
					}
				}
			}
		}
	}
	return out
}

// refuseDeclared refuses a name the package already declares.
func refuseDeclared(names map[string]string, dir string, ids ...string) error {
	for _, id := range ids {
		if f, ok := names[id]; ok {
			return fmt.Errorf("%s is already declared in %s/%s; pick another name", id, dir, f)
		}
	}
	return nil
}

// refuseExisting refuses to create a file that is already there.
func refuseExisting(root, path string) error {
	_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
	switch {
	case err == nil:
		return fmt.Errorf("%s already exists; scaffold never overwrites a file", path)
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return err
	}
}

// findFunc returns the function, or with method set the method, named
// name declared in f.
func findFunc(f *goFile, name string, method bool) *ast.FuncDecl {
	for _, d := range f.ast.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name && (fd.Recv != nil) == method && fd.Body != nil {
			return fd
		}
	}
	return nil
}

// insertBefore returns f's source with text inserted at the start of the
// line that holds pos, gofmt-ed. That line must hold nothing before pos
// but indentation: pos closes a list (a block, a composite literal, a call's
// arguments) on its own line, so text is appended to the list.
func (f *goFile) insertBefore(pos token.Pos, text string) ([]byte, error) {
	at := f.fset.Position(pos)
	start := bytes.LastIndexByte(f.src[:at.Offset], '\n') + 1
	if len(bytes.TrimSpace(f.src[start:at.Offset])) != 0 {
		return nil, fmt.Errorf("%s:%d: the list does not close on a line of its own, so scaffold cannot append to it; add the line by hand", f.path, at.Line)
	}
	out := make([]byte, 0, len(f.src)+len(text))
	out = append(append(append(out, f.src[:start]...), text...), f.src[start:]...)
	return gofmt(f.path, out)
}

// gofmt formats a file the scaffold wrote; a failure is a scaffold bug.
func gofmt(path string, src []byte) ([]byte, error) {
	out, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("%s: what scaffold wrote does not parse, a bug in scaffold: %v", path, err)
	}
	return out, nil
}

// render executes a file template and gofmts the result.
func render(path string, tmpl *template.Template, data any) ([]byte, error) {
	var b bytes.Buffer
	if err := tmpl.Execute(&b, data); err != nil {
		return nil, err
	}
	return gofmt(path, b.Bytes())
}

// modulePath reads the module path from root's go.mod.
func modulePath(root string) (string, error) {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if mod, ok := strings.CutPrefix(strings.TrimSpace(s.Text()), "module "); ok {
			return strings.Trim(strings.TrimSpace(mod), `"`), nil
		}
	}
	if err := s.Err(); err != nil {
		return "", err
	}
	return "", errors.New("go.mod names no module")
}

// hasLine reports whether the text file at path holds line exactly.
func hasLine(root, path, line string) (bool, error) {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return false, err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == line {
			return true, nil
		}
	}
	return false, nil
}
