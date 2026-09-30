package main

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"strings"
)

// ctorPlan is one constructor as it will be written.
type ctorPlan struct {
	constructor
	entries []entry
	lines   int // the function's span, "func" line to closing brace, as K14 counts it
}

// planSplit assigns the table's entries to the spec's constructors. The
// spec keys tools by name, so it survives edits to their bodies; when the
// table has moved on since the spec was written (rule R4: the split is
// regenerated on the latest master), a tool the spec does not name joins
// the constructor of the tool before it, a name the table no longer has is
// dropped, and a constructor left with no tool is dropped, each with a note.
// What cannot be reconciled is refused: a spec that lists a tool twice, or
// an order the concatenation could not reproduce.
func planSplit(sp *spec, t *table, maxFunc int) ([]*ctorPlan, []string, error) {
	index := map[string]int{}
	for i, e := range t.entries {
		index[e.name] = i
	}
	owner := make([]int, len(t.entries))
	for i := range owner {
		owner[i] = -1
	}
	var notes []string
	listedIn := map[string]string{}
	for ci, c := range sp.Constructors {
		last := -1
		for _, name := range c.Tools {
			if other, dup := listedIn[name]; dup {
				return nil, nil, fmt.Errorf("the spec lists %q twice, in %s and %s", name, other, c.Func)
			}
			listedIn[name] = c.Func
			i, ok := index[name]
			if !ok {
				notes = append(notes, fmt.Sprintf("the spec names %q (in %s), which %s() does not list: left out", name, c.Func, sp.funcName()))
				continue
			}
			if i < last {
				return nil, nil, fmt.Errorf("constructor %s lists %q after %q, but %s() lists it first; list a constructor's tools in the table's order",
					c.Func, name, t.entries[last].name, sp.funcName())
			}
			owner[i], last = ci, i
		}
	}
	notes = append(notes, adopt(sp, t, owner)...)
	for i := 1; i < len(owner); i++ {
		if owner[i] < owner[i-1] {
			return nil, nil, fmt.Errorf("%s() lists %q (constructor %s) right after %q (constructor %s), but the spec puts %s first: "+
				"constructors are concatenated in spec order, so they must keep the table's order",
				sp.funcName(), t.entries[i].name, sp.Constructors[owner[i]].Func, t.entries[i-1].name,
				sp.Constructors[owner[i-1]].Func, sp.Constructors[owner[i]].Func)
		}
	}
	var plans []*ctorPlan
	for ci, c := range sp.Constructors {
		cp := &ctorPlan{constructor: c}
		for i, e := range t.entries {
			if owner[i] == ci {
				cp.entries = append(cp.entries, e)
			}
		}
		if len(cp.entries) == 0 {
			notes = append(notes, fmt.Sprintf("constructor %s is left with no tool: not written", c.Func))
			continue
		}
		cp.lines = 4 // func line, return line, the literal's closing brace, the function's
		for i, e := range cp.entries {
			cp.lines += countLines(entryText(e, i))
		}
		if cp.lines > maxFunc {
			return nil, nil, fmt.Errorf("constructor %s would span %d lines, over K14's %d-line function budget (internal/archtest): "+
				"give some of its tools a constructor of their own in the spec", c.Func, cp.lines, maxFunc)
		}
		plans = append(plans, cp)
	}
	if len(plans) == 0 {
		return nil, nil, fmt.Errorf("the spec places none of %s()'s %d entries", sp.funcName(), len(t.entries))
	}
	return plans, notes, nil
}

// entryText is the text an entry takes in its constructor: its chunk, less
// the blank lines above it when it comes first.
func entryText(e entry, i int) []byte {
	if i == 0 {
		return trimBlankLines(e.chunk)
	}
	return e.chunk
}

// adopt gives each tool the spec does not name the constructor of the tool
// before it in the table, or, for the first tools, of the first named one.
func adopt(sp *spec, t *table, owner []int) []string {
	var notes []string
	first := -1
	for i := range owner {
		if owner[i] >= 0 {
			first = i
			break
		}
	}
	if first < 0 {
		return nil // nothing named at all: planSplit reports every constructor empty
	}
	for i := range owner {
		if owner[i] >= 0 {
			continue
		}
		if i < first {
			owner[i] = owner[first]
			notes = append(notes, fmt.Sprintf("%q is not in the spec: it joins %s, before %q", t.entries[i].name,
				sp.Constructors[owner[i]].Func, t.entries[first].name))
			continue
		}
		owner[i] = owner[i-1]
		notes = append(notes, fmt.Sprintf("%q is not in the spec: it joins %s, after %q", t.entries[i].name,
			sp.Constructors[owner[i]].Func, t.entries[i-1].name))
	}
	return notes
}

// checkNames refuses a constructor name the package already declares, or a
// file that already exists: splittools only adds.
func checkNames(dir string, p *pkg, sp *spec, plans []*ctorPlan) error {
	declared := map[string]string{}
	for _, f := range p.files {
		if !p.sameScope(f) {
			continue
		}
		for name := range f.imports {
			declared[name] = f.name + " (an import)"
		}
		for _, d := range f.ast.Decls {
			for _, name := range declNames(d) {
				declared[name] = f.name
			}
		}
	}
	for _, cp := range plans {
		if where, ok := declared[cp.Func]; ok {
			return fmt.Errorf("constructor %s: the package already declares %s in %s", cp.Func, cp.Func, where)
		}
		if _, err := os.Lstat(filepath.Join(dir, cp.File)); err == nil {
			return fmt.Errorf("constructor %s: %s already exists; splittools writes new files only", cp.Func, cp.File)
		}
	}
	return nil
}

// declNames lists the package-level names a declaration binds (methods
// bind none).
func declNames(d ast.Decl) []string {
	var out []string
	switch x := d.(type) {
	case *ast.FuncDecl:
		if x.Recv == nil {
			out = append(out, x.Name.Name)
		}
	case *ast.GenDecl:
		for _, s := range x.Specs {
			switch sp := s.(type) {
			case *ast.TypeSpec:
				out = append(out, sp.Name.Name)
			case *ast.ValueSpec:
				for _, id := range sp.Names {
					out = append(out, id.Name)
				}
			}
		}
	}
	return out
}

// mapLines is the tool-to-constructor map a M11a pull request's description
// carries: "tool -> func (file)", in table order.
func mapLines(plans []*ctorPlan) []string {
	var out []string
	for _, cp := range plans {
		for _, e := range cp.entries {
			out = append(out, fmt.Sprintf("%s -> %s (%s)", e.name, cp.Func, cp.File))
		}
	}
	return out
}

// sizeLine summarises the constructors' spans.
func sizeLine(plans []*ctorPlan, maxFunc int) string {
	parts := make([]string, len(plans))
	for i, cp := range plans {
		parts[i] = fmt.Sprintf("%s %d", cp.Func, cp.lines)
	}
	return fmt.Sprintf("constructor lines (budget %d): %s", maxFunc, strings.Join(parts, ", "))
}
