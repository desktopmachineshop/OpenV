package archtest

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// buildPathRecords are the files that may quote a build or run of a single
// file under cmd/, because they record the past rather than instruct: the
// refactor plan, whose M1 row names the commands M1 replaced, and the dated
// assessments, which describe the tree as it was. An entry ending in "/" is
// a directory. Grow it only for such a record, in a class T commit.
var buildPathRecords = []string{
	"docs/plans/codebase-refactor.md",
	"docs/assessments/",
}

// goFileCommands are the go subcommands that compile the files they are
// given, and only those files.
var goFileCommands = setOf([]string{"build", "run", "install"})

// checkBuildByPackagePath fails a README, doc, script, Makefile,
// Dockerfile, compose file or workflow that builds or runs a single .go
// file under cmd/ (M1). Given files, go build and go run compile those
// files alone, so a command that names cmd/server/main.go stops compiling
// once cmd/server is split into several files, and only where it runs: in
// an image CI's Go jobs never build, or in a README step.
func checkBuildByPackagePath(c *check) {
	files, err := buildPathFiles(c.m.root)
	if err != nil {
		c.violation("reading the build instructions: %v", err)
		return
	}
	c.baseline("%d READMEs, docs, scripts, Makefiles, Dockerfiles, compose files and workflows read", len(files))
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(c.m.root, filepath.FromSlash(rel)))
		if err != nil {
			c.violation("%s: %v", rel, err)
			continue
		}
		for _, v := range fileBuildsByPath(string(data)) {
			c.violation("%s:%d: %s", rel, v.line, v.text)
		}
	}
}

// buildPathFiles lists, module-relative and slash-separated, the files the
// rule reads. It skips what the go command skips (testdata, and names
// starting with "." or "_"), except .github at the root, and node_modules.
func buildPathFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			n := d.Name()
			if rel != "." && rel != ".github" &&
				(n == "testdata" || n == "node_modules" || strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if buildPathScope(rel) && !buildPathRecord(rel) {
			out = append(out, rel)
		}
		return nil
	})
	return out, err
}

// buildPathScope reports whether the rule reads a file: a README or doc
// (any *.md, a README* file, a .txt under docs/), a script (*.sh, *.bash,
// *.ps1, anything under scripts/), a Makefile, a Dockerfile, a compose file
// or a workflow (YAML under .github/). A test file is left out: it may
// quote a command as a fixture, and runs none.
func buildPathScope(rel string) bool {
	base := path.Base(rel)
	lower := strings.ToLower(base)
	ext := path.Ext(lower)
	isYAML := ext == ".yml" || ext == ".yaml"
	switch {
	case strings.HasSuffix(strings.TrimSuffix(lower, ext), "_test") || strings.Contains(lower, ".test.") || strings.Contains(lower, ".spec."):
		return false
	case ext == ".md", strings.HasPrefix(lower, "readme"), strings.HasPrefix(rel, "docs/") && ext == ".txt":
		return true
	case ext == ".sh", ext == ".bash", ext == ".ps1", strings.HasPrefix(rel, "scripts/"):
		return true
	case lower == "makefile", lower == "gnumakefile", ext == ".mk":
		return true
	case lower == "dockerfile", strings.HasPrefix(lower, "dockerfile."), ext == ".dockerfile":
		return true
	case isYAML && (strings.HasPrefix(rel, ".github/") || strings.Contains(lower, "compose")):
		return true
	}
	return false
}

func buildPathRecord(rel string) bool {
	for _, r := range buildPathRecords {
		if rel == r || strings.HasSuffix(r, "/") && strings.HasPrefix(rel, r) {
			return true
		}
	}
	return false
}

// pathBuild is one command that builds or runs files under cmd/.
type pathBuild struct {
	line int
	text string
}

// fileBuildsByPath finds the go build, go run and go install commands in a
// text that name a .go file under cmd/. Lines ending in a backslash are
// joined first; then shellCommands reads each line.
func fileBuildsByPath(content string) []pathBuild {
	var out []pathBuild
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		start, line := i, strings.TrimRight(lines[i], " \t\r")
		for strings.HasSuffix(line, `\`) && i+1 < len(lines) {
			i++
			line = strings.TrimSuffix(line, `\`) + " " + strings.TrimRight(lines[i], " \t\r")
		}
		for _, words := range shellCommands(line) {
			for _, b := range singleFileBuilds(words) {
				out = append(out, pathBuild{start + 1, b})
			}
		}
	}
	return out
}

// shellCommands splits a line into commands, each a list of words, roughly
// as a shell would: unquoted blanks end a word; unquoted ; & | ( ) and the
// backtick end a command, the backtick so that a Markdown code span is a
// command of its own; quotes group. A quoted span holding a blank, or one
// left open at the end of the line (an apostrophe in prose), is read again
// as a line of its own, so the commands inside sh -c '...' count. Brackets,
// braces and commas around a word are dropped, so an argument list such as
// Python's ["go", "build", ...] reads as the command it runs.
func shellCommands(line string) [][]string {
	var out, nested [][]string
	var cmd []string
	var word, span strings.Builder
	inWord, quote := false, rune(0)
	endWord := func() {
		if w := strings.Trim(word.String(), "[]{},"); inWord && w != "" {
			cmd = append(cmd, w)
		}
		word.Reset()
		inWord = false
	}
	endCommand := func() {
		endWord()
		if len(cmd) > 0 {
			out = append(out, cmd)
		}
		cmd = nil
	}
	for _, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
			if strings.ContainsAny(span.String(), " \t") {
				nested = append(nested, shellCommands(span.String())...)
			}
		case quote != 0:
			word.WriteRune(r)
			span.WriteRune(r)
		case r == '"' || r == '\'':
			quote, inWord = r, true
			span.Reset()
		case r == ' ' || r == '\t':
			endWord()
		case strings.ContainsRune("`;&|()", r):
			endCommand()
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		nested = append(nested, shellCommands(span.String())...)
	}
	endCommand()
	return append(out, nested...)
}

// singleFileBuilds describes each go build, run or install in words that
// names a .go file under cmd/. A go run stops at its package: the words
// after it are the program's arguments.
func singleFileBuilds(words []string) []string {
	var out []string
	for i := 0; i+1 < len(words); i++ {
		sub := words[i+1]
		if path.Base(words[i]) != "go" || !goFileCommands[sub] {
			continue
		}
		var files []string
		for j := i + 2; j < len(words); j++ {
			w := words[j]
			if strings.HasPrefix(w, "-") {
				flag := "-" + strings.TrimLeft(w, "-")
				if !strings.Contains(flag, "=") && (goBuildFlagsWithValue[flag] || sub == "run" && flag == "-exec") {
					j++
				}
				continue
			}
			if !strings.HasSuffix(w, ".go") {
				if sub == "run" {
					break
				}
				continue
			}
			if f, ok := underCmd(w); ok {
				files = append(files, f)
			}
		}
		if len(files) > 0 {
			out = append(out, fmt.Sprintf("go %s %s: builds by file path, which compiles only the files named, not their package; name the package instead: go %s ./%s",
				sub, strings.Join(files, " "), sub, path.Dir(files[0])))
		}
	}
	return out
}

// underCmd returns a file argument cleaned, with slashes, from its cmd/
// directory on, and whether it lies under one.
func underCmd(arg string) (string, bool) {
	f := path.Clean(strings.ReplaceAll(arg, `\`, "/"))
	if strings.HasPrefix(f, "cmd/") {
		return f, true
	}
	if k := strings.Index(f, "/cmd/"); k >= 0 {
		return f[k+1:], true
	}
	return "", false
}

// TestBuildByPackagePath proves the build-by-package-path rule on a
// fixture: each kind of file it reads, a command split over lines, in a
// code span (after an apostrophe too), in quotes, inside sh -c '...' or as
// an argument list fails; a package path, a go
// run's own arguments, a file outside cmd/, a test fixture, a record and
// the directories the rule skips pass.
func TestBuildByPackagePath(t *testing.T) {
	root := writeFixture(t, map[string]string{
		"Dockerfile.api": "FROM golang:1 AS build\nRUN CGO_ENABLED=0 go build -a -installsuffix cgo \\\n" +
			"    -o server cmd/server/main.go\nRUN go build -o /out/agentd ./cmd/agentd\n",
		"Dockerfile.worker": "RUN go build -ldflags \"-s -w\" -o /out/x ./cmd/x && go build -tags 'a b' ./cmd/y\n",
		"Makefile": "run:\n\tgo run ./cmd/openv-vapid\nbuild:\n\tgo build -o bin/x cmd/x/main.go cmd/x/flags.go\n" +
			"win:\n\tdocker run -v \"$(CURDIR):/app\" img sh -c 'GOOS=windows go build -o bin/y.exe cmd/y/main.go && go build ./cmd/z'\n",
		"README.md": "Run it:\n\n```bash\ngo run cmd/server/main.go\n```\n\nor `go run ./cmd/server`.\n",
		"docs/guide.md": "Locally, `go run -tags dev ./cmd/server/main.go` starts it; go build things as you like.\n" +
			"The server's own `go run cmd/server/main.go` and the worker's `go run ./cmd/agentd` differ.\n",
		"docs/notes.txt":  "/usr/local/go/bin/go install cmd\\tool\\main.go\n",
		"frontend/README": "go run ./cmd/server -config cmd/server/dev.go\n",
		".github/workflows/ci.yml": "jobs:\n  b:\n    steps:\n      - run: go vet cmd/server/main.go\n" +
			"      - run: go build -o /tmp/s cmd/server/main.go; /tmp/s\n",
		"docker-compose.yml":                    "services:\n  api:\n    command: [\"go\", \"run\", \"cmd/server/main.go\"]\n",
		"scripts/dev.sh":                        "exec go run ./cmd/server \"$@\"\ngo run internal/tools/gen.go\n",
		"scripts/build.py":                      "subprocess.run([\"go\", \"build\", \"-o\", \"bin/x\", \"cmd/x/main.go\"], check=True)\n",
		"scripts/build_test.py":                 "FIXTURE = \"RUN go build -o server cmd/server/main.go\"\n",
		"internal/x/testdata/Makefile":          "\tgo build cmd/x/main.go\n",
		"frontend/node_modules/p/README.md":     "go run cmd/p/main.go\n",
		".claude/notes.md":                      "go run cmd/server/main.go\n",
		"docs/plans/codebase-refactor.md":       "from `go run cmd/server/main.go` to `go run ./cmd/server`\n",
		"docs/assessments/2026-01-01/README.md": "the API runs with `go run cmd/server/main.go`\n",
		"internal/tools/README.md":              "go run ./internal/tools/declhash -base origin/master cmd/server\n",
		"cmd/server/main.go":                    "package main\n\n// go run cmd/server/main.go\nfunc main() {}\n",
	})
	files, err := buildPathFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range fileBuildsByPath(string(data)) {
			got = append(got, fmt.Sprintf("%s:%d: %s", rel, v.line, v.text))
		}
	}
	want := []string{
		".github/workflows/ci.yml:5: go build cmd/server/main.go: builds by file path",
		"Dockerfile.api:2: go build cmd/server/main.go: builds by file path",
		"Makefile:4: go build cmd/x/main.go cmd/x/flags.go: builds by file path",
		"Makefile:6: go build cmd/y/main.go: builds by file path",
		"README.md:4: go run cmd/server/main.go: builds by file path",
		"docker-compose.yml:3: go run cmd/server/main.go: builds by file path",
		"docs/guide.md:1: go run cmd/server/main.go: builds by file path",
		"docs/guide.md:2: go run cmd/server/main.go: builds by file path",
		"docs/notes.txt:1: go install cmd/tool/main.go: builds by file path",
		"scripts/build.py:1: go build cmd/x/main.go: builds by file path",
	}
	if len(got) != len(want) {
		t.Fatalf("found %d file-path builds, want %d:\n  %s", len(got), len(want), strings.Join(got, "\n  "))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("finding %d = %q, want it to start %q", i, got[i], want[i])
		}
	}
	if !strings.HasSuffix(got[1], "name the package instead: go build ./cmd/server") ||
		!strings.HasSuffix(got[8], "name the package instead: go install ./cmd/tool") {
		t.Errorf("the fix names the package: %q, %q", got[1], got[8])
	}
}
