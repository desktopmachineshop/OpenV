package runner

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

// TestEveryCommandGetsChildEnv keeps the credentials childEnv drops out of
// every process the runner starts, whichever file or build tag starts it:
// each exec.Command or exec.CommandContext in the package's code is assigned
// to a variable whose Env the same function sets from childEnv, since a nil
// Env inherits the whole environment, and nothing starts a process another
// way (os.StartProcess, syscall's ForkExec, Exec or StartProcess, an
// exec.Cmd literal). agentd's own code, which cannot call childEnv, starts
// no process at all.
func TestEveryCommandGetsChildEnv(t *testing.T) {
	fset := token.NewFileSet()
	var problems []string
	commands := 0
	var files []string
	for _, dir := range []string{".", filepath.Join("..", "..", "cmd", "agentd")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if name := e.Name(); !e.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
				files = append(files, filepath.Join(dir, name))
			}
		}
	}
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		imports := map[string]string{} // local name → import path
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			local := path[strings.LastIndex(path, "/")+1:]
			if imp.Name != nil {
				local = imp.Name.Name
			}
			imports[local] = path
		}
		pkgFunc := func(e ast.Expr) (string, string) {
			sel, ok := e.(*ast.SelectorExpr)
			if !ok {
				return "", ""
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok {
				return "", ""
			}
			return imports[x.Name], sel.Sel.Name
		}
		isCommand := func(e ast.Expr) bool {
			call, ok := e.(*ast.CallExpr)
			if !ok {
				return false
			}
			path, fn := pkgFunc(call.Fun)
			return path == "os/exec" && (fn == "Command" || fn == "CommandContext")
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			assigned := map[string]token.Pos{}
			envSet := map[string]bool{}
			inAssign := map[ast.Expr]bool{}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for i, rhs := range as.Rhs {
					if i >= len(as.Lhs) {
						break
					}
					if isCommand(rhs) {
						if id, ok := as.Lhs[i].(*ast.Ident); ok {
							assigned[id.Name] = rhs.Pos()
							inAssign[rhs] = true
						}
					}
					sel, ok := as.Lhs[i].(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "Env" {
						continue
					}
					x, ok := sel.X.(*ast.Ident)
					call, isCall := rhs.(*ast.CallExpr)
					if !ok || !isCall {
						continue
					}
					if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "childEnv" {
						envSet[x.Name] = true
					}
				}
				return true
			})
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					if isCommand(n) {
						commands++
						if !inAssign[n] {
							problems = append(problems, fset.Position(n.Pos()).String()+": "+fd.Name.Name+" starts a command it does not keep in a variable, so its Env cannot be set from childEnv")
						}
					}
					switch path, fn := pkgFunc(n.Fun); {
					case path == "os" && fn == "StartProcess",
						path == "syscall" && (fn == "ForkExec" || fn == "Exec" || fn == "StartProcess"):
						problems = append(problems, fset.Position(n.Pos()).String()+": "+fd.Name.Name+" starts a process with "+fn+"; use exec.Command with Env from childEnv")
					}
				case *ast.CompositeLit:
					if path, fn := pkgFunc(n.Type); path == "os/exec" && fn == "Cmd" {
						problems = append(problems, fset.Position(n.Pos()).String()+": "+fd.Name.Name+" builds an exec.Cmd literal; use exec.Command with Env from childEnv")
					}
				}
				return true
			})
			for v, pos := range assigned {
				if !envSet[v] {
					problems = append(problems, fset.Position(pos).String()+": "+fd.Name.Name+" never sets "+v+".Env = childEnv(...), so the process inherits the runner's keys")
				}
			}
		}
	}
	if commands == 0 {
		t.Fatal("found no exec.Command in the runner: the guard is looking in the wrong place")
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}
