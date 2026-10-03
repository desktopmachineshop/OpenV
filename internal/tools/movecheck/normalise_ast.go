package main

import (
	"go/ast"
	"go/token"
	"reflect"
)

// The syntax helpers of the -flatten -base normalisation (normalise.go):
// the fields of the stages' receiver type, what each identifier names, and
// a walk that replaces expressions in place.

// structFields lists the fields of the struct type named name in the
// package's production files.
func structFields(p *pkgInfo, name string) map[string]bool {
	out := map[string]bool{}
	for _, f := range p.files {
		if f.set != "" {
			continue
		}
		for _, d := range f.ast.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, s := range gd.Specs {
				ts := s.(*ast.TypeSpec)
				st, ok := ts.Type.(*ast.StructType)
				if !ok || ts.Name.Name != name {
					continue
				}
				for _, fld := range st.Fields.List {
					for _, n := range fld.Names {
						out[n.Name] = true
					}
					if len(fld.Names) == 0 {
						out[typeName(fld.Type)] = true
					}
				}
			}
		}
	}
	return out
}

func typeName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return typeName(x.X)
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.Ident:
		return x.Name
	}
	return ""
}

// tagFunc records what each identifier in fd's body names. recv is the
// stage's receiver, nil for the function itself.
func (sd *side) tagFunc(fd *ast.FuncDecl, recv *ast.Object) {
	top := topDecls(fd)
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		switch {
		case id.Obj == nil:
			sd.tags[id] = bindFree
		case id.Obj.Kind == ast.Lbl:
		case recv != nil && id.Obj == recv:
			sd.tags[id] = bindRecv
		case top[declNode(id.Obj)]:
			sd.tags[id] = bindTop
		case within(fd.Body, declNode(id.Obj)):
			sd.tags[id] = bindNested
		default:
			sd.tags[id] = bindFree
		}
		return true
	})
}

// topDecls are the nodes that declare fd's top-level locals: its top-level
// := statements and the specs of its top-level declarations.
func topDecls(fd *ast.FuncDecl) map[ast.Node]bool {
	top := map[ast.Node]bool{}
	for _, s := range fd.Body.List {
		switch x := s.(type) {
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				top[x] = true
			}
		case *ast.DeclStmt:
			if gd, ok := x.Decl.(*ast.GenDecl); ok {
				for _, sp := range gd.Specs {
					top[sp] = true
				}
			}
		}
	}
	return top
}

func declNode(o *ast.Object) ast.Node {
	n, _ := o.Decl.(ast.Node)
	return n
}

func within(b *ast.BlockStmt, n ast.Node) bool {
	return n != nil && b.Lbrace <= n.Pos() && n.Pos() <= b.Rbrace
}

// tagged is one identifier of a normalised side, in walk order.
type tagged struct {
	name string
	bind binding
	stmt int
	pos  token.Pos
}

// bindings lists what each identifier of the normalised side names, in
// walk order: a selector's name, a composite literal's keys and labels
// name no variable and are left out.
func (sd *side) bindings() []tagged {
	var out []tagged
	for i, s := range sd.list {
		var walk func(n ast.Node)
		walk = func(n ast.Node) {
			ast.Inspect(n, func(m ast.Node) bool {
				switch x := m.(type) {
				case *ast.SelectorExpr:
					walk(x.X)
					return false
				case *ast.CompositeLit:
					if x.Type != nil {
						walk(x.Type)
					}
					for _, e := range x.Elts {
						if kv, ok := e.(*ast.KeyValueExpr); ok {
							if _, isID := kv.Key.(*ast.Ident); isID {
								walk(kv.Value)
								continue
							}
						}
						walk(e)
					}
					return false
				case *ast.Ident:
					if stmts, ok := sd.fl.placeholders[x.Name]; ok {
						for _, st := range stmts {
							walk(st)
						}
						return false
					}
					if b, ok := sd.tags[x]; ok {
						out = append(out, tagged{name: x.Name, bind: b, stmt: i, pos: x.Pos()})
					}
				}
				return true
			})
		}
		walk(s)
	}
	return out
}

var (
	exprType    = reflect.TypeOf((*ast.Expr)(nil)).Elem()
	objectType  = reflect.TypeOf((*ast.Object)(nil))
	scopeType   = reflect.TypeOf((*ast.Scope)(nil))
	commentType = reflect.TypeOf((*ast.CommentGroup)(nil))
)

// replaceIn calls f on every expression under n, outermost first, and
// puts what f returns in its place; f returns its argument to keep it and
// look inside it.
func replaceIn(n ast.Node, f func(ast.Expr) ast.Expr) {
	replaceValue(reflect.ValueOf(n), f)
}

func replaceValue(v reflect.Value, f func(ast.Expr) ast.Expr) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() || v.Type() == objectType || v.Type() == scopeType || v.Type() == commentType {
			return
		}
		replaceValue(v.Elem(), f)
	case reflect.Interface:
		if !v.IsNil() {
			replaceValue(v.Elem(), f)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if fv := v.Field(i); fv.CanSet() {
				replaceField(fv, f)
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			replaceField(v.Index(i), f)
		}
	}
}

func replaceField(fv reflect.Value, f func(ast.Expr) ast.Expr) {
	if fv.Kind() == reflect.Interface && fv.Type() == exprType && !fv.IsNil() {
		e := fv.Interface().(ast.Expr)
		if r := f(e); r != e {
			fv.Set(reflect.ValueOf(r))
			return
		}
	}
	replaceValue(fv, f)
}
