package main

import (
	"fmt"
	"go/ast"
	"io"
	"sort"
	"strings"
)

// selfCheck re-reads what the rewrite made of a file: it parses, a second
// run finds nothing to rewrite, every site left before is still there,
// and each helper is called once more for each rewrite to it.
func selfCheck(r *fileResult) error {
	if string(r.out) == string(r.src) {
		return nil
	}
	after, err := parseFile(r.path, r.out)
	if err != nil {
		return fmt.Errorf("the rewrite does not parse: %v", err)
	}
	before, err := parseFile(r.path, r.src)
	if err != nil {
		return err
	}
	left := map[string]int{}
	want := map[string]int{}
	for _, s := range r.sites {
		if s.helper == "" {
			left[s.kind]++
		} else {
			want[s.helper]++
		}
	}
	for _, s := range after.analyze() {
		if s.helper != "" {
			return fmt.Errorf("line %d: a second run would rewrite again (%s)", s.line, s.helper)
		}
		left[s.kind]--
	}
	for kind, n := range left {
		if n != 0 {
			return fmt.Errorf("the %s sites left as they are changed in number by %d", kind, -n)
		}
	}
	got, had := helperCalls(after.file), helperCalls(before.file)
	for _, h := range append(helperNames[:len(helperNames):len(helperNames)], hInvalidBody) {
		if got[h]-had[h] != want[h] {
			return fmt.Errorf("%s is called %d more times, not %d", h, got[h]-had[h], want[h])
		}
	}
	return nil
}

// helperCalls counts the calls of each helper in f, and the uses of the
// constant.
func helperCalls(f *ast.File) map[string]int {
	n := map[string]int{}
	ast.Inspect(f, func(x ast.Node) bool {
		switch x := x.(type) {
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && isHelperName(id.Name) {
				n[id.Name]++
			}
		case *ast.Ident:
			if x.Name == hInvalidBody && x.Obj == nil {
				n[hInvalidBody]++
			}
		}
		return true
	})
	return n
}

// tally counts one group's sites.
type tally struct {
	helpers      map[string]int
	leftEncodes  int
	leftLiterals int
	kept         int
	encodes      int
	literals     int
}

func (t *tally) add(s site) {
	switch s.kind {
	case kindEncode:
		t.encodes++
	case kindLiteral:
		t.literals++
	}
	switch {
	case s.helper != "":
		t.helpers[s.helper]++
	case s.kept:
		t.kept++
	case s.kind == kindEncode:
		t.leftEncodes++
	case s.kind == kindLiteral:
		t.leftLiterals++
	}
}

func (t *tally) String() string {
	part := func(names ...string) string {
		var p []string
		for _, n := range names {
			p = append(p, fmt.Sprintf("%s %d", n, t.helpers[n]))
		}
		return strings.Join(p, ", ")
	}
	return fmt.Sprintf("encodes %d (%s; left %d), %q literals %d (%s; left %d), %s, kept %d",
		t.encodes, part(hWriteJSON, hWriteJSONOK, hWriteJSONBare, hWriteJSONBareStatus), t.leftEncodes,
		invalidBody, t.literals, part(hDecodeJSON, hInvalidBody), t.leftLiterals, part(hDecodeJSONMsg), t.kept)
}

// report prints every site, then a summary per area (when there is an area
// index) and a total.
func report(w io.Writer, results []*fileResult, byArea bool) {
	total := &tally{helpers: map[string]int{}}
	areas := map[string]*tally{}
	for _, r := range results {
		for _, s := range r.sites {
			switch {
			case s.helper != "":
				fmt.Fprintf(w, "%s:%d: %s (%s)\n", s.file, s.line, s.helper, s.shape)
			case s.kept:
				fmt.Fprintf(w, "%s:%d: kept: %s\n", s.file, s.line, s.reason)
			default:
				fmt.Fprintf(w, "%s:%d: left: %s\n", s.file, s.line, s.reason)
			}
			total.add(s)
			name := r.area
			if name == "" {
				name = "(no area)"
			}
			if areas[name] == nil {
				areas[name] = &tally{helpers: map[string]int{}}
			}
			areas[name].add(s)
		}
	}
	if byArea {
		names := make([]string, 0, len(areas))
		for n := range areas {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(w, "area %s: %s\n", n, areas[n])
		}
	}
	fmt.Fprintf(w, "total: %s\n", total)
}
