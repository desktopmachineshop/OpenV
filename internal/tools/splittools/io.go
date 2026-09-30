package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// writeAll writes the table's file and the new files. A new file must not
// exist (checkNames made sure); if a write fails, every file is restored.
func writeAll(dir string, files map[string][]byte, written []string, table *goFile) error {
	for i, name := range written {
		if err := os.WriteFile(filepath.Join(dir, name), files[name], 0o644); err != nil {
			restore(dir, written[:i+1], table)
			return fmt.Errorf("write %s: %w; every file is restored", name, err)
		}
	}
	return nil
}

// restore puts the table's file back and removes the new files.
func restore(dir string, written []string, table *goFile) {
	for _, name := range written {
		p := filepath.Join(dir, name)
		if name == table.name {
			_ = os.WriteFile(p, table.src, 0o644)
		} else {
			_ = os.Remove(p)
		}
	}
}

// checkCmd compares the package at base with the head side: Tools()'s
// entries, and everything outside them (sameOutside). It prints the head
// side's tool-to-constructor map.
func checkCmd(dir, fn, base, head string, stdout, stderr io.Writer) int {
	load := func(ref string) (*pkg, error) {
		var srcs []source
		var err error
		if ref == "" {
			srcs, err = readDir(dir)
		} else {
			srcs, err = readGitDir(ref, dir)
		}
		if err != nil {
			return nil, err
		}
		if len(srcs) == 0 {
			side := "the working tree"
			if ref != "" {
				side = ref
			}
			return nil, fmt.Errorf("%s holds no Go files at %s; name the package directory (./... is not expanded)", dir, side)
		}
		return parsePkg(srcs)
	}
	flatAt := func(ref string) (*pkg, []flat, error) {
		p, err := load(ref)
		if err != nil {
			return nil, nil, err
		}
		l, err := flatten(p, fn)
		return p, l, err
	}
	bp, b, err := flatAt(base)
	if err != nil {
		fmt.Fprintf(stderr, "splittools: base %s: %v\n", base, err)
		return 2
	}
	hp, h, err := flatAt(head)
	if err != nil {
		side := head
		if side == "" {
			side = "working tree"
		}
		fmt.Fprintf(stderr, "splittools: head (%s): %v\n", side, err)
		return 2
	}
	for _, e := range h {
		fmt.Fprintf(stdout, "%s -> %s (%s)\n", e.name, e.fn, e.file)
	}
	entries, outside := compareFlat(b, h), sameOutside(bp, hp, fn, b, h)
	for _, d := range append(entries, outside...) {
		fmt.Fprintln(stdout, "splittools: "+d)
	}
	if len(entries) > 0 {
		fmt.Fprintf(stdout, "splittools: %s() differs from %s: not a faithful split\n", fn, base)
	}
	if len(outside) > 0 {
		fmt.Fprintf(stdout, "splittools: the package differs from %s outside %s()'s entries: not a faithful split\n", base, fn)
	}
	if len(entries)+len(outside) > 0 {
		return 1
	}
	fmt.Fprintf(stdout, "splittools: %s() returns the same %d entries in the same order as at %s, each unchanged; "+
		"every other declaration of the package, and %s()'s doc comment and signature, are as they were\n", fn, len(h), base, fn)
	return 0
}

// readGitDir reads the Go files of a package directory as they are at a git
// ref, without touching the working tree. The directory need not exist in
// the working tree; the nearest one above it that does locates the repository.
func readGitDir(ref, dir string) ([]source, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	start := abs
	for {
		if st, err := os.Stat(start); err == nil && st.IsDir() {
			break
		}
		start = filepath.Dir(start)
	}
	prefix, err := runGit(start, nil, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, err
	}
	rest, err := filepath.Rel(start, abs)
	if err != nil {
		return nil, err
	}
	rel := path.Join(strings.TrimSpace(string(prefix)), filepath.ToSlash(rest))
	args := []string{"ls-tree", "-z", "--full-tree", ref}
	if rel != "." && rel != "" {
		args = append(args, "--", rel+"/")
	}
	listing, err := runGit(start, nil, args...)
	if err != nil {
		return nil, err
	}
	var srcs []source
	for _, entry := range strings.Split(string(listing), "\x00") {
		meta, p, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || fields[1] != "blob" || !isGoFile(path.Base(p)) || path.Dir(p) != rel {
			continue
		}
		blob, err := runGit(start, nil, "cat-file", "blob", fields[2])
		if err != nil {
			return nil, err
		}
		srcs = append(srcs, source{path.Base(p), blob})
	}
	return srcs, nil
}

func runGit(dir string, stdin io.Reader, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-c", "core.quotePath=false"}, args...)...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
