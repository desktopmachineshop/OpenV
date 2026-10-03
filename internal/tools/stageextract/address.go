package main

import (
	"go/ast"
	"go/token"
	"go/types"
)

// What takes a variable's address, and so can keep a pointer to the copy
// of a local that a stage split gives each stage: stageextract makes such
// a local a field rather than split it, and movecheck -flatten -base fails
// a split one. internal/tools/stageextract and internal/tools/movecheck
// hold this file byte for byte (the module's import edges are frozen, so
// they cannot share a package); movecheck's TestAddressFileIsShared keeps
// the copies equal.

// addressedVar is the variable whose address e takes: explicitly (&x,
// &x.f of a struct value, &x[i] of an array) or implicitly, by a method
// with a pointer receiver called or taken as a value on x or a part of it
// (x.m(), x.m, x.f.m()), or by slicing an array (x[:]). It is nil when e
// takes none, or reaches the storage through a pointer, whose address is
// not x's.
func addressedVar(info *types.Info, e ast.Expr) *types.Var {
	switch x := e.(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return storageOf(info, x.X)
		}
	case *ast.SelectorExpr:
		if pointerMethodOnValue(info, x) {
			return storageOf(info, x.X)
		}
	case *ast.SliceExpr:
		if t := info.TypeOf(x.X); t != nil {
			if _, ok := t.Underlying().(*types.Array); ok {
				return storageOf(info, x.X)
			}
		}
	}
	return nil
}

// storageOf is the variable e is stored in: x for x, for a field of a
// struct value x.f, for an element of an array x[i]; nil when e is reached
// through a pointer (or a slice or a map, which hold pointers).
func storageOf(info *types.Info, e ast.Expr) *types.Var {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.Ident:
			v, _ := info.Uses[x].(*types.Var)
			return v
		case *ast.SelectorExpr:
			sel := info.Selections[x]
			if sel == nil || sel.Kind() != types.FieldVal || sel.Indirect() {
				return nil
			}
			e = x.X
		case *ast.IndexExpr:
			t := info.TypeOf(x.X)
			if t == nil {
				return nil
			}
			if _, ok := t.Underlying().(*types.Array); !ok {
				return nil
			}
			e = x.X
		default:
			return nil
		}
	}
}

// pointerMethodOnValue reports x.m where m has a pointer receiver that x,
// not a pointer, provides itself (not through an embedded pointer): the
// call, or the method value, takes x's address.
func pointerMethodOnValue(info *types.Info, x *ast.SelectorExpr) bool {
	sel := info.Selections[x]
	if sel == nil || sel.Kind() != types.MethodVal || sel.Indirect() {
		return false
	}
	fn, ok := sel.Obj().(*types.Func)
	if !ok {
		return false
	}
	recv := fn.Type().(*types.Signature).Recv()
	if recv == nil {
		return false
	}
	if _, ptr := recv.Type().(*types.Pointer); !ptr {
		return false
	}
	t := info.TypeOf(x.X)
	if t == nil || types.IsInterface(t) {
		return false
	}
	_, isPtr := t.Underlying().(*types.Pointer)
	return !isPtr
}
