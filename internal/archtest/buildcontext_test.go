package archtest

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// dockerfiles are the Go images built with the repository root as context.
// serverImage must build cmd/server.
var dockerfiles = []string{"Dockerfile.api", "Dockerfile.worker"}

const serverImage = "Dockerfile.api"

// dockerfile is what the build-context rule reads from a Dockerfile: the
// context paths copied to their own module path under the directory where
// go build runs, and the packages go build is asked for.
type dockerfile struct {
	name    string
	copies  []string // module-relative, slash-separated; "." is everything
	targets []string // package names
}

// checkBuildContext checks, for each Go image, that every package its go
// build targets link, and every //go:embed pattern in them, lies inside
// the Dockerfile's COPY list, as does every embed pattern in the module for
// the server image. CI's Go jobs never build the images, so without this a
// new directory, root file or embedded asset breaks only the Docker build.
func checkBuildContext(c *check) {
	for _, name := range dockerfiles {
		data, err := os.ReadFile(filepath.Join(c.m.root, name))
		if err != nil {
			c.violation("%s: %v", name, err)
			continue
		}
		df := parseDockerfile(c.m.root, name, string(data))
		c.baseline("%s copies %s and builds %s", name, strings.Join(df.copies, ", "), strings.Join(df.targets, ", "))
		if name == serverImage && !slices.Contains(df.targets, "cmd/server") {
			c.violation("%s no longer builds cmd/server as this rule reads it (go build targets found: %v)", name, df.targets)
		}
		for _, v := range contextViolations(c.m, df, name == serverImage) {
			c.violation("%s", v)
		}
	}
}

// contextViolations lists what df's build needs but does not copy. With
// allEmbeds, every embed pattern in the module is checked, not only those
// in linked packages.
func contextViolations(m *module, df *dockerfile, allEmbeds bool) []string {
	var out []string
	copied := strings.Join(df.copies, ", ")
	if len(df.targets) == 0 {
		return []string{fmt.Sprintf("%s: no go build targets found", df.name)}
	}
	for _, need := range []string{"go.mod", "go.sum"} {
		if !df.covers(need) {
			out = append(out, fmt.Sprintf("%s does not copy %s (COPY list: %s)", df.name, need, copied))
		}
	}
	checked := map[string]bool{}
	for _, target := range expandTargets(m, df.targets) {
		if p := m.pkgs[target]; p == nil || len(p.files) == 0 {
			out = append(out, fmt.Sprintf("%s builds %s, which is not a package of this module", df.name, target))
			continue
		}
		parent := linked(m.edges, target)
		for _, name := range sortedKeys(parent) {
			if checked[name] {
				continue
			}
			checked[name] = true
			p := m.pkgs[name]
			if p == nil {
				// Not parsed, so its files cannot be checked: fail, never skip.
				out = append(out, fmt.Sprintf("%s builds %s, which links %s (%s), a package outside the root package, cmd/ and internal/, "+
					"which archtest does not read: keep Go code under cmd/ or internal/", df.name, target, name, chain(parent, name)))
				continue
			}
			var missing []string
			for _, f := range p.files {
				if !df.covers(f.rel) {
					missing = append(missing, f.rel)
				}
			}
			if len(missing) > 0 {
				out = append(out, fmt.Sprintf("%s builds %s, which links %s (%s), but %s lies outside its COPY list (%s)",
					df.name, target, name, chain(parent, name), strings.Join(missing, ", "), copied))
			}
			if !allEmbeds {
				out = append(out, df.embedViolations(p.files, copied)...)
			}
		}
	}
	if allEmbeds {
		out = append(out, df.embedViolations(m.production(), copied)...)
	}
	return out
}

// expandTargets replaces a "dir/..." pattern with the packages under dir.
func expandTargets(m *module, targets []string) []string {
	var out []string
	for _, t := range targets {
		dir, ok := strings.CutSuffix(t, "...")
		if !ok {
			out = append(out, t)
			continue
		}
		for _, name := range m.names {
			if len(m.pkgs[name].files) > 0 && strings.HasPrefix(name+"/", dir) {
				out = append(out, name)
			}
		}
	}
	return out
}

func (df *dockerfile) embedViolations(files []*file, copied string) []string {
	var out []string
	for _, f := range files {
		for _, pattern := range embedPatterns(f) {
			if prefix := path.Join(f.pkg.dir, globPrefix(pattern)); !df.covers(prefix) {
				out = append(out, fmt.Sprintf("%s: //go:embed %s resolves to %s, outside the %s COPY list (%s)",
					f.rel, pattern, prefix, df.name, copied))
			}
		}
	}
	return out
}

// covers reports whether a module-relative path lies inside a copied path.
func (df *dockerfile) covers(rel string) bool {
	for _, src := range df.copies {
		if src == "." || rel == src || strings.HasPrefix(rel, src+"/") {
			return true
		}
	}
	return false
}

// embedPatterns returns the patterns of a file's //go:embed directives.
func embedPatterns(f *file) []string {
	var out []string
	for _, cg := range f.ast.Comments {
		for _, cm := range cg.List {
			if rest, ok := strings.CutPrefix(cm.Text, "//go:embed "); ok {
				for _, p := range splitEmbedArgs(rest) {
					out = append(out, strings.TrimPrefix(p, "all:"))
				}
			}
		}
	}
	return out
}

// splitEmbedArgs splits a //go:embed argument list: space-separated
// patterns, each optionally a Go string literal.
func splitEmbedArgs(s string) []string {
	var out []string
	for s = strings.TrimSpace(s); s != ""; s = strings.TrimSpace(s) {
		end := strings.IndexAny(s, " \t")
		if s[0] == '"' || s[0] == '`' {
			end = strings.IndexByte(s[1:], s[0]) + 2
		}
		if end <= 0 || end > len(s) {
			end = len(s)
		}
		arg := s[:end]
		if uq, err := strconv.Unquote(arg); err == nil {
			arg = uq
		}
		out = append(out, arg)
		s = s[end:]
	}
	return out
}

// globPrefix returns the leading path elements of a pattern that hold no
// glob metacharacter.
func globPrefix(pattern string) string {
	var keep []string
	for _, el := range strings.Split(pattern, "/") {
		if strings.ContainsAny(el, `*?[\`) {
			break
		}
		keep = append(keep, el)
	}
	return strings.Join(keep, "/")
}

// parseDockerfile reads COPY/ADD sources from the build context and go
// build targets from RUN lines. A source counts as copied only when it
// lands at its own path under the directory where go build runs.
func parseDockerfile(root, name, content string) *dockerfile {
	df := &dockerfile{name: name}
	type copyOp struct{ src, landing string }
	var ops []copyOp
	buildDir, workdir := "", "/"
	for _, line := range logicalLines(content) {
		instr, rest, _ := strings.Cut(line, " ")
		rest = strings.TrimSpace(rest)
		switch strings.ToUpper(instr) {
		case "FROM":
			workdir = "/"
		case "WORKDIR":
			if path.IsAbs(rest) {
				workdir = path.Clean(rest)
			} else {
				workdir = path.Join(workdir, rest)
			}
		case "COPY", "ADD":
			srcs, dest, ok := copyArgs(rest)
			if !ok {
				continue
			}
			for _, src := range expandSources(root, srcs) {
				ops = append(ops, copyOp{src, landing(root, workdir, src, dest)})
			}
		case "RUN":
			for _, t := range goBuildTargets(rest) {
				if !slices.Contains(df.targets, t) {
					df.targets = append(df.targets, t)
				}
				buildDir = workdir
			}
		}
	}
	for _, op := range ops {
		if op.landing == path.Join(buildDir, op.src) {
			df.copies = append(df.copies, op.src)
		}
	}
	return df
}

// logicalLines drops comments and joins continuation lines.
func logicalLines(content string) []string {
	var out []string
	cur := ""
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, `\`) {
			cur += strings.TrimSuffix(line, `\`) + " "
			continue
		}
		if cur+line != "" {
			out = append(out, strings.TrimSpace(cur+line))
		}
		cur = ""
	}
	return out
}

// copyArgs splits COPY/ADD arguments into sources and destination. It
// reports false for a copy from another stage or image (--from).
func copyArgs(rest string) ([]string, string, bool) {
	var args []string
	for _, a := range strings.Fields(rest) {
		if strings.HasPrefix(a, "--from") {
			return nil, "", false
		}
		if !strings.HasPrefix(a, "--") {
			args = append(args, a)
		}
	}
	if joined := strings.Join(args, " "); strings.HasPrefix(joined, "[") {
		args = nil
		if json.Unmarshal([]byte(joined), &args) != nil {
			return nil, "", false
		}
	}
	if len(args) < 2 {
		return nil, "", false
	}
	return args[:len(args)-1], args[len(args)-1], true
}

// expandSources cleans sources relative to the context and expands globs.
func expandSources(root string, srcs []string) []string {
	var out []string
	for _, s := range srcs {
		s = path.Clean(strings.TrimPrefix(s, "/"))
		if !strings.ContainsAny(s, `*?[`) {
			out = append(out, s)
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(s)))
		for _, mt := range matches {
			if rel, err := filepath.Rel(root, mt); err == nil {
				out = append(out, filepath.ToSlash(rel))
			}
		}
	}
	return out
}

// landing is where a copied source ends up: a directory's contents go into
// dest; a file goes into dest when dest names a directory, else becomes it.
func landing(root, workdir, src, dest string) string {
	abs := dest
	if !path.IsAbs(dest) {
		abs = path.Join(workdir, dest)
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(src)))
	if err == nil && info.IsDir() {
		return abs
	}
	if strings.HasSuffix(dest, "/") || dest == "." {
		return path.Join(abs, path.Base(src))
	}
	return abs
}

// goBuildFlagsWithValue are the go build flags that take the next word.
var goBuildFlagsWithValue = setOf([]string{
	"-o", "-p", "-C", "-tags", "-ldflags", "-gcflags", "-asmflags", "-installsuffix", "-mod", "-modfile",
	"-overlay", "-pkgdir", "-toolexec", "-buildmode", "-compiler", "-gccgoflags", "-pgo", "-covermode", "-coverpkg",
})

// goBuildTargets returns the packages named by go build commands in a RUN
// line, as module package names (a file target names its directory).
func goBuildTargets(run string) []string {
	var out []string
	for _, cmd := range strings.FieldsFunc(run, func(r rune) bool { return r == '&' || r == ';' || r == '|' }) {
		words := shellWords(cmd)
		i := slices.Index(words, "go")
		if i < 0 || i+1 >= len(words) || words[i+1] != "build" {
			continue
		}
		for j := i + 2; j < len(words); j++ {
			w := words[j]
			switch {
			case strings.HasPrefix(w, "-"):
				if goBuildFlagsWithValue[w] {
					j++
				}
			case strings.HasSuffix(w, ".go"):
				out = append(out, pkgNameOfDir(path.Dir(w)))
			default:
				out = append(out, pkgNameOfDir(w))
			}
		}
	}
	return out
}

// shellWords splits a command into words, keeping a single- or
// double-quoted span inside one word and dropping the quotes.
func shellWords(s string) []string {
	var out []string
	var cur strings.Builder
	in, quote := false, rune(0)
	for _, r := range s {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			quote, in = r, true
		case r == ' ' || r == '\t':
			if in {
				out = append(out, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if in {
		out = append(out, cur.String())
	}
	return out
}

func pkgNameOfDir(dir string) string {
	if dir = path.Clean(strings.TrimPrefix(dir, "./")); dir == "." {
		return rootName
	}
	return dir
}

// TestDockerfileParsing pins how the build-context rule reads a Dockerfile,
// and that a linked package or embed pattern outside the COPY list fails, as
// does a linked package outside the directories archtest reads.
func TestDockerfileParsing(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"go.mod":            "module example.com/fixture\n\ngo 1.25\n",
		"go.sum":            "",
		"root.go":           "package fixture\n\nimport _ \"embed\"\n\n//go:embed NOTES.md\nvar notes string\n",
		"NOTES.md":          "notes\n",
		"cmd/app/main.go":   "package main\n\nimport (\n\t_ \"example.com/fixture\"\n\t_ \"example.com/fixture/internal/x\"\n\t_ \"example.com/fixture/misc\"\n)\n\nfunc main() {}\n",
		"cmd/other/main.go": "package main\n\nfunc main() {}\n",
		"internal/x/x.go":   "package x\n\nimport \"embed\"\n\n//go:embed \"data\" all:static/*.json\nvar FS embed.FS\n",
		"misc/tool.go":      "package misc\n",
	})
	content := strings.Join([]string{
		"FROM golang:1 AS build",
		"WORKDIR /src",
		"COPY go.mod go.sum ./",
		"# a comment",
		"COPY --chown=1:1 cmd cmd",
		`COPY ["internal", "internal"]`,
		"COPY misc/tool.go ./",
		"COPY --from=build /out /out",
		"WORKDIR /src",
		`RUN CGO_ENABLED=0 go build -a -o /out/app -tags x \`,
		`    -ldflags "-s -w -X main.version=1" ./cmd/app && go build -ldflags=-s cmd/other/main.go; go build -tags 'a b' ./internal/...`,
	}, "\n")
	df := parseDockerfile(root, "Dockerfile.fixture", content)
	if got, want := strings.Join(df.copies, " "), "go.mod go.sum cmd internal"; got != want {
		t.Errorf("copies = %q, want %q (misc/tool.go lands at /src/tool.go, not its own path; a repeated absolute WORKDIR stays /src)", got, want)
	}
	if got, want := strings.Join(df.targets, " "), "cmd/app cmd/other internal/..."; got != want {
		t.Errorf("targets = %q, want %q (a quoted flag value is one word)", got, want)
	}
	m, err := parseModule(root)
	if err != nil {
		t.Fatal(err)
	}
	got := contextViolations(m, df, false)
	if len(got) != 3 || !strings.Contains(got[0], "links (root) (cmd/app -> (root)), but root.go lies outside") ||
		!strings.Contains(got[1], "root.go: //go:embed NOTES.md resolves to NOTES.md, outside") ||
		!strings.Contains(got[2], "links misc (cmd/app -> misc), a package outside the root package, cmd/ and internal/") {
		t.Errorf("violations = %q, want the uncopied root package, its embed, and misc, which archtest does not read", got)
	}
	if pats := embedPatterns(m.pkgs["internal/x"].files[0]); strings.Join(pats, " ") != "data static/*.json" {
		t.Errorf("embed patterns = %q", pats)
	}
}
