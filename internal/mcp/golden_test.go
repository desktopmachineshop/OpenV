package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file is the harness behind the MCP goldens under testdata/ (refactor
// plan step S7, invariant I11): where they live, how a mismatch is reported,
// and how a REST call a tool makes is resolved against the API's route
// inventory. Deployed agent CLIs call these tools by name, with these
// arguments, and the tools call these routes, so every golden here is part of
// the compatibility promise (REQ-143). The runner package keeps its own copy
// of the same helpers for the worker wire goldens.

// updateGoldenEnv set to exactly 1 rewrites the goldens from the current code
// instead of comparing against them; any other value compares. Only a
// deliberate behavior change regenerates a golden; a refactor never does.
const updateGoldenEnv = "UPDATE_GOLDEN"

func updatingGoldens() bool { return os.Getenv(updateGoldenEnv) == "1" }

// regenerateCommand is the one command that rewrites the golden a test owns,
// with a reminder that no other value of UPDATE_GOLDEN does.
func regenerateCommand(test string) string {
	return updateGoldenEnv + "=1 go test ./internal/mcp ./internal/runner -run " + test +
		"\n(only " + updateGoldenEnv + "=1 regenerates; any other value compares)"
}

// goldenPkgDir is how failure messages name this package's golden files.
const goldenPkgDir = "internal/mcp"

// checkGolden compares got with the golden file at path (relative to this
// package), or rewrites the file when UPDATE_GOLDEN is 1.
func checkGolden(t *testing.T, path string, got []byte, test string) {
	t.Helper()
	shown := filepath.ToSlash(filepath.Join(goldenPkgDir, path))
	if updatingGoldens() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create the directory for %s: %v", shown, err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", shown, err)
		}
		t.Logf("regenerated %s", shown)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nCreate it with:\n  %s", shown, err, regenerateCommand(test))
	}
	if !bytes.Equal(want, got) {
		t.Errorf("golden %s does not match the code:\n%s\n"+
			"A refactor never changes a golden. If this is a deliberate behavior change, regenerate it with:\n  %s",
			shown, goldenDiff(string(want), string(got)), regenerateCommand(test))
	}
}

// encodeGolden renders a JSON golden: two-space indent, no HTML escaping, and
// a trailing newline.
func encodeGolden(t *testing.T, v interface{}) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatalf("encode golden: %v", err)
	}
	return buf.Bytes()
}

// goldenJSONWidth is the column limit under which a container that holds only
// scalars is written on one line: a schema property, a required list or a
// tool's calls read as one row each.
const goldenJSONWidth = 100

// encodeGoldenCompact renders a JSON golden like encodeGolden, except that an
// object or array holding no other container is kept on one line when it fits
// in goldenJSONWidth columns. Keys keep their document order.
func encodeGoldenCompact(t *testing.T, v interface{}) []byte {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(encodeGoldenLine(t, v)))
	dec.UseNumber()
	root, err := decodeGoldenNode(t, dec)
	if err != nil {
		t.Fatalf("re-read golden JSON: %v", err)
	}
	var b strings.Builder
	root.render(&b, "", 0)
	b.WriteByte('\n')
	return []byte(b.String())
}

// encodeGoldenLine is v as one line of JSON without HTML escaping.
func encodeGoldenLine(t *testing.T, v interface{}) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatalf("encode golden: %v", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

// goldenNode is a parsed JSON value that keeps object keys in order. A
// scalar holds its JSON text; a container has open set to '{' or '['.
type goldenNode struct {
	scalar string
	open   byte
	keys   []string // quoted object keys, in document order
	items  []*goldenNode
}

func decodeGoldenNode(t *testing.T, dec *json.Decoder) (*goldenNode, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, isDelim := tok.(json.Delim)
	if !isDelim {
		return &goldenNode{scalar: string(encodeGoldenLine(t, tok))}, nil
	}
	n := &goldenNode{open: byte(delim)}
	for dec.More() {
		if n.open == '{' {
			key, err := dec.Token()
			if err != nil {
				return nil, err
			}
			n.keys = append(n.keys, string(encodeGoldenLine(t, key)))
		}
		item, err := decodeGoldenNode(t, dec)
		if err != nil {
			return nil, err
		}
		n.items = append(n.items, item)
	}
	_, err = dec.Token() // the closing delimiter
	return n, err
}

func (n *goldenNode) closer() byte {
	if n.open == '{' {
		return '}'
	}
	return ']'
}

// prefix is what precedes item i: its key inside an object.
func (n *goldenNode) prefix(i int) string {
	if n.open == '{' {
		return n.keys[i] + ": "
	}
	return ""
}

// oneLine renders a container of scalars on one line; ok is false when it
// holds another container.
func (n *goldenNode) oneLine() (string, bool) {
	parts := make([]string, len(n.items))
	for i, item := range n.items {
		if item.open != 0 {
			return "", false
		}
		parts[i] = n.prefix(i) + item.scalar
	}
	return string(n.open) + strings.Join(parts, ", ") + string(n.closer()), true
}

// render writes n starting at column col, with indent as its own line prefix.
func (n *goldenNode) render(b *strings.Builder, indent string, col int) {
	if n.open == 0 {
		b.WriteString(n.scalar)
		return
	}
	if line, ok := n.oneLine(); ok && (len(n.items) == 0 || col+len(line) <= goldenJSONWidth) {
		b.WriteString(line)
		return
	}
	inner := indent + "  "
	b.WriteByte(n.open)
	for i, item := range n.items {
		b.WriteString("\n" + inner + n.prefix(i))
		item.render(b, inner, len(inner)+len(n.prefix(i)))
		if i < len(n.items)-1 {
			b.WriteByte(',')
		}
	}
	b.WriteString("\n" + indent)
	b.WriteByte(n.closer())
}

// goldenDiffLimit caps the diff a failure prints, so that a wholesale change
// does not flood the log.
const goldenDiffLimit = 60

// goldenDiff is a compact line diff of the golden (want) against the current
// output (got): changed lines marked - and +, each run with two lines of
// context, headed by the golden's line number.
func goldenDiff(want, got string) string {
	ops := diffLines(strings.Split(want, "\n"), strings.Split(got, "\n"))
	show := make([]bool, len(ops))
	changed := 0
	for i, op := range ops {
		if op.kind == ' ' {
			continue
		}
		changed++
		for k := max(0, i-2); k <= min(len(ops)-1, i+2); k++ {
			show[k] = true
		}
	}
	var b strings.Builder
	printed := 0
	for i, op := range ops {
		if !show[i] {
			continue
		}
		if printed == goldenDiffLimit {
			fmt.Fprintf(&b, "... (diff truncated: %d changed lines in all)\n", changed)
			break
		}
		if i == 0 || !show[i-1] {
			fmt.Fprintf(&b, "@@ golden line %d @@\n", op.line)
		}
		fmt.Fprintf(&b, "%c %s\n", op.kind, op.text)
		printed++
	}
	return b.String()
}

// diffOp is one line of a diff: kind is ' ' (both), '-' (golden only) or '+'
// (current output only); line is the golden's line number at that point.
type diffOp struct {
	kind byte
	text string
	line int
}

// diffLines is a longest-common-subsequence line diff. The goldens are a few
// hundred lines, so the quadratic table is cheap.
func diffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				lcs[i][j] = lcs[i+1][j+1] + 1
			case lcs[i+1][j] >= lcs[i][j+1]:
				lcs[i][j] = lcs[i+1][j]
			default:
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i], i + 1})
			i++
			j++
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			ops = append(ops, diffOp{'-', a[i], i + 1})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j], i + 1})
			j++
		}
	}
	return ops
}

// routeInventoryFile is the API's route list, one "METHOD /path/{param}" per
// line, pinned by TestRouteInventoryIsBackwardCompatible in internal/api. It
// is read as a file: this package does not import internal/api.
const routeInventoryFile = "../api/testdata/routes.txt"

// routeInventoryShown is how failure messages name the inventory.
const routeInventoryShown = "internal/api/testdata/routes.txt"

func loadRouteInventory(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(routeInventoryFile))
	if err != nil {
		t.Fatalf("read the route inventory %s: %v", routeInventoryShown, err)
	}
	var routes []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			routes = append(routes, line)
		}
	}
	return routes
}

// matchRoute returns the inventory line a request resolves to. isID says
// which path segments carry an id: a template matches when its literal
// segments equal the request's and its {parameters} sit exactly on the id
// segments, so a retargeted path cannot pass by landing on a lookalike route.
func matchRoute(routes []string, method, path string, isID func(string) bool) (string, bool) {
	path, _, _ = strings.Cut(path, "?")
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for _, route := range routes {
		m, tmpl, ok := strings.Cut(route, " ")
		if !ok || m != method {
			continue
		}
		tsegs := strings.Split(strings.Trim(tmpl, "/"), "/")
		if len(tsegs) != len(segs) {
			continue
		}
		match := true
		for i, ts := range tsegs {
			param := strings.HasPrefix(ts, "{") && strings.HasSuffix(ts, "}")
			if param != isID(segs[i]) || (!param && ts != segs[i]) {
				match = false
				break
			}
		}
		if match {
			return route, true
		}
	}
	return "", false
}
