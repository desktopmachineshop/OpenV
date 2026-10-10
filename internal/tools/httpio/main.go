// Command httpio is refactor step X1's codemod
// (docs/plans/codebase-refactor.md §6.7): it swaps the exact statement
// sequences internal/api writes JSON responses and decodes JSON requests
// with, one for one, for the helpers X1a adds to respond.go.
//
//	go run ./internal/tools/httpio [-n] [-area <name>[,<name>...]] <package dir or .go file>...
//
// scripts/refactor/httpio.sh <area>... runs it on internal/api for the
// areas of docs/areas.json an X1 pull request rewrites (X1a-X1h). With W an http.ResponseWriter parameter, R an
// *http.Request parameter, and `_ =` before an encode allowed:
//
//	W.Header().Set("Content-Type", "application/json")
//	W.WriteHeader(X)
//	json.NewEncoder(W).Encode(V)              -> writeJSON(W, X, V)
//
//	W.Header().Set("Content-Type", "application/json")
//	json.NewEncoder(W).Encode(V)              -> writeJSONOK(W, V)
//
//	W.WriteHeader(X)
//	json.NewEncoder(W).Encode(V)              -> writeJSONBareStatus(W, X, V)
//
//	json.NewEncoder(W).Encode(V)              -> writeJSONBare(W, V)
//
//	if err := json.NewDecoder(R.Body).Decode(P); err != nil {
//		writeJSONError(W, http.StatusBadRequest, "invalid request body")
//		return ...
//	}                                         -> if !decodeJSON(W, R, P) { return ... }
//
// and the same decode answered 400 with another literal message becomes
// decodeJSONMsg(W, R, P, "<message>"). Any other "invalid request body"
// literal becomes the constant invalidRequestBody, whose value it is. helpers.go holds each helper's
// contract, which the rewrite relies on: it makes the same calls on the
// writer, in the same order, as the statements it replaces.
//
// A sequence matches only as a whole and only in place: the statements are
// consecutive in one block, nothing is reordered, and the replacement
// takes their place. It is left as it is, and named with the reason, when
// the Content-Type is not exactly application/json, when an earlier
// statement on every path to it (in its block or an enclosing one of the
// same function) sets the Content-Type, writes the status or the body (a
// helper there would misname the response), when an argument that would now
// be evaluated before the header or status is written reads the writer or
// calls a function value, when a comment sits inside the sequence, in an
// SSE handler (one that sets text/event-stream), and in the helpers
// themselves. Every json.NewEncoder call outside respond.go and every
// "invalid request body" literal, what the raw_json_encodes and
// invalid_request_body_literals ratchets count, is printed as rewritten or
// left with its reason. A file that no longer uses encoding/json drops its
// import; each changed file is gofmt'd. A second run finds nothing to do.
//
// Before writing, it checks its result: each changed file parses, finds
// nothing more to rewrite, keeps every site it left, and calls each helper
// as often as it rewrote to it; and each package it writes to declares the
// helpers it calls exactly as helpers.go does (X1a adds them), so it never
// writes code that would not build or that means something else. -n prints
// the plan, with a summary per area, and writes nothing.
//
// Exit status: 0 done (sites left as they are included), 1 refused (the
// helpers are missing or differ, or the self-check failed), 2 a usage,
// read or parse error.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// errRefuse marks a refusal: writing would break or change the package.
var errRefuse = errors.New("refused")

// fileResult is one file: its sites and what the rewrite makes of it.
type fileResult struct {
	path  string
	dir   string
	area  string
	src   []byte
	out   []byte
	sites []site
}

func run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("httpio", flag.ContinueOnError)
	fl.SetOutput(stderr)
	dry := fl.Bool("n", false, "print the plan and write nothing")
	area := fl.String("area", "", "rewrite only the files of these areas of "+areasFile+" (comma-separated)")
	fl.Usage = func() {
		fmt.Fprintln(stderr, "usage: go run ./internal/tools/httpio [-n] [-area <name>[,<name>...]] <package dir or .go file>...")
	}
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if fl.NArg() == 0 {
		fl.Usage()
		return 2
	}
	paths, err := collect(fl.Args())
	if err != nil {
		fmt.Fprintln(stderr, "httpio:", err)
		return 2
	}
	idx, err := findAreas(".")
	if err != nil {
		fmt.Fprintln(stderr, "httpio:", err)
		return 2
	}
	var areas map[string]bool
	if *area != "" {
		areas = map[string]bool{}
		for _, a := range strings.Split(*area, ",") {
			if idx == nil || !idx.has(a) {
				fmt.Fprintf(stderr, "httpio: no area %q in %s\n", a, areasFile)
				return 2
			}
			areas[a] = true
		}
	}
	results, err := analyzeAll(paths, idx, areas)
	if err != nil {
		fmt.Fprintln(stderr, "httpio:", err)
		if errors.Is(err, errRefuse) {
			return 1
		}
		return 2
	}
	if !*dry {
		if err := checkPackages(results); err != nil {
			fmt.Fprintln(stderr, "httpio:", err)
			return 1
		}
	}
	report(stdout, results, anyArea(results))
	if *dry {
		return 0
	}
	for _, r := range results {
		if string(r.out) == string(r.src) {
			continue
		}
		info, err := os.Stat(r.path)
		if err == nil {
			err = os.WriteFile(r.path, r.out, info.Mode().Perm())
		}
		if err != nil {
			fmt.Fprintln(stderr, "httpio:", err)
			return 2
		}
	}
	return 0
}

// collect expands the arguments: a directory to its non-test .go files (not
// recursively), a .go file to itself. A path with no Go source is an error,
// so a mistyped path never passes by rewriting nothing.
func collect(args []string) ([]string, error) {
	var out []string
	for _, a := range args {
		info, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if !strings.HasSuffix(a, ".go") {
				return nil, fmt.Errorf("%s is not a Go file", a)
			}
			out = append(out, filepath.Clean(a))
			continue
		}
		matches, err := filepath.Glob(filepath.Join(a, "*.go"))
		if err != nil {
			return nil, err
		}
		n := 0
		for _, m := range matches {
			if !strings.HasSuffix(m, "_test.go") {
				out = append(out, m)
				n++
			}
		}
		if n == 0 {
			return nil, fmt.Errorf("%s holds no Go source", a)
		}
	}
	sort.Strings(out)
	return out, nil
}

// analyzeAll reads, analyzes and rewrites each file of the areas (every
// file when areas is nil), and checks each result.
func analyzeAll(paths []string, idx *areaIndex, areas map[string]bool) ([]*fileResult, error) {
	var results []*fileResult
	for _, p := range paths {
		r := &fileResult{path: filepath.ToSlash(p), dir: filepath.Dir(p)}
		if idx != nil {
			r.area = idx.owner(p)
		}
		if areas != nil && !areas[r.area] {
			continue
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		fi, err := parseFile(r.path, src)
		if err != nil {
			return nil, err
		}
		r.src, r.sites = src, fi.analyze()
		if r.out, err = rewrite(r.path, src, r.sites); err != nil {
			return nil, fmt.Errorf("%w: %v", errRefuse, err)
		}
		if err := selfCheck(r); err != nil {
			return nil, fmt.Errorf("%w: %s: %v", errRefuse, r.path, err)
		}
		results = append(results, r)
	}
	return results, nil
}

// anyArea reports whether some file belongs to an area, which makes the
// report's summary per area worth printing.
func anyArea(results []*fileResult) bool {
	for _, r := range results {
		if r.area != "" {
			return true
		}
	}
	return false
}

// checkPackages requires each package the rewrite writes to to declare the
// helpers it calls as helpers.go does.
func checkPackages(results []*fileResult) error {
	need := map[string]map[string]bool{}
	for _, r := range results {
		for _, s := range r.sites {
			if s.helper != "" {
				if need[r.dir] == nil {
					need[r.dir] = map[string]bool{}
				}
				need[r.dir][s.helper] = true
			}
		}
	}
	dirs := make([]string, 0, len(need))
	for d := range need {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		problems, err := checkHelpers(d, need[d])
		if err != nil {
			return err
		}
		if len(problems) > 0 {
			return fmt.Errorf("%w: %s: %s; X1a adds the helpers to respond.go, from internal/tools/httpio/helpers.go, before any area is rewritten",
				errRefuse, filepath.ToSlash(d), strings.Join(problems, "; "))
		}
	}
	return nil
}
