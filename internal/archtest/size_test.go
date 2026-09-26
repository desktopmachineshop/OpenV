package archtest

import "go/ast"

// K14 size budgets for production Go code. Generated files and _test.go
// files are exempt; so is everything grandfathered in ratchets.json, up to
// its ceiling.
const (
	fileBudget = 800 // lines per non-test .go file
	funcBudget = 100 // lines per function declaration, "func" line to closing brace
)

func checkFileSizes(c *check) {
	sizes := map[string]int{}
	for _, f := range c.m.production() {
		if !f.gen {
			sizes[f.rel] = f.lines
		}
	}
	c.next.FileLines = c.judgeCeilings(c.stored.FileLines, sizes, nil, fileBudget, "file")
}

func checkFuncSizes(c *check) {
	sizes, where := funcSizes(c.m)
	c.next.FuncLines = c.judgeCeilings(c.stored.FuncLines, sizes, where, funcBudget, "function")
}

// funcSizes measures every function declaration in production code, keyed
// "package:Func" or "package:Receiver.Method", so that a function keeps its
// ceiling when it moves to another file of its package. Build-tag variants
// of one function share a key and the larger size. where holds each key's
// file:line.
func funcSizes(m *module) (sizes map[string]int, where map[string]string) {
	sizes, where = map[string]int{}, map[string]string{}
	for _, f := range m.production() {
		if f.gen {
			continue
		}
		for _, d := range f.ast.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name == "_" {
				continue
			}
			key := f.pkg.name + ":" + funcName(fd)
			if n := m.line(fd.End()) - m.line(fd.Pos()) + 1; n > sizes[key] {
				sizes[key], where[key] = n, m.pos(fd)
			}
		}
	}
	return sizes, where
}

// funcName is Name, or Receiver.Name for a method (pointer and type
// parameters dropped).
func funcName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	return recvName(fd.Recv.List[0].Type) + "." + fd.Name.Name
}

func recvName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return recvName(x.X)
	case *ast.ParenExpr:
		return recvName(x.X)
	case *ast.IndexExpr:
		return recvName(x.X)
	case *ast.IndexListExpr:
		return recvName(x.X)
	case *ast.Ident:
		return x.Name
	}
	return "?"
}
