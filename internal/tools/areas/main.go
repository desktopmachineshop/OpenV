// Command areas answers from the area index, docs/areas.json (refactor plan
// step N1, convention K15), which area a file belongs to.
//
//	go run ./internal/tools/areas which <path>
//
// which prints the name of the one area whose globs match path and exits 0.
// When no area claims path, or more than one does, it names the path and
// the areas with their globs on stderr and exits 1; a usage error, or a path
// outside the repository, exits 2. The path is taken relative to the
// current directory, or is absolute, and need not exist yet, so which also
// answers where a new file would belong. The index is found by walking up
// from the current directory.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const usage = "usage: go run ./internal/tools/areas which <path>"

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "areas:", err)
		os.Exit(2)
	}
	os.Exit(run(os.Args[1:], cwd, os.Stdout, os.Stderr))
}

func run(args []string, cwd string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] != "which" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	root, err := findRoot(cwd)
	if err != nil {
		fmt.Fprintln(stderr, "areas:", err)
		return 2
	}
	idx, err := LoadIndex(filepath.Join(root, IndexFile))
	if err != nil {
		fmt.Fprintln(stderr, "areas:", err)
		return 2
	}
	path, err := repoPath(root, cwd, args[1])
	if err != nil {
		fmt.Fprintln(stderr, "areas:", err)
		return 2
	}
	owner, err := idx.Owner(path)
	if err != nil {
		fmt.Fprintln(stderr, "areas:", err)
		return 1
	}
	fmt.Fprintln(stdout, owner)
	return 0
}

// findRoot walks up from dir to the repository root: the first directory
// that holds the area index.
func findRoot(dir string) (string, error) {
	for d := filepath.Clean(dir); ; {
		if _, err := os.Stat(filepath.Join(d, filepath.FromSlash(IndexFile))); err == nil {
			return d, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", fmt.Errorf("no %s in %s or above it; run inside the repository", IndexFile, dir)
		}
		d = parent
	}
}

// repoPath turns path, absolute or relative to cwd, into a path relative to
// the repository root with forward slashes.
func repoPath(root, cwd, path string) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, path)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the repository (%s)", path, root)
	}
	if rel == "." {
		return "", fmt.Errorf("%s is the repository root, not a file", path)
	}
	return filepath.ToSlash(rel), nil
}
