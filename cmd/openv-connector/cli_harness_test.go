package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The CLI snapshot harness (refactor plan S8, invariant I14): every command
// under cmd/ but the server carries this file, the same in each apart from
// its package clause (TestCLIHarnessCopies fails when a copy differs), and a
// cli_test.go whose TestCLI names the command (cliName) and its scenarios. It builds the real binary with go build, runs
// each scenario twice with an empty environment but for fresh HOME and
// XDG_CONFIG_HOME directories and what the scenario sets, and empty stdin
// unless it gives one, and compares what it printed and its exit status,
// normalised, with testdata/cli/<scenario>.txt.

// TestCLIHarnessCopies keeps every cmd/*/cli_harness_test.go the same below
// its package clause, so a fix to one copy reaches them all.
func TestCLIHarnessCopies(t *testing.T) {
	copies, err := filepath.Glob(filepath.Join("..", "*", "cli_harness_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	var ref, refFile string
	for _, c := range copies {
		src, err := os.ReadFile(c)
		if err != nil {
			t.Fatal(err)
		}
		_, body, _ := strings.Cut(string(src), "\n")
		switch {
		case ref == "":
			ref, refFile = body, c
		case body != ref:
			t.Errorf("%s differs from %s below its package clause: keep every copy the same", c, refFile)
		}
	}
}

// cliScenario is one run of the command and the golden it writes.
type cliScenario struct {
	name  string   // the golden, testdata/cli/<name>.txt
	args  []string // after the command's name
	env   []string // KEY=VALUE beside HOME, USERPROFILE and XDG_CONFIG_HOME
	stdin string
	// setup prepares the run's fresh home and config directories.
	setup func(t *testing.T, home, config string)
	// normalise checks and replaces what differs between runs by design
	// (a generated key); the two runs' raw output must then differ.
	normalise func(t *testing.T, stdout string) string
}

// cliTimeout bounds one run; every scenario exits on its own well before.
const cliTimeout = time.Minute

var (
	cliTimestamp = regexp.MustCompile(`(?m)^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} `)
	cliBareArg   = regexp.MustCompile(`^[A-Za-z0-9_./:=,+-]+$`)
)

// runCLI builds the command once and checks every scenario.
func runCLI(t *testing.T, scenarios []cliScenario) {
	if runtime.GOOS == "windows" {
		t.Skip("the snapshots are of the Unix command line: on Windows paths differ and openv-connector writes the registry")
	}
	bin := cliBuild(t)
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			first, raw1 := cliRun(t, bin, sc)
			second, raw2 := cliRun(t, bin, sc)
			if first != second {
				t.Fatalf("two runs printed different output after normalising, so the snapshot cannot hold:\n%s\n---\n%s", first, second)
			}
			if sc.normalise != nil && raw1 == raw2 {
				t.Fatalf("two runs printed the same output, which this scenario generates afresh each time:\n%s", raw1)
			}
			cliCheckGolden(t, filepath.Join("testdata", "cli", sc.name+".txt"), first)
		})
	}
}

// cliBuild builds ./cmd/<cliName> from the module root into a directory of
// its own, so nothing else lies next to it.
func cliBuild(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	for ; err == nil; dir = filepath.Dir(dir) {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			break
		}
		if filepath.Dir(dir) == dir {
			err = errors.New("no go.mod above the test's directory")
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	gotool := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(gotool); err != nil {
		gotool = "go"
	}
	bin := filepath.Join(t.TempDir(), cliName)
	cmd := exec.Command(gotool, "build", "-o", bin, "./cmd/"+cliName)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/%s: %v\n%s", cliName, err, out)
	}
	return bin
}

// cliRun runs one scenario in fresh directories and renders the golden.
func cliRun(t *testing.T, bin string, sc cliScenario) (golden, rawStdout string) {
	t.Helper()
	tmp := t.TempDir()
	home, config := filepath.Join(tmp, "home"), filepath.Join(tmp, "config")
	for _, d := range []string{home, config} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if sc.setup != nil {
		sc.setup(t, home, config)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cliTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, sc.args...)
	cmd.Args[0] = cliName
	cmd.Dir = tmp
	cmd.Env = append([]string{"HOME=" + home, "USERPROFILE=" + home, "XDG_CONFIG_HOME=" + config}, sc.env...)
	cmd.Stdin = strings.NewReader(sc.stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	exit := 0
	var ee *exec.ExitError
	if err := cmd.Run(); errors.As(err, &ee) && ctx.Err() == nil {
		exit = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run %s %q: %v", cliName, sc.args, err)
	}
	var paths []string // longest first, each as given and with symlinks resolved
	for _, p := range [][2]string{{config, "$XDG_CONFIG_HOME"}, {home, "$HOME"}, {filepath.Dir(bin), "$BINDIR"}, {tmp, "$TMP"}} {
		paths = append(paths, p[0], p[1])
		if real, err := filepath.EvalSymlinks(p[0]); err == nil && real != p[0] {
			paths = append(paths, real, p[1])
		}
	}
	normal := func(s string) string {
		s = strings.NewReplacer(paths...).Replace(cliTimestamp.ReplaceAllString(s, ""))
		for _, root := range []string{filepath.Dir(tmp), filepath.Dir(filepath.Dir(bin))} {
			if strings.Contains(s, root) {
				t.Errorf("a temporary path survived normalising:\n%s", s)
			}
		}
		return s
	}
	out := normal(stdout.String())
	if sc.normalise != nil {
		out = sc.normalise(t, out)
	}
	return cliRender(sc, exit, out, normal(stderr.String())), stdout.String()
}

// cliRender writes a golden: a header, the command, what it was given, its
// exit status and both streams.
func cliRender(sc cliScenario, exit int, stdout, stderr string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# The command line of %s (refactor plan S8, invariant I14), written by\n"+
		"# TestCLI in cmd/%s/cli_test.go; regenerate with\n"+
		"#   UPDATE_GOLDEN=1 go test ./cmd/%s -count=1 -run '^TestCLI$'\n"+
		"# (only UPDATE_GOLDEN=1 regenerates; any other value compares).\n"+
		"# One run with an empty environment but for the env lines below and fresh\n"+
		"# $HOME and $XDG_CONFIG_HOME directories; $BINDIR is the binary's\n"+
		"# directory and $TMP the working directory.\n", cliName, cliName, cliName)
	b.WriteString("$ " + cliName)
	for _, a := range sc.args {
		if cliBareArg.MatchString(a) {
			b.WriteString(" " + a)
		} else {
			b.WriteString(" " + strconv.Quote(a))
		}
	}
	b.WriteString("\n")
	for _, kv := range sc.env {
		k, v, _ := strings.Cut(kv, "=")
		fmt.Fprintf(&b, "env %s=%s\n", k, strconv.Quote(v))
	}
	stream := func(name, s string) {
		b.WriteString("--- " + name + "\n" + s)
		if s != "" && !strings.HasSuffix(s, "\n") {
			b.WriteString("\n\\ no newline at end\n")
		}
	}
	if sc.stdin != "" {
		stream("stdin", sc.stdin)
	}
	fmt.Fprintf(&b, "exit %d\n", exit)
	stream("stdout", stdout)
	stream("stderr", stderr)
	return b.String()
}

// cliCheckGolden compares got with the golden at path, or rewrites it when
// UPDATE_GOLDEN is exactly 1.
func cliCheckGolden(t *testing.T, path, got string) {
	t.Helper()
	shown := "cmd/" + cliName + "/" + filepath.ToSlash(path)
	regenerate := "UPDATE_GOLDEN=1 go test ./cmd/" + cliName + " -count=1 -run '^TestCLI$'\n" +
		"(only UPDATE_GOLDEN=1 regenerates; any other value compares)"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("regenerated %s", shown)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v\nCreate it with:\n  %s", shown, err, regenerate)
	}
	if string(want) != got {
		t.Errorf("golden %s does not match the command (- golden, + now):\n%s"+
			"A refactor never changes a golden: a renamed, dropped or added flag, subcommand or message is a behavior change."+
			" If it is deliberate, regenerate it with:\n  %s", shown, cliDiff(string(want), got), regenerate)
	}
}

// cliDiff is a longest-common-subsequence line diff of two small texts,
// with one line of context around each change, at most 40 lines.
func cliDiff(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var ops []string
	for i, j := 0, 0; i < len(a) || j < len(b); {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			ops, i, j = append(ops, "  "+a[i]), i+1, j+1
		case j < len(b) && (i == len(a) || lcs[i][j+1] >= lcs[i+1][j]):
			ops, j = append(ops, "+ "+b[j]), j+1
		default:
			ops, i = append(ops, "- "+a[i]), i+1
		}
	}
	var out []string
	for k, op := range ops {
		near := op[0] != ' ' || (k > 0 && ops[k-1][0] != ' ') || (k+1 < len(ops) && ops[k+1][0] != ' ')
		if near && len(out) < 40 {
			out = append(out, op)
		}
	}
	return strings.Join(out, "\n") + "\n"
}
