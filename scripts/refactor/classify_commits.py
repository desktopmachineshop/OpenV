#!/usr/bin/env python3
"""Classify commits by the layers they touch, and measure hub-touch rates.

The refactor plan (docs/plans/codebase-refactor.md, A§9) measures how
changes spread: how many layers a commit touches, how many files, and how
often it edits one of the five hub files every kind of work passes through.
This script takes those measurements over any range of commits, so the same
numbers can be compared before and after the refactor.

Usage:
  python3 scripts/refactor/classify_commits.py [-n N] [--first-parent] [--json] [--files] [REV...]

REV arguments are passed to `git log` (default HEAD), so `origin/master`,
`-n 50 origin/master` or `v0.14.0..v0.15.0` all work. Merge commits are
skipped and their commits classified one by one; with --first-parent each
merge is one unit, classified by its diff against its first parent.

Layers, first match wins: release notes, docs, tests, migration/schema,
repository, route registration (internal/api/routes.go, or a diff line in
internal/api with HandleFunc), handler, domain, main wiring (cmd/server),
services (other Go), tooling, frontend client (src/api), types (type-only
modules, or a diff line declaring an interface or type in frontend/src),
CSS, views/components, frontend other, other. Route registration and types
are also counted when a commit's diff lines show them, whatever the file.

Standard library only; run the tests with
  python3 -m unittest scripts/refactor/classify_commits_test.py
"""

import argparse
import fnmatch
import json
import re
import statistics
import subprocess
import sys

HUBS = [
    "internal/api/handlers.go",
    "cmd/server/main.go",
    "internal/persistence/postgres/migrations.go",
    "frontend/src/api/client.ts",
    "frontend/src/App.tsx",
]

LAYERS = [
    "release notes",
    "docs",
    "tests",
    "migration/schema",
    "repository",
    "route registration",
    "handler",
    "domain",
    "main wiring",
    "services",
    "tooling",
    "frontend client",
    "types",
    "CSS",
    "views/components",
    "frontend other",
    "other",
]

# (layer, glob patterns); fnmatch's * also matches "/", so "a/*" is a subtree.
RULES = [
    ("release notes", ["RELEASE_NOTES.md"]),
    ("tests", [
        "*_test.go", "*.test.ts", "*.test.tsx", "*.spec.ts", "*.spec.tsx", "*_test.py", "test_*.py",
        "e2e/*", "*/testdata/*", "*/__snapshots__/*", "frontend/src/test/*", "frontend/src/setupTests.ts",
    ]),
    ("docs", ["*.md", "docs/*", "examples/*"]),
    ("migration/schema", [
        "internal/persistence/postgres/migrations.go", "internal/persistence/postgres/migration_*.go",
        "internal/persistence/postgres/migrate_*.go", "internal/persistence/postgres/schema*.go",
    ]),
    ("repository", ["internal/persistence/*"]),
    ("route registration", ["internal/api/routes.go"]),
    ("handler", ["internal/api/*"]),
    ("domain", ["internal/domain/*"]),
    ("main wiring", ["cmd/server/*"]),
    ("services", ["*.go"]),
    ("tooling", [
        ".github/*", "Makefile", "go.mod", "go.sum", "Dockerfile*", "docker-compose*", "scripts/*", "frontend/scripts/*",
        "frontend/Dockerfile*", "frontend/*.config.*", "frontend/eslint.config.js", "frontend/package*.json",
        "frontend/tsconfig.json", ".claude/*", ".mcp.json", ".gitignore",
    ]),
    ("types", ["frontend/src/types/*", "frontend/src/api/types/*", "frontend/src/*.d.ts", "frontend/src/generated/*"]),
    ("frontend client", ["frontend/src/api/*"]),
    ("CSS", ["*.css"]),
    ("views/components", ["frontend/src/views/*", "frontend/src/components/*", "frontend/src/*.tsx"]),
    ("frontend other", ["frontend/*"]),
]

# Diff lines that show a layer whatever file they are in: git log -G takes
# these, with the paths each applies to.
CONTENT_RULES = [
    ("route registration", r"HandleFunc\(", ["internal/api"]),
    ("types", r"^[[:space:]]*(export[[:space:]]+)?(interface|type)[[:space:]]+[A-Za-z_]", ["frontend/src"]),
]

SOURCE_EXCLUDED = {"docs", "release notes"}


def classify_path(path):
    """Returns the layer a file path belongs to."""
    for layer, patterns in RULES:
        if any(fnmatch.fnmatchcase(path, p) for p in patterns):
            return layer
    return "other"


def git(args, cwd=None):
    return subprocess.run(["git"] + args, cwd=cwd, check=True, capture_output=True, text=True).stdout


def log_args(revs, max_count, first_parent):
    args = ["log", "--format=%x00%H%x1f%an%x1f%ad%x1f%s", "--date=short", "--name-only"]
    if first_parent:
        args += ["--first-parent", "--diff-merges=first-parent"]
    else:
        args.append("--no-merges")
    if max_count:
        args.append(f"--max-count={max_count}")
    return args + (revs or ["HEAD"])


def parse_log(text):
    """Parses `git log --format=%x00%H%x1f%an%x1f%ad%x1f%s --name-only` output."""
    commits = []
    for chunk in text.split("\x00")[1:]:
        lines = chunk.split("\n")
        sha, author, date, subject = (lines[0].split("\x1f") + ["", "", ""])[:4]
        files = sorted({l for l in lines[1:] if l.strip()})
        commits.append({"sha": sha, "author": author, "date": date, "subject": subject, "files": files})
    return commits


def content_hits(revs, max_count, first_parent, cwd=None):
    """Maps each content-rule layer to the set of commits whose diffs match."""
    hits = {}
    for layer, regex, paths in CONTENT_RULES:
        args = log_args(revs, max_count, first_parent)
        args[1] = "--format=%H"
        args.remove("--name-only")
        out = git(args[:1] + ["-G" + regex] + args[1:] + ["--"] + paths, cwd=cwd)
        hits[layer] = {l for l in out.split() if l}
    return hits


def classify(commits, hits=None):
    """Adds each commit's layers and hub files, in place, and returns it."""
    hits = hits or {}
    for c in commits:
        layers = {classify_path(f) for f in c["files"]}
        for layer, shas in hits.items():
            if c["sha"] in shas:
                layers.add(layer)
        c["layers"] = [l for l in LAYERS if l in layers]
        c["hubs"] = [h for h in HUBS if h in c["files"]]
        c["source"] = any(classify_path(f) not in SOURCE_EXCLUDED for f in c["files"])
    return commits


def summarize(commits):
    """The range-wide numbers: layer counts, files per commit, hub rates."""
    n = len(commits)
    source = [c for c in commits if c["source"]]
    sizes = [len(c["files"]) for c in commits] or [0]
    layer_counts = {l: sum(1 for c in commits if l in c["layers"]) for l in LAYERS}
    return {
        "commits": n,
        "source_commits": len(source),
        "files_per_commit": {
            "mean": round(statistics.mean(sizes), 1),
            "median": statistics.median(sizes),
            "max": max(sizes),
        },
        "layers_per_commit": round(statistics.mean([len(c["layers"]) for c in commits] or [0]), 1),
        "layers": {l: k for l, k in layer_counts.items() if k},
        "hubs": {h: sum(1 for c in commits if h in c["hubs"]) for h in HUBS},
        "any_hub": sum(1 for c in commits if c["hubs"]),
        "any_hub_source": sum(1 for c in source if c["hubs"]),
    }


def pct(k, n):
    return f"{100 * k / n:.0f}%" if n else "-"


def render(commits, summary, what, show_files=False):
    n = summary["commits"]
    s = summary["source_commits"]
    fpc = summary["files_per_commit"]
    out = [
        f"Commits: {n} ({what}); {s} change source (anything but docs and release notes)",
        f"Files per commit: mean {fpc['mean']}, median {fpc['median']}, max {fpc['max']}; "
        f"layers per commit: mean {summary['layers_per_commit']}",
        "",
        "Layers touched (commits):",
    ]
    for layer, k in summary["layers"].items():
        out.append(f"  {layer:<20} {k:>4}  {pct(k, n):>4}")
    out += ["", "Hub-touch rate (commits):"]
    for hub, k in summary["hubs"].items():
        out.append(f"  {hub:<45} {k:>4}  {pct(k, n):>4}")
    out.append(f"  {'any hub':<45} {summary['any_hub']:>4}  {pct(summary['any_hub'], n):>4}")
    out.append(f"  {'any hub, of commits that change source':<45} {summary['any_hub_source']:>4}  "
               f"{pct(summary['any_hub_source'], s):>4}")
    out += ["", "Per commit:"]
    for c in commits:
        hubs = f"  hubs: {', '.join(h.rsplit('/', 1)[-1] for h in c['hubs'])}" if c["hubs"] else ""
        out.append(f"  {c['sha'][:9]} {c['date']} {len(c['files']):>3} files  "
                   f"[{', '.join(c['layers'])}]{hubs}  {c['subject'][:72]}")
        if show_files:
            out += [f"      {classify_path(f):<18} {f}" for f in c["files"]]
    return "\n".join(out) + "\n"


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("revs", nargs="*", help="revisions for git log (default HEAD)")
    p.add_argument("-n", "--max-count", type=int, default=0, help="classify at most N commits")
    p.add_argument("--first-parent", action="store_true", help="treat each merge as one unit")
    p.add_argument("--json", action="store_true", help="print JSON instead of text")
    p.add_argument("--files", action="store_true", help="list each commit's files with their layer")
    args = p.parse_args(argv)
    try:
        commits = parse_log(git(log_args(args.revs, args.max_count, args.first_parent)))
        hits = content_hits(args.revs, args.max_count, args.first_parent)
    except subprocess.CalledProcessError as e:
        print(f"classify_commits: {e.stderr.strip()}", file=sys.stderr)
        return 2
    classify(commits, hits)
    summary = summarize(commits)
    if args.json:
        json.dump({"summary": summary, "commits": commits}, sys.stdout, indent=2)
        print()
        return 0
    what = " ".join(([f"-n {args.max_count}"] if args.max_count else []) + (args.revs or ["HEAD"]))
    what += ", first parent" if args.first_parent else ", merges skipped"
    sys.stdout.write(render(commits, summary, what, args.files))
    return 0


if __name__ == "__main__":
    sys.exit(main())
