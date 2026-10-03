package main

import (
	"fmt"
	"go/ast"
	"go/token"
)

// The value check of -flatten -base (S14c). A local declared at the top of
// two or more of the function's bodies (its own and its stages') is one
// variable before the split and one per body after it, and equal text
// cannot tell the two apart. The split is faithful only if, in flattened
// order, every read of a body's copy comes after a write that body made,
// with no other body's write in between: then the read sees the value it
// saw when the copies were one. main()'s err read in the tail after a stage
// wrote its own err is the shape this fails; each stage declaring err and
// writing it before reading it passes.

// flow follows the copies of the names declared at the top of more than one
// body through the flattened statements of one side.
type flow struct {
	sd      *side
	copyOf  map[*ast.Object]*ast.FuncDecl // each tracked copy, and the body declaring it
	top     map[ast.Stmt]bool             // the bodies' top-level statements
	written map[*ast.Ident]bool           // identifiers in a write position
	last    map[string]*ast.FuncDecl      // the body whose copy of a name was written last
	failed  map[string]bool
	errs    []string
}

// crossings walks the side's flattened statements and describes each name
// whose copy one body reads after another body wrote its own; declaredIn
// lists the bodies that declare each name at their top.
func (sd *side) crossings(bodies []*ast.FuncDecl, declaredIn map[string][]*ast.FuncDecl) []string {
	f := &flow{sd: sd, copyOf: map[*ast.Object]*ast.FuncDecl{}, top: map[ast.Stmt]bool{}, written: map[*ast.Ident]bool{},
		last: map[string]*ast.FuncDecl{}, failed: map[string]bool{}}
	for _, fd := range bodies {
		for _, obj := range topObjects(fd) {
			if len(declaredIn[obj.Name]) > 1 {
				f.copyOf[obj] = fd
			}
		}
		for _, s := range fd.Body.List {
			f.top[s] = true
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			var ids []ast.Expr
			switch x := n.(type) {
			case *ast.AssignStmt:
				ids = x.Lhs
			case *ast.IncDecStmt:
				ids = []ast.Expr{x.X}
			case *ast.RangeStmt:
				if x.Tok == token.ASSIGN {
					ids = []ast.Expr{x.Key, x.Value}
				}
			}
			for _, e := range ids {
				if id, ok := e.(*ast.Ident); ok {
					f.written[id] = true
				}
			}
			return true
		})
	}
	if len(f.copyOf) == 0 {
		return nil
	}
	for _, s := range sd.list {
		f.stmt(s)
	}
	return f.errs
}

// stmt follows one statement: a top-level statement of a body that is an
// =, := or var writes the copies it names on its left whole, after its
// other identifiers are read; any other identifier of a copy is a read,
// and a write too when it is assigned (a write in a nested block may not
// run, so the value before it can still reach a later read).
func (f *flow) stmt(s ast.Stmt) {
	whole := map[*ast.Ident]bool{}
	var order []*ast.Ident
	if f.top[s] {
		var lhs []*ast.Ident
		switch x := s.(type) {
		case *ast.AssignStmt:
			if x.Tok == token.ASSIGN || x.Tok == token.DEFINE {
				for _, e := range x.Lhs {
					if id, ok := e.(*ast.Ident); ok {
						lhs = append(lhs, id)
					}
				}
			}
		case *ast.DeclStmt:
			if gd, ok := x.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
				for _, sp := range gd.Specs {
					lhs = append(lhs, sp.(*ast.ValueSpec).Names...)
				}
			}
		}
		for _, id := range lhs {
			if f.copyOf[id.Obj] != nil {
				whole[id] = true
				order = append(order, id)
			}
		}
	}
	ast.Inspect(s, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || whole[id] {
			return true
		}
		if stmts, ok := f.sd.fl.placeholders[id.Name]; ok && id.Obj == nil {
			for _, st := range stmts {
				f.stmt(st)
			}
			return false
		}
		f.read(id)
		if f.written[id] {
			f.write(id)
		}
		return true
	})
	for _, id := range order {
		f.write(id)
	}
}

func (f *flow) read(id *ast.Ident) {
	fd := f.copyOf[id.Obj]
	w := f.last[id.Name]
	if fd == nil || w == nil || w == fd || f.failed[id.Name] {
		return
	}
	f.failed[id.Name] = true
	f.errs = append(f.errs, fmt.Sprintf("%s: %s reads its %s after %s wrote its own %s: %s is declared at the top of both, so before "+
		"the split they were one variable and this read saw that write; make %s a field of the stages' receiver",
		f.sd.fl.p.fset.Position(id.Pos()), funcKey(fd), id.Name, funcKey(w), id.Name, id.Name, id.Name))
}

func (f *flow) write(id *ast.Ident) {
	if fd := f.copyOf[id.Obj]; fd != nil {
		f.last[id.Name] = fd
	}
}
