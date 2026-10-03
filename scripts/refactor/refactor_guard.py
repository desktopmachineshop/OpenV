#!/usr/bin/env python3
"""Refactor guard: the checks of the refactor plan's S14b job, on every pull request.

docs/plans/codebase-refactor.md defines them (§6.4 S14b, rules R1-R3 in
§4.1, the verification classes in §4.2, the labels in §8.1);
CONTRIBUTING.md, "Refactor PRs", is the author's guide. The job
(.github/workflows/refactor-guard.yml) checks out GitHub's merge commit, so
HEAD^1 is the base the pull request was merged onto and HEAD^2 its head.
The pull request's change is HEAD^1..HEAD, and its commits are
HEAD^1..HEAD^2. The job runs the base's copy of this script, so a pull
request's edits to the lists and rules below take effect once it merges;
X2B_CALL_SHAPE_CHANGES alone is read from the pull request's tree.

Every pull request:
  (1) golden freeze: an M or D on the golden list (GOLDEN_LIST, 20 entries)
      fails unless the change adds a RELEASE_NOTES.md bullet under
      "## Unreleased" or carries the maintainer's behavior-change label;
      inline snapshots (toMatchInlineSnapshot,
      toThrowErrorMatchingInlineSnapshot) added to a test file fail, since
      they are goldens outside the list that `vitest -u` rewrites in place.

A pull request labelled `refactor` or `refactor:<anything>` also fails on:
  - a golden M or D in any case, and the behavior-change label;
  - (2) an M or D under FROZEN_DATA or PROTECTED_PATHS;
  - an M or D on GUARD_CODE except in a class C or T commit that modifies or
    deletes no golden (LINT_ALLOWLISTS and X2B_CALL_SHAPE_CHANGES are
    carved out of their files: see there);
  - an entry of internal/archtest/ratchets.json raised or added (in a
    commit of any class), apart from class D's new-package entries and a
    class T commit's new rule key;
  - an entry of an S12 lint allowlist added or raised;
  - the per-commit class checks of §4.2, read from each commit's
    Refactor-Class trailer (CLASS_TRAILER), with Refactor-Script (class R)
    and Refactor-Characterization (class E).
Something the pull request itself adds (a golden, a guard test, an
allowlist) is not frozen until it merges, so its later commits may refine
it; ratchets, allowlists and ceilings are judged against the base as well as
the commit's parent, so a pull request may restore what the base had. A file
of the base deleted in one commit and re-added in a later one is judged
against the base: the re-add counts as an edit unless it restores the base's
bytes. Growth is judged per entry, so an entry removed in one commit and
re-added higher in a later one counts as raised against the base.
(3) Stale ratchets is the job's next step, not this script: it runs
`UPDATE_RATCHETS=1 go test -count=1 -run '^TestArchitecture$' ./internal/archtest`
and diffs ratchets.json.

Usage:
  python3 scripts/refactor/refactor_guard.py [--labels TEXT] [--label NAME]...
                                             [--merge REV] [--base REV] [--head REV]
                                             [--summary FILE]

--labels takes newline-separated label names (what `gh api ... --jq
'.labels[].name'` prints); --label adds one. On a merge commit (CI, or a
local `git merge --no-ff` of the branch into its base) the defaults are
--merge HEAD, --base HEAD^1, --head HEAD^2. Anywhere else pass
--base <merge-base> --head HEAD; the change is then base..head. Exit
status 0 passes, 1 fails, 2 is a usage or git error. The summary goes to
standard output and, as Markdown, to --summary or $GITHUB_STEP_SUMMARY.

Class A runs `go run ./internal/tools/declhash` and, for TypeScript,
frontend/scripts/tsdeclhash.mjs and tsmovecheck.mjs (which need `npm ci` in
frontend/); class R re-runs the named script in a scratch worktree of the
commit's parent.

Standard library only. Tests:
  python3 -m unittest scripts/refactor/refactor_guard_test.py
"""

import argparse
import ast
import importlib.util
import json
import os
import re
import shlex
import shutil
import subprocess
import sys
import tempfile

# ---------------------------------------------------------------------------
# The data this guard enforces, in one place. Each entry names the plan step
# that owns it. A later step adds its entries here in a class T commit of its
# own pull request (CONTRIBUTING.md, "Refactor PRs").
# ---------------------------------------------------------------------------

REFACTOR_LABEL = "refactor"
REFACTOR_LABEL_PREFIX = "refactor:"
BEHAVIOR_CHANGE_LABEL = "behavior-change"
# §8.1: the label each class adds to `refactor`; B, D and E add none.
CLASS_LABELS = {"A": "refactor:move", "C": "refactor:test", "T": "refactor:tooling", "R": "refactor:script"}
CLASSES = ("A", "B", "C", "D", "E", "R", "T")

CLASS_TRAILER = "Refactor-Class"
# Class R: the committed script and its arguments, which the job re-runs on
# the commit's parent tree; the result must equal the commit byte for byte.
SCRIPT_TRAILER = "Refactor-Script"
# Class E: a characterization test file, one per trailer, merged in an
# earlier pull request and unchanged by this one.
CHARACTERIZATION_TRAILER = "Refactor-Characterization"

# (1) The golden list (§6.4 S14b): (owning step, what it pins, patterns).
# One entry may hold several patterns. `**` spans directories, `*` does not.
# An entry for a step not yet merged is the pattern the plan names; it
# matches nothing until that step lands its goldens. X4a/X5 add one entry
# in a class T commit (S17, which would have added another, is dropped).
GOLDEN_LIST = [
    ("I1, pre-S2", "HTTP route set", ["internal/api/testdata/routes.txt"]),
    ("S2", "route binding in registration order", ["internal/api/testdata/route_handlers.txt"]),
    ("S2", "same-method route overlaps", ["internal/api/testdata/route_overlaps.txt"]),
    ("S2", "guards per route", ["internal/api/testdata/route_guards.txt"]),
    ("S6", "domain-event payload Go types", ["internal/api/testdata/event_payload_types.txt"]),
    ("S3", "migration and every-boot freeze", ["internal/persistence/postgres/testdata/freeze/**"]),
    ("S3", "schema after Migrate and MigrateAndBackfill", ["internal/persistence/postgres/testdata/schema/**"]),
    ("S3", "purge catalogue", ["internal/persistence/postgres/testdata/purge/**"]),
    ("S4", "boot probes per env profile", ["cmd/server/testdata/boot/**"]),
    ("S4", "boot statement order", ["cmd/server/testdata/boot_steps.txt"]),
    ("S5", "API tour and authorization matrix", ["cmd/server/testdata/tour/**"]),
    ("S6, S13", "cross-language contracts", ["contracts/**"]),
    ("S7", "MCP tools and JSON-RPC", ["internal/mcp/testdata/**"]),
    ("S7", "worker wire", ["internal/runner/testdata/wire/**"]),
    ("S8", "env var inventory and parse table", ["internal/archtest/testdata/env_*.txt"]),
    ("S8", "CLI surfaces", ["cmd/*/testdata/cli/**"]),
    ("S9", "export, import and report formats",
     ["**/testdata/formats/**", "**/import_fields.txt", "**/testdata/proposal_payloads/**"]),
    ("S10", "notification content", ["internal/notify/testdata/notifications/**"]),
    ("S12, S12b, S16", "frontend file snapshots", ["frontend/src/**/__snapshots__/**"]),
    ("S12b", "bundle shape", ["frontend/scripts/testdata/bundle-shape.json"]),
]

# (2) Frozen data a refactor pull request may add to but never modify or
# delete, beyond the golden list: (owning step, pattern).
FROZEN_DATA = [
    ("Phase 0 guards", "**/testdata/**"),
    ("S12, S12b, S16", "**/__snapshots__/**"),
    ("X4a, X5", "frontend/src/generated/**"),
    ("S9", "docs/exports/*.json"),
]

# (2) Protected paths (§3 I25-I27): (invariant, pattern, condition). A
# condition is a function of the guard; the path is protected when it
# returns True. Dockerfile.api is protected once M1 has changed its build
# to a package path, which M1's own class T commit does. Beyond the plan's
# list: the frontend's package manifest and lockfile, which decide what its
# image installs and builds, and examples/**, which the API image copies and
# serves as project templates (both I26).
PROTECTED_PATHS = [
    ("I25", "RELEASE_NOTES.md", None),
    ("I25", "internal/domain/release/features.go", None),
    ("I26", "go.mod", None),
    ("I26", "go.sum", None),
    ("I26", "frontend/package.json", None),  # the npm counterpart of go.mod and go.sum
    ("I26", "frontend/package-lock.json", None),
    ("I26", "examples/**", None),  # copied by Dockerfile.api and served as project templates
    ("I26, after M1", "Dockerfile.api", lambda g: g.base_builds_by_package_path()),
    ("I26", "frontend/public/**", None),
    ("I26", "frontend/index.html", None),
    ("I26", "frontend/docker-entrypoint.d/**", None),
    ("I27", "frontend/nginx.conf", None),
    ("I27", "frontend/security-headers.conf", None),
    ("I27", "frontend/openv-nginx/**", None),
    ("I27", "frontend/Dockerfile.prod", None),
    ("I27", "frontend/railway.json", None),
    ("I27", "frontend/vite.config.ts", None),
]

# (2) Guard code (§6.4 S14b): M or D only in a class C or T commit that
# modifies or deletes no golden. (owning step, patterns). An empty list is a
# slot: the plan names no file for that step's guard code yet, and the step
# fills its slot in a class T commit when it lands. S14a, S14b, S14c, S14d,
# S14e and S14f are not in the plan's list: they are the tools that prove
# class A, this guard, M4's, M10's and M11a's class B and F1's move, so a
# commit of another class may not edit them either.
# Since the job runs the base's copy of this script, a pull request that also
# drops rows here is still judged by the rows it started from.
GUARD_CODE = [
    ("S1", ["internal/archtest/**"]),
    ("I1, S2", ["internal/api/route_inventory_test.go", "internal/api/route_binding_test.go",
                "internal/api/route_binding_guards_test.go"]),
    ("S3", ["internal/persistence/postgres/migration_freeze_test.go",
            "internal/persistence/postgres/migration_freeze_schema_test.go",
            "internal/persistence/postgres/migration_freeze_purge_test.go"]),
    ("S4a", ["cmd/server/harness_test.go", "cmd/server/harness_portlock_test.go",
             "cmd/server/harness_portlock_other_test.go", "cmd/server/boot_smoke_test.go",
             "cmd/server/boot_steps_test.go"]),
    ("S4b", ["cmd/server/boot_profiles_test.go", "cmd/server/boot_misconfigured_test.go"]),
    # The tour's framework, each file by name so that a rename shows (S5a:
    # tour_test.go, its normaliser, bodies and fixtures; S5b: the event-stream
    # reader; S5c: the accounts and settings, the mail catcher and the
    # stand-ins; S5d: the runner's requests; S5e: the authorization matrix,
    # its golden side, its shared columns, its no-database check and the
    # coverage union across slices), every area,
    # cmd/server/tour_<slice>_<key>_test.go, and the tests a slice pins below
    # the API because no request reaches the case (S5d: the stream's replay
    # draining every page of a run's log).
    ("S5a-S5e", ["cmd/server/tour_test.go", "cmd/server/tour_normalise_test.go", "cmd/server/tour_bodies_test.go",
                 "cmd/server/tour_fixtures_test.go", "cmd/server/tour_stream_test.go",
                 "cmd/server/tour_accounts_test.go", "cmd/server/tour_mail_test.go",
                 "cmd/server/tour_standin_test.go", "cmd/server/tour_worker_test.go",
                 "cmd/server/tour_matrix_test.go", "cmd/server/tour_matrix_golden_test.go",
                 "cmd/server/tour_matrix_cast_test.go", "cmd/server/tour_matrix_check_test.go",
                 "cmd/server/tour_coverage_union_test.go",
                 "cmd/server/tour_*_test.go", "internal/api/run_stream_replay_test.go"]),
    ("S6", ["internal/api/sse_contract_test.go", "internal/api/sse_scan_test.go",
            "internal/api/event_payload_types_test.go", "internal/api/event_payload_drives_test.go",
            "internal/domain/events/event_types_test.go", "frontend/src/components/agents/RunDetailPanel.test.tsx",
            "frontend/src/views/InterviewChat.stream.test.tsx"]),
    ("S7", ["internal/mcp/golden_test.go", "internal/mcp/tools_golden_test.go", "internal/mcp/jsonrpc_golden_test.go",
            "internal/runner/wire_golden_test.go", "internal/runner/wire_cases_test.go"]),
    # S8's inventory and its fixture are internal/archtest/env_*_test.go,
    # under S1's row. Outside it, by file name: each command's CLI snapshot
    # (its scenarios and the harness every command carries a copy of), and
    # each getter package's parse-table writer beside its copy of the shared
    # helpers. The patterns name no package, so a command or a package that
    # gains a getter (TestEnvInventory then asks for its TestEnvParse) is
    # covered with no edit here.
    ("S8", ["cmd/*/cli_test.go", "cmd/*/cli_harness_test.go",
            "cmd/*/env_parse_test.go", "cmd/*/env_parse_helpers_test.go",
            "internal/**/env_parse_test.go", "internal/**/env_parse_helpers_test.go"]),
    ("S12", ["frontend/src/arch/**", "frontend/eslint.config.js"]),
    ("S12b", ["frontend/src/**/cssOrder.test.ts", "frontend/scripts/bundle-check.mjs"]),
    ("S13", []),  # slot: the Go vocabulary writer and the vitest parity test
    ("S14a", ["internal/tools/declhash/**", "frontend/scripts/tsdeclhash.mjs", "frontend/scripts/tsmovecheck.mjs"]),
    # S14d's migration generator, its tests and their fixture and goldens.
    # M10 is what it writes; the S3 freeze and the generator's own self-check
    # (every lifted body its literal token for token, comments included,
    # everything else byte for byte) are M10's proof, so a refactor pull
    # request changes the generator only in a class C or T commit, never in
    # the commits it proves. M10's declmove spec is not under it.
    ("S14d", ["internal/tools/liftmigrations/**"]),
    ("S14b", [".github/workflows/refactor-guard.yml", "scripts/refactor/refactor_guard.py",
              "scripts/refactor/refactor_guard_test.py"]),
    # S14e's splittools generates M11a and, with -check, proves its class B
    # commit (every entry of Tools() unchanged and in order, and every other
    # declaration of the package unchanged); its tests prove the generator
    # on d11dee8's table and on the working tree. Its sources and tests
    # only: M11a may still edit its spec (specs/*.json) in any class, and
    # its testdata/ is frozen data like every other.
    ("S14e", ["internal/tools/splittools/*.go"]),
    # S14c's stageextract generates M4 (main() into wire_<stage>.go stages)
    # and checks its own result; movecheck's -flatten -base, with the
    # normalisation S14c gave it, is M4's proof (the split flattens to the
    # old main() once the stage rewrites are undone), and its map is M6-M9's.
    # Their sources and tests only: M4 may still edit its spec
    # (specs/*.json) in any class, and testdata/ is frozen data.
    ("S14c", ["internal/tools/stageextract/*.go", "internal/tools/movecheck/*.go"]),
    # S14f's tsdeclmove generates F1 (client.ts behind a barrel) and checks
    # what it writes as this guard's class A check will (tsdeclhash
    # --no-module and tsmovecheck over src/, and the barrel's surface); its
    # tests prove the committed spec on d11dee8's client.ts and on the
    # working tree through every frontend gate. The tool and its tests only:
    # F1 may still edit its spec (frontend/scripts/specs/*.json) in a class T
    # commit, and its testdata/ is frozen data like every other.
    ("S14f", ["frontend/scripts/tsdeclmove*.mjs"]),
]
# Under a guard-code pattern but governed by the ratchet rule instead.
GUARD_CODE_EXCEPT = ["internal/archtest/ratchets.json"]

# S12's lint allowlists (frontend/eslint.config.js). Removing or lowering an
# entry is allowed in a commit of any class and is not a guard-code edit
# (X4b, X15b-X15e); adding or raising one fails in any class. The kind says
# what an entry holds: "list" (file -> targets) or "count" (file -> number).
LINT_ALLOWLIST_FILE = "frontend/eslint.config.js"
LINT_ALLOWLISTS = {"COMPONENTS_IMPORTING_VIEWS": "list", "EVENT_SOURCE_SITES": "count"}

# The S1 ratchets. Numbers may only fall and lists and maps only shrink, in
# a commit of any class, except: a class D commit's import_edges and
# client_domain_deps entries for the package it creates (§4.2), and a new
# top-level key added by a class T commit that also changes the archtest
# rules (RATCHET_RULE_CODE), such as M5's K3 allowlist.
RATCHETS_FILE = "internal/archtest/ratchets.json"
RATCHET_RULE_CODE = "internal/archtest/*.go"

# X2b's named exception (§8.3): the route_guards.txt lines X2b converts, as
# ("line before", "line after") pairs, filled by X2b in its own class E
# commit (an edit to this list alone is not a guard-code edit in class E).
# A route_guards.txt change made of exactly these replacements, in class E
# commits of a refactor pull request, does not count as a golden change.
# Until X2b fills it, every route_guards.txt change is an authorization
# change. The guard reads the list from the pull request's copy of this file
# (GUARD_SCRIPT), since the job runs the base's copy and X2b fills the list
# in its own pull request.
GUARD_SCRIPT = "scripts/refactor/refactor_guard.py"
ROUTE_GUARDS_FILE = "internal/api/testdata/route_guards.txt"
X2B_CALL_SHAPE_CHANGES = [
]

# Shrink-only numbers inside guard code: S12's ceiling on inline error chains
# (quirk Q20). Lowering one is allowed in a commit of any class and is not a
# guard-code edit (X15b-X15e convert the chains in the files they touch and
# lower it, as the test asks); raising one fails in any class, like a
# ratchet. {file: [constant names]}, each a `const NAME = <number>;` line.
GUARD_CODE_CEILINGS = {"frontend/src/arch/errorChains.test.ts": ["CEILING"]}

# Parts of guard-code files that are data a commit of the given classes may
# edit without that counting as a guard-code edit: {file: {name: classes}},
# where "*" means any class and a name is a literal block or a numeric
# constant.
GUARD_CODE_CARVE_OUTS = {
    LINT_ALLOWLIST_FILE: {name: "*" for name in LINT_ALLOWLISTS},
    GUARD_SCRIPT: {"X2B_CALL_SHAPE_CHANGES": "E"},
    **{path: {name: "*" for name in names} for path, names in GUARD_CODE_CEILINGS.items()},
}

# Class C (§4.2): test files, S12's test-only helpers, and new files under a
# golden directory. frontend/src/test/** is F2's mock helper (class C in the
# plan); e2e/tests/** holds the Playwright specs and their helpers.
TEST_FILES = ["**/*_test.go", "**/*.test.ts", "**/*.test.tsx", "**/*.spec.ts", "**/*.spec.tsx", "**/*.test.mjs",
              "**/*_test.py", "e2e/tests/**"]
C_HELPERS = ["frontend/src/arch/**", "frontend/src/test/**"]
C_ADDED_ONLY = ["**/testdata/**", "**/__snapshots__/**", "contracts/**"]

# Class T (§4.2): CI, the Makefile, the Dockerfiles' build commands (M1),
# lint config, templates, docs and the tooling directories. TS the build
# erases (X5) also counts once S12b's base-vs-head build identity exists;
# until then any other frontend/src file fails a class T commit.
T_PATHS = [".github/workflows/**", ".github/pull_request_template.md", ".github/PULL_REQUEST_TEMPLATE/**",
           ".github/ISSUE_TEMPLATE/**", "Makefile", "Dockerfile*", "frontend/Dockerfile*", "frontend/eslint.config.js",
           "**/*.md", "docs/**", ".git-blame-ignore-revs", "internal/tools/**", "internal/archtest/**",
           "internal/contract/**", "scripts/**", "frontend/scripts/**", "frontend/src/generated/**"]

# Class A (§4.2): Go and frontend TypeScript sources, proved by declhash and
# tsdeclhash/tsmovecheck, plus a lowered ratchets.json and the declmove spec
# that generated the move (R4).
A_PATHS = ["**/*.go", "frontend/src/**/*.ts", "frontend/src/**/*.tsx", RATCHETS_FILE,
           "internal/tools/declmove/specs/*.json"]
A_TS = ["frontend/src/**/*.ts", "frontend/src/**/*.tsx"]

INLINE_SNAPSHOT = re.compile(r"\b(toMatchInlineSnapshot|toThrowErrorMatchingInlineSnapshot)\b")
INLINE_SNAPSHOT_FILES = ["**/*.test.ts", "**/*.test.tsx", "**/*.spec.ts", "**/*.spec.tsx", "**/*.test.js",
                         "**/*.test.jsx", "**/*.test.mjs", "e2e/**"]

# ---------------------------------------------------------------------------


def glob_regex(pattern):
    """A path glob as a regex: `**/` spans any directories (or none), a
    trailing `/**` everything below, `*` and `?` stay within one segment."""
    out, i = [], 0
    while i < len(pattern):
        if pattern.startswith("**/", i):
            out.append("(?:.*/)?")
            i += 3
        elif pattern.startswith("/**", i) and i + 3 == len(pattern):
            out.append("(?:/.*)?")
            i += 3
        elif pattern.startswith("**", i):
            out.append(".*")
            i += 2
        elif pattern[i] == "*":
            out.append("[^/]*")
            i += 1
        elif pattern[i] == "?":
            out.append("[^/]")
            i += 1
        else:
            out.append(re.escape(pattern[i]))
            i += 1
    return re.compile("^" + "".join(out) + "$")


_GLOBS = {}


def matches(path, patterns):
    for p in patterns:
        rx = _GLOBS.get(p)
        if rx is None:
            rx = _GLOBS[p] = glob_regex(p)
        if rx.match(path):
            return True
    return False


def golden_entry(path):
    for step, what, patterns in GOLDEN_LIST:
        if matches(path, patterns):
            return step, what
    return None


def guard_code_step(path):
    if matches(path, GUARD_CODE_EXCEPT):
        return None
    for step, patterns in GUARD_CODE:
        if matches(path, patterns):
            return step
    return None


def is_test_file(path):
    return matches(path, TEST_FILES)


def modifies(status):
    """M, D and T (type change) alter an existing file; A does not."""
    return status in ("M", "D", "T")


class GitError(Exception):
    pass


# git diff as the guard parses it, whatever the user's config says:
# color.ui=always would hide every added line behind an escape code, and
# an external diff driver would replace the patch.
PLAIN_DIFF = ("--no-renames", "--no-color", "--no-ext-diff")


class Git:
    """git in one work tree. Maintenance stays off: the guard never needs it,
    and a detached gc would race a test's temporary repository cleanup."""

    def __init__(self, root):
        self.root = root

    def run(self, *args, cwd=None, check=True, text=True, input=None):
        cmd = ["git", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-c", "core.quotePath=false"] + list(args)
        p = subprocess.run(cmd, cwd=cwd or self.root, capture_output=True, text=text, input=input)
        if check and p.returncode != 0:
            err = p.stderr if text else p.stderr.decode("utf-8", "replace")
            raise GitError(f"git {' '.join(args)}: {err.strip()}")
        return p

    def out(self, *args, **kw):
        return self.run(*args, **kw).stdout

    def rev(self, rev):
        """The commit rev names, or "" when it names none."""
        p = self.run("rev-parse", "--verify", "--quiet", rev + "^{commit}", check=False)
        return p.stdout.strip() if p.returncode == 0 else ""

    def parents(self, sha):
        return self.out("rev-list", "--parents", "-n", "1", sha).split()[1:]

    def commits(self, base, head):
        """Every commit in base..head, oldest first."""
        return self.out("rev-list", "--reverse", "--topo-order", f"{base}..{head}").split()

    def changes(self, a, b):
        """[(status, path)] between two commits; renames are a D and an A."""
        raw = self.out("diff", *PLAIN_DIFF, "--name-status", "-z", a, b)
        parts = raw.split("\0")
        return [(parts[i][0], parts[i + 1]) for i in range(0, len(parts) - 1, 2)]

    def is_ancestor(self, a, b):
        return self.run("merge-base", "--is-ancestor", a, b, check=False).returncode == 0

    def combined_changes(self, merge, base=None):
        """[(status, path)] of a merge commit's own change: the files where
        its tree differs from git's own merge of its two parents (git
        merge-tree, which leaves a conflict's markers in), apart from a file
        that takes the upstream parent's copy (the one parent on base), since
        that hides nothing from the pull request's net change. So a conflict
        settled with the branch's side wholesale (`git checkout --ours`,
        `git merge -s ours`), which drops what master changed there, is a
        change of its own. An octopus merge, or a git older than 2.38, falls
        back to the combined diff (git diff-tree --cc)."""
        parents = self.parents(merge)
        if len(parents) == 2:
            p = self.run("merge-tree", "--write-tree", "--no-messages", *parents, check=False)
            if p.returncode in (0, 1):  # 0 clean, 1 conflicted
                auto = p.stdout.split("\n", 1)[0].strip()
                upstream = [q for q in parents if base and self.is_ancestor(q, base)]
                take = upstream[0] if len(upstream) == 1 else None
                return [(s, path) for s, path in self.changes(auto, merge)
                        if take is None or self.show(take, path) != self.show(merge, path)]
        patch = self.out("diff-tree", "-r", "--cc", "--no-commit-id", "-p", merge)
        paths = []
        for line in patch.split("\n"):
            m = re.match(r"^diff --(?:cc|combined) (.+)$", line)
            if m:
                paths.append(m.group(1))
        if not paths:
            return []
        raw = self.out("diff-tree", "-r", "-c", "--no-commit-id", "--name-status", "-z", merge)
        parts = raw.split("\0")
        status = {parts[i + 1]: parts[i] for i in range(0, len(parts) - 1, 2)}
        out = []
        for p in paths:
            s = status.get(p, "M")
            out.append(("A" if set(s) == {"A"} else "D" if set(s) == {"D"} else "M", p))
        return out

    def show(self, rev, path):
        """The file's bytes at rev, or None when it is not a file there."""
        p = self.run("cat-file", "-t", f"{rev}:{path}", check=False)
        if p.returncode != 0 or p.stdout.strip() != "blob":
            return None
        return self.run("cat-file", "blob", f"{rev}:{path}", text=False).stdout

    def text(self, rev, path):
        b = self.show(rev, path)
        return None if b is None else b.decode("utf-8", "replace")

    def is_dir(self, rev, path):
        p = self.run("cat-file", "-t", f"{rev}:{path}", check=False)
        return p.returncode == 0 and p.stdout.strip() == "tree"

    def tree(self, rev):
        return self.out("rev-parse", "--verify", rev + "^{tree}").strip()

    def short(self, sha):
        return self.out("rev-parse", "--short", sha).strip()

    def subject(self, sha):
        return self.out("log", "-1", "--format=%s", sha).strip()

    def message(self, sha):
        return self.out("log", "-1", "--format=%B", sha)

    def trailers(self, sha):
        """[(key, value)] of the commit's trailer block, keys as written."""
        out = []
        for line in self.out("log", "-1", "--format=%(trailers:only,unfold)", sha).split("\n"):
            key, sep, value = line.partition(":")
            if sep and key.strip():
                out.append((key.strip(), value.strip()))
        return out

    def touching(self, base, head, path):
        """The commits in base..head that change path, oldest first."""
        return self.out("log", "--reverse", "--format=%H", "--full-history", f"{base}..{head}", "--", path).split()

    def added_lines(self, a, b, path):
        diff = self.out("diff", *PLAIN_DIFF, "-U0", a, b, "--", path)
        return [line[1:] for line in diff.split("\n") if line.startswith("+") and not line.startswith("+++")]


class ToolHashers:
    """The class A proofs S14a built, run from the repository root. go() and
    ts() return (ok, output); the tests inject a stub with the same shape.
    Each tool exits 1 for a difference and 2 when it could not compare."""

    def go(self, git, parent, commit, dirs):
        return self._run(git, ["go", "run", "./internal/tools/declhash", "-base", parent, "-head", commit] + dirs)

    def ts(self, git, parent, commit):
        ok1, out1 = self._run(git, ["node", "frontend/scripts/tsdeclhash.mjs", "--base", parent, "--head", commit,
                                    "--no-module"])
        ok2, out2 = self._run(git, ["node", "frontend/scripts/tsmovecheck.mjs", "--head", commit, parent])
        return ok1 and ok2, out1 + out2

    @staticmethod
    def _run(git, cmd):
        shown = "$ " + " ".join(cmd) + "\n"
        try:
            p = subprocess.run(cmd, cwd=git.root, capture_output=True, text=True, timeout=1800)
        except (OSError, subprocess.TimeoutExpired) as e:
            return False, f"{shown}{e}\n(the proof could not run)\n"
        out = shown + p.stdout + p.stderr
        if p.returncode not in (0, 1):
            out += (f"(exit {p.returncode}: the proof could not run; the TypeScript proofs need `npm ci` in "
                    "frontend/)\n")
        return p.returncode == 0, out


def load_release_notes():
    """scripts/release_notes.py, the parser the release-notes job uses."""
    path = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "release_notes.py")
    spec = importlib.util.spec_from_file_location("openv_release_notes", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


# ------------------------------------------------------------- JS/Py blocks

def find_block(text, name):
    """(start, end) line indexes of a top-level `NAME = [` or `const NAME = {`
    literal: its opening line, and the first later line that closes it at
    column 0 (`]`, `}`, `];` or `};`), or the opening line alone when it
    closes there too (`NAME = []`). None when there is no such literal."""
    lines = text.split("\n")
    opener = re.compile(r"^(?:(?:export\s+)?(?:const|let|var)\s+)?" + re.escape(name) + r"\s*=\s*[\[{]")
    for i, line in enumerate(lines):
        if not opener.match(line):
            continue
        code = line.split("//")[0].split("#")[0]
        if code.count("[") + code.count("{") <= code.count("]") + code.count("}"):
            return i, i
        for j in range(i + 1, len(lines)):
            if re.match(r"^[\]}];?\s*(?://.*|#.*)?$", lines[j]):
                return i, j
        return None
    return None


def find_constant(text, name):
    """(line index, value) of a top-level `const NAME = <integer>;`, or None."""
    rx = re.compile(r"^(?:export\s+)?(?:const|let|var)\s+" + re.escape(name) + r"\s*=\s*(\d+)\s*;?\s*(?://.*)?$")
    for i, line in enumerate(text.split("\n")):
        m = rx.match(line)
        if m:
            return i, int(m.group(1))
    return None


def blank_blocks(text, names):
    """text with each named literal or numeric constant replaced by a
    placeholder line, so two versions that differ only there compare
    equal."""
    lines = text.split("\n")
    spans = []
    for name in names:
        span = find_block(text, name)
        if not span:
            const = find_constant(text, name)
            span = (const[0], const[0]) if const else None
        if span:
            spans.append((span, name))
    for (start, end), name in sorted(spans, reverse=True):
        lines[start:end + 1] = [f"<{name}>"]
    return "\n".join(lines)


def block_text(text, name):
    span = find_block(text, name)
    if not span:
        return ""
    lines = text.split("\n")
    return "\n".join(lines[span[0]:span[1] + 1])


def parse_allowlist(text, name, kind):
    """The entries of an eslint.config.js allowlist: {file: [targets]} for a
    list, {file: count} for a count. An absent block is empty."""
    body = re.sub(r"//[^\n]*", "", block_text(text or "", name))
    body = body.split("=", 1)[1] if "=" in body else ""
    entries = {}
    for m in re.finditer(r"""(['"])([^'"]+)\1\s*:\s*(\[[^\]]*\]|\d+)""", body):
        key, value = m.group(2), m.group(3)
        if kind == "count":
            entries[key] = int(value) if value.isdigit() else 0
        else:
            entries[key] = [t[1] for t in re.findall(r"""(['"])([^'"]*)\1""", value)]
    return entries


def x2b_changes_in(text):
    """X2B_CALL_SHAPE_CHANGES as a copy of this script holds it, a list of
    (before, after) string pairs, or None when the literal is missing or is
    not such a list."""
    block = block_text(text or "", "X2B_CALL_SHAPE_CHANGES")
    if "=" not in block:
        return None
    try:
        value = ast.literal_eval(block.split("=", 1)[1])
    except (ValueError, SyntaxError):
        return None
    pairs = isinstance(value, list) and all(
        isinstance(p, tuple) and len(p) == 2 and all(isinstance(x, str) for x in p) for p in value)
    return value if pairs else None


def allowlist_growth(old_text, new_text):
    """[(allowlist, description)] for every entry added or raised."""
    return [(name, desc) for name, _, desc in allowlist_growth_keyed(old_text, new_text)]


def allowlist_growth_keyed(old_text, new_text):
    out = []
    for name, kind in LINT_ALLOWLISTS.items():
        old, new = parse_allowlist(old_text, name, kind), parse_allowlist(new_text, name, kind)
        for key, value in new.items():
            if key not in old:
                out.append((name, key, f"adds '{key}'"))
            elif kind == "count" and value > old[key]:
                out.append((name, key, f"raises '{key}' from {old[key]} to {value}"))
            elif kind == "list":
                for target in value:
                    if target not in old[key]:
                        out.append((name, key, f"adds '{target}' to '{key}'"))
    return out


# ----------------------------------------------------------------- ratchets

def is_number(x):
    return isinstance(x, (int, float)) and not isinstance(x, bool)


def ratchet_growth(old, new, prefix=()):
    """[(keypath, kind, detail)] for everything new holds that old does not
    allow: an added key or list item ("added"), a higher number ("raised"),
    or a value whose shape changed ("shape"). Strings (the "about" text) are
    free to change."""
    if isinstance(new, dict):
        if not isinstance(old, dict):
            return [(prefix, "shape", "is now an object")]
        out = []
        for key, value in new.items():
            if key not in old:
                out.append((prefix + (key,), "added", json.dumps(value)[:120]))
            else:
                out.extend(ratchet_growth(old[key], value, prefix + (key,)))
        return out
    if isinstance(new, list):
        if not isinstance(old, list):
            return [(prefix, "shape", "is now a list")]
        out, have = [], list(old)
        for item in new:
            if item in have:
                have.remove(item)
            else:
                out.append((prefix + (item if isinstance(item, str) else json.dumps(item),), "added", ""))
        return out
    if is_number(new):
        if not is_number(old):
            return [(prefix, "shape", f"changed from {json.dumps(old)} to {json.dumps(new)}")]
        return [(prefix, "raised", f"{old} -> {new}")] if new > old else []
    if isinstance(old, (dict, list)) or is_number(old):
        return [(prefix, "shape", f"changed from {json.dumps(old)[:60]} to {json.dumps(new)[:60]}")]
    return []


def keypath(kp):
    return ".".join(str(k) for k in kp) if kp else "(file)"


# -------------------------------------------------------------------- guard

class Failure:
    def __init__(self, rule, message, fix, commit=None, path=None):
        self.rule, self.message, self.fix, self.commit, self.path = rule, message, fix, commit, path

    def render(self):
        where = []
        if self.commit:
            where.append(f"commit {self.commit[0]} \"{self.commit[1]}\"")
        if self.path:
            where.append(self.path)
        head = f"[{self.rule}] " + (": ".join(where) + ": " if where else "")
        return f"{head}{self.message}\n    Fix: {self.fix}"


class Commit:
    def __init__(self, sha, short, subject, parents, changes, merge, clean_merge, trailers):
        self.sha, self.short, self.subject, self.parents = sha, short, subject, parents
        self.changes, self.merge, self.clean_merge, self.trailers = changes, merge, clean_merge, trailers
        self.klass = None
        self.notes = []
        self.failed = 0

    @property
    def ident(self):
        return (self.short, self.subject)

    def trailer_values(self, key):
        return [v for k, v in self.trailers if k.lower() == key.lower()]


class Guard:
    def __init__(self, git, base, head, merge, labels, hashers=None):
        self.git, self.base, self.head, self.merge = git, base, head, merge
        self.labels = sorted(set(l for l in labels if l))
        self.hashers = hashers or ToolHashers()
        self.failures, self.warnings, self.notes = [], [], []
        self.refactor = any(l == REFACTOR_LABEL or l.startswith(REFACTOR_LABEL_PREFIX) for l in self.labels)
        self.behavior_change = BEHAVIOR_CHANGE_LABEL in self.labels
        self.commits = []
        self.pr_changes = []
        self._on_base = {}

    # ---- helpers

    def fail(self, rule, message, fix, commit=None, path=None):
        f = Failure(rule, message, fix, commit.ident if isinstance(commit, Commit) else commit, path)
        self.failures.append(f)
        if isinstance(commit, Commit):
            commit.failed += 1

    def who_touched(self, path):
        """(short, subject) of the first commit of the pull request that
        touched path, for messages about the pull request's net change."""
        shas = self.git.touching(self.base, self.head, path)
        if not shas:
            return None
        return (self.git.short(shas[0]), self.git.subject(shas[0]))

    def on_base(self, path):
        """Whether path is a file on the base. A file the pull request adds
        is not frozen yet, so the pull request's own later commits may still
        refine it (a guard step adding a golden and its test)."""
        if path not in self._on_base:
            self._on_base[path] = self.git.show(self.base, path) is not None
        return self._on_base[path]

    def judged_against(self, c):
        """The trees a commit's ratchets, allowlists and ceilings are judged
        against: its parents, and the base. Something counts as raised or
        added only when it is more than in every one of them, so a merge
        keeps what either side allowed and a pull request may restore within
        itself what the base already had."""
        return self.parents_for_diff(c) + [self.base]

    def base_builds_by_package_path(self):
        text = self.git.text(self.base, "Dockerfile.api")
        return text is not None and "cmd/server/main.go" not in text

    def protected_entry(self, path):
        for inv, pattern, cond in PROTECTED_PATHS:
            if matches(path, [pattern]) and (cond is None or cond(self)):
                return inv
        return None

    # ---- the run

    def run(self):
        self.pr_changes = self.git.changes(self.base, self.merge)
        self.load_commits()
        self.check_goldens()
        self.check_inline_snapshots()
        if self.refactor:
            self.check_labels()
            self.check_frozen_paths()
            for c in self.commits:
                self.check_commit(c)
            self.check_class_mixing()
        return not self.failures

    def load_commits(self):
        for sha in self.git.commits(self.base, self.head):
            parents = self.git.parents(sha)
            merge = len(parents) > 1
            if merge:
                changes = self.git.combined_changes(sha, self.base)
            else:
                changes = self.git.changes(parents[0] if parents else self.empty_tree(), sha)
            self.commits.append(Commit(sha, self.git.short(sha), self.git.subject(sha), parents, changes, merge,
                                       merge and not changes, self.git.trailers(sha)))

    def empty_tree(self):
        return self.git.out("hash-object", "-t", "tree", "--stdin", input="").strip()

    # ---- (1) goldens, every pull request

    def x2b_exempt(self):
        """True when the route_guards.txt change is exactly X2b's listed
        call-shape replacements, made in class E commits of a refactor pull
        request (§8.3). The list is the pull request's copy (GUARD_SCRIPT in
        the merge commit), not this module's, which is the base's in CI."""
        if not self.refactor:
            return False
        listed = x2b_changes_in(self.git.text(self.merge, GUARD_SCRIPT))
        if listed is None:
            self.warnings.append(f"X2B_CALL_SHAPE_CHANGES in {GUARD_SCRIPT} is missing or not a list of "
                                 "(before, after) string pairs; counting no X2b exception")
            return False
        if not listed:
            return False
        old = (self.git.text(self.base, ROUTE_GUARDS_FILE) or "").split("\n")
        new = (self.git.text(self.merge, ROUTE_GUARDS_FILE) or "").split("\n")
        allowed = {before: after for before, after in listed}
        replaced = [allowed.get(line, line) for line in old]
        if replaced != new:
            return False
        for c in self.commits:
            if any(p == ROUTE_GUARDS_FILE for _, p in c.changes) and c.trailer_values(CLASS_TRAILER) != ["E"]:
                return False
        return True

    def release_note_added(self):
        try:
            rn = load_release_notes()
        except Exception as e:  # noqa: BLE001 - a missing parser is reported, not raised
            self.warnings.append(f"cannot load scripts/release_notes.py ({e}); counting no release note")
            return []
        head_text = self.git.text(self.merge, "RELEASE_NOTES.md") or ""
        base_text = self.git.text(self.base, "RELEASE_NOTES.md") or ""
        try:
            head, _, _ = rn.parse(head_text)
        except rn.NotesError as e:
            self.warnings.append(f"RELEASE_NOTES.md does not parse ({e}); counting no release note")
            return []
        base = []
        if base_text.strip():
            try:
                base, _, _ = rn.parse(base_text, strict=False)
            except rn.NotesError:
                base = []
        return [n for n in head if n not in base]

    def check_goldens(self):
        changed = [(s, p, golden_entry(p)) for s, p in self.pr_changes if modifies(s) and golden_entry(p)]
        if any(p == ROUTE_GUARDS_FILE for _, p, _ in changed) and self.x2b_exempt():
            changed = [x for x in changed if x[1] != ROUTE_GUARDS_FILE]
            self.notes.append(f"{ROUTE_GUARDS_FILE}: only X2b's listed call-shape lines changed (§8.3)")
        if self.refactor and self.behavior_change:
            self.fail("(1) golden freeze", f"the pull request carries both a refactor label and "
                      f"'{BEHAVIOR_CHANGE_LABEL}'", "a refactor changes no behavior: remove the refactor labels "
                      f"if this is a behavior change, or '{BEHAVIOR_CHANGE_LABEL}' if it is not (§8.1)")
        if not changed:
            return
        verb = {"M": "modified", "D": "deleted", "T": "changed type"}
        if self.refactor:
            for s, p, (step, what) in changed:
                self.fail("(1) golden freeze", f"{verb[s]} a golden ({what}, {step}) in a refactor pull request",
                          "a refactor leaves every golden byte-identical. Fix the code so the golden stays as it "
                          "is; if the change is deliberate it is not a refactor: drop the refactor labels and add a "
                          "RELEASE_NOTES.md bullet, or ask the maintainer for 'behavior-change' (R3)",
                          commit=self.who_touched(p), path=p)
            return
        if self.behavior_change:
            self.notes.append(f"{len(changed)} golden(s) changed under the maintainer's '{BEHAVIOR_CHANGE_LABEL}' "
                              "label")
            return
        added = self.release_note_added()
        if added:
            bullet = added[0][1] if len(added[0][1]) <= 100 else added[0][1][:97] + "..."
            self.notes.append(f"{len(changed)} golden(s) changed with a release note ({added[0][0]}: {bullet})")
            return
        for s, p, (step, what) in changed:
            self.fail("(1) golden freeze", f"{verb[s]} a golden ({what}, {step}) without a release note",
                      "a golden pins behavior people or programs see. Add a bullet under '## Unreleased' in "
                      "RELEASE_NOTES.md saying what changed, or, for a change nobody sees (a test-only pull request "
                      f"extending a golden), ask the maintainer for the '{BEHAVIOR_CHANGE_LABEL}' label",
                      commit=self.who_touched(p), path=p)

    def check_inline_snapshots(self):
        for s, p in self.pr_changes:
            if s not in ("A", "M") or not matches(p, INLINE_SNAPSHOT_FILES):
                continue
            for line in self.git.added_lines(self.base, self.merge, p):
                m = INLINE_SNAPSHOT.search(line)
                if m:
                    self.fail("K16 inline snapshot", f"adds {m.group(1)}: {line.strip()[:100]}",
                              "vitest goldens are file snapshots under __snapshots__/ (toMatchFileSnapshot), never "
                              "inline: `vitest -u` would rewrite an inline one inside the test, outside the golden "
                              "list", commit=self.who_touched(p), path=p)
                    break

    # ---- (2) refactor pull requests

    def check_labels(self):
        declared = {c.trailer_values(CLASS_TRAILER)[0] for c in self.commits
                    if len(c.trailer_values(CLASS_TRAILER)) == 1 and not c.clean_merge}
        for klass, label in sorted(CLASS_LABELS.items()):
            if klass in declared and label not in self.labels:
                self.warnings.append(f"a commit declares class {klass} but the pull request lacks the '{label}' "
                                     "label (§8.1)")
            if label in self.labels and klass not in declared:
                self.warnings.append(f"the pull request carries '{label}' but no commit declares class {klass}")

    def check_frozen_paths(self):
        for s, p in self.pr_changes:
            if not modifies(s) or golden_entry(p):
                continue  # goldens are rule (1)'s
            inv = self.protected_entry(p)
            if inv:
                self.fail("(2) protected path", f"{'deletes' if s == 'D' else 'modifies'} a protected path ({inv})",
                          "a refactor never changes release notes, feature gating, modules or what the images "
                          "build and serve (§3 I25-I27); make this change in its own, non-refactor pull request",
                          commit=self.who_touched(p), path=p)
                continue
            for step, pattern in FROZEN_DATA:
                if matches(p, [pattern]):
                    self.fail("(2) frozen data", f"{'deletes' if s == 'D' else 'modifies'} frozen data under "
                              f"{pattern} ({step})", "a refactor may add files there, never modify or delete one; "
                              "if the data must change, the change is not a refactor (R3)",
                              commit=self.who_touched(p), path=p)
                    break

    def check_class_mixing(self):
        classes = {c.klass for c in self.commits if c.klass}
        if "E" in classes and len(classes) > 1:
            others = ", ".join(sorted(classes - {"E"}))
            for c in self.commits:
                if c.klass == "E":
                    self.fail("R2 class E alone", f"class E is mixed with class {others} in this pull request",
                              "a class E semantic extraction is its own pull request: move the other commits to "
                              "another pull request (R2)", commit=c)
                    break

    def check_commit(self, c):
        if c.clean_merge:
            c.notes.append("clean merge")
            return
        values = c.trailer_values(CLASS_TRAILER)
        if not values:
            own = ""
            if c.merge:
                paths = [p for _, p in c.changes]
                own = (" (a merge that differs from git's own merge of its parents, other than by taking master's "
                       "copy of a file, is a change of its own: " + ", ".join(paths[:5]) +
                       (f" and {len(paths) - 5} more" if len(paths) > 5 else "") + ")")
            if re.search(rf"(?im)^\s*{CLASS_TRAILER}\s*:", self.git.message(c.sha)):
                self.fail("R2 class trailer", f"has a {CLASS_TRAILER} line outside its trailer block: git reads "
                          "trailers only from the message's last paragraph, and only while every line there is a "
                          "trailer", f"move the {CLASS_TRAILER} line into the last paragraph with Signed-off-by, "
                          "with no blank line or other text among the trailers (git commit --amend, or git rebase "
                          "with 'reword')", commit=c)
                return
            self.fail("R2 class trailer", f"has no {CLASS_TRAILER} trailer" + own,
                      f"end the message with '{CLASS_TRAILER}: <{'|'.join(CLASSES)}>' before Signed-off-by "
                      "(git commit --amend, or git rebase with 'reword'); a commit with two classes is split into "
                      "one commit per class", commit=c)
            return
        if len(values) > 1:
            self.fail("R2 class trailer", f"declares {len(values)} classes ({', '.join(values)})",
                      "split the commit, one class per commit (R2)", commit=c)
            return
        klass = values[0]
        if klass not in CLASSES:
            self.fail("R2 class trailer", f"declares an unknown class '{klass}'",
                      f"use one of {', '.join(CLASSES)} (§4.2)", commit=c)
            return
        c.klass = klass
        getattr(self, "check_class_" + klass)(c)
        self.check_guard_code(c)
        self.check_ratchets(c)
        self.check_lint_allowlists(c)
        self.check_guard_ceilings(c)

    def parents_for_diff(self, c):
        return c.parents if c.parents else [self.empty_tree()]

    # ---- per-class checks (§4.2, §8.1)

    def check_class_A(self, c):
        if c.merge:
            self.fail("class A", "a merge commit carries a class A change",
                      "regenerate the move on the latest master instead of resolving it in a merge (R4)", commit=c)
            return
        others = [p for _, p in c.changes if not matches(p, A_PATHS)]
        for p in others:
            self.fail("class A", "a pure move changes only Go and frontend/src TypeScript sources (plus a lowered "
                      "ratchets.json and its declmove spec)", "split the commit: put this file in a commit of its "
                      "own class", commit=c, path=p)
        parent = self.parents_for_diff(c)[0]
        go_dirs = sorted({os.path.dirname(p) or "." for _, p in c.changes if p.endswith(".go")})
        if go_dirs:
            ok, out = self.hashers.go(self.git, parent, c.sha, go_dirs)
            c.notes.append(last_line(out))
            if not ok:
                self.fail("class A", "declhash differs between the parent and this commit:\n" + indent(out),
                          "a pure move keeps every declaration byte-identical (go run ./internal/tools/declhash "
                          f"-base {c.short}^ -head {c.short} <pkg dirs>); regenerate it with declmove, or declare "
                          "the class it is (B for an extraction in place)", commit=c)
        if any(matches(p, A_TS) for _, p in c.changes):
            ok, out = self.hashers.ts(self.git, parent, c.sha)
            c.notes.append(last_line(out))
            if not ok:
                self.fail("class A", "tsdeclhash or tsmovecheck finds more than a move:\n" + indent(out),
                          "a pure TypeScript move changes only imports and keeps evaluation order (node "
                          f"frontend/scripts/tsmovecheck.mjs --head {c.short} {c.short}^); split it or declare its "
                          "class", commit=c)

    def check_class_B(self, c):
        self.check_no_goldens(c, "B")

    def check_class_D(self, c):
        self.check_no_goldens(c, "D")

    def check_no_goldens(self, c, klass):
        for s, p in c.changes:
            entry = golden_entry(p)
            if entry:
                self.fail(f"class {klass}", f"changes a golden ({entry[1]}, {entry[0]})",
                          "an extraction in place or a package move leaves the ordered goldens unchanged (§4.2); "
                          "the golden change is behavior, not a refactor", commit=c, path=p)

    def check_class_C(self, c):
        for s, p in c.changes:
            if is_test_file(p) or matches(p, C_HELPERS):
                continue
            if matches(p, C_ADDED_ONLY):
                if s == "A" or not self.on_base(p):
                    continue  # new, or new in this pull request
                self.fail("class C", "a test-only commit may add files under testdata/, __snapshots__/ or contracts/"
                          f", not {'delete' if s == 'D' else 'modify'} them", "leave the golden as it is; a golden "
                          "that must change is a behavior change", commit=c, path=p)
                continue
            self.fail("class C", "a test-only commit changes only test files, frontend/src/arch/** and new golden "
                      "files", "split the commit: put this file in a commit of its own class (T for tooling and "
                      "docs)", commit=c, path=p)

    def check_class_T(self, c):
        for s, p in c.changes:
            if matches(p, T_PATHS):
                continue
            if p.startswith("frontend/src/"):
                self.fail("class T", "a tooling commit may change TS under frontend/src only once S12b's "
                          "base-vs-head build proves the build output identical, and S12b has not landed",
                          "declare the class this change is (A, B, C or E), or wait for S12b", commit=c, path=p)
                continue
            self.fail("class T", "a tooling commit changes only CI, the Makefile, Dockerfile build commands, lint "
                      "config, templates, docs and the tooling directories (§4.2)", "split the commit: put this "
                      "file in a commit of its own class", commit=c, path=p)

    def check_class_R(self, c):
        if c.merge:
            self.fail("class R", "a merge commit carries a class R change",
                      "re-run the script on the latest master instead of resolving it in a merge (R4)", commit=c)
            return
        values = c.trailer_values(SCRIPT_TRAILER)
        if len(values) != 1:
            self.fail("class R", f"needs exactly one {SCRIPT_TRAILER} trailer, found {len(values)}",
                      f"add '{SCRIPT_TRAILER}: <script path> [args...]' naming the committed script that produced "
                      "this commit", commit=c)
            return
        try:
            argv = shlex.split(values[0])
        except ValueError as e:
            self.fail("class R", f"{SCRIPT_TRAILER} does not parse: {e}", "quote it as a shell command line",
                      commit=c)
            return
        script = argv[0] if argv else ""
        parent = self.parents_for_diff(c)[0]
        if not script or script.startswith(("/", "../")) or "/../" in script:
            self.fail("class R", f"{SCRIPT_TRAILER} names '{script}', not a path in the repository",
                      "name the script by its path from the repository root", commit=c)
            return
        if self.git.show(parent, script) is None:
            self.fail("class R", f"the script '{script}' is not in the commit's parent",
                      "commit the script (and any tool it drives) in an earlier class T commit, so this commit holds "
                      "only what the script writes", commit=c)
            return
        ok, out = self.rerun_script(parent, c.sha, argv)
        c.notes.append(f"re-ran {script}")
        if not ok:
            self.fail("class R", out, "the commit must be exactly what the script writes on its parent: re-run it "
                      "on the latest master and commit the result unedited (R4)", commit=c)

    def rerun_script(self, parent, commit, argv):
        tmp = tempfile.mkdtemp(prefix="refactor-guard-")
        tree_dir = os.path.join(tmp, "tree")
        try:
            try:
                self.git.run("worktree", "add", "--detach", "--quiet", tree_dir, parent)
            except GitError as e:
                return False, f"cannot check out the parent to re-run the script: {e}"
            script = argv[0]
            if script.endswith(".py"):
                cmd = [sys.executable, script] + argv[1:]
            elif script.endswith(".sh"):
                cmd = ["bash", script] + argv[1:]
            else:
                cmd = [os.path.join(tree_dir, script)] + argv[1:]
            try:
                p = subprocess.run(cmd, cwd=tree_dir, capture_output=True, text=True, timeout=1800)
            except (OSError, subprocess.TimeoutExpired) as e:
                return False, f"running {' '.join(argv)} on the parent failed: {e}"
            if p.returncode != 0:
                return False, (f"{' '.join(argv)} exited {p.returncode} on the parent:\n" +
                               indent((p.stdout + p.stderr)[-2000:]))
            try:
                self.git.run("add", "-A", cwd=tree_dir)
                got = self.git.out("write-tree", cwd=tree_dir).strip()
            except GitError as e:
                return False, f"cannot record what the script wrote: {e}"
            want = self.git.tree(commit)
            if got != want:
                stat = self.git.out("diff", *PLAIN_DIFF, "--stat", got, want)
                return False, ("re-running the script on the parent does not reproduce the commit (script output "
                               "-> commit):\n" + indent(stat))
            return True, ""
        finally:
            self.git.run("worktree", "remove", "--force", tree_dir, check=False)
            shutil.rmtree(tmp, ignore_errors=True)
            self.git.run("worktree", "prune", check=False)

    def check_class_E(self, c):
        values = [v for raw in c.trailer_values(CHARACTERIZATION_TRAILER) for v in re.split(r"[\s,]+", raw) if v]
        if not values:
            self.fail("class E", f"names no {CHARACTERIZATION_TRAILER} trailer",
                      f"add '{CHARACTERIZATION_TRAILER}: <test file>' for each characterization test, merged in an "
                      "earlier pull request, that pins the logic this commit moves (R1)", commit=c)
            return
        changed = {p for _, p in self.pr_changes}
        for path in values:
            if not is_test_file(path):
                self.fail("class E", f"{CHARACTERIZATION_TRAILER} '{path}' is not a test file",
                          "name the characterization test file itself", commit=c, path=path)
            elif self.git.show(self.base, path) is None:
                self.fail("class E", f"the characterization '{path}' is not on the base (HEAD^1)",
                          "merge the characterization in its own, earlier pull request (R1)", commit=c, path=path)
            elif path in changed:
                self.fail("class E", f"the characterization '{path}' is changed by this pull request",
                          "a class E change is proved by characterization tests that stay as they are", commit=c,
                          path=path)
        c.notes.append("characterized by " + ", ".join(values))

    # ---- guard code, ratchets and allowlists, every class

    def carved_out_only(self, c, path):
        """True when a modified guard-code file differs from its parent only
        inside blocks this commit's class may edit."""
        blocks = GUARD_CODE_CARVE_OUTS.get(path)
        if not blocks:
            return False
        names = [n for n, classes in blocks.items() if classes == "*" or c.klass in classes]
        if not names:
            return False
        new = self.git.text(c.sha, path)
        if new is None:
            return False
        for parent in self.parents_for_diff(c):
            old = self.git.text(parent, path)
            if old is not None and blank_blocks(old, names) == blank_blocks(new, names):
                return True
        return False

    def check_guard_code(self, c):
        golden_modified = any(modifies(s) and golden_entry(p) and self.on_base(p) for s, p in c.changes)
        for s, p in c.changes:
            step = guard_code_step(p)
            if not step or not self.on_base(p):
                continue  # not guard code, or added by this pull request
            if not modifies(s) and self.git.show(self.base, p) == self.git.show(c.sha, p):
                continue  # deleted earlier in this pull request and re-added as the base has it
            if s == "M" and self.carved_out_only(c, p):
                continue
            if c.klass in ("C", "T") and not golden_modified:
                continue
            why = (f"a class {c.klass} commit" if c.klass not in ("C", "T")
                   else f"a class {c.klass} commit that also modifies or deletes a golden")
            verb = {"D": "deletes", "A": "re-adds, changed from the base,"}.get(s, "modifies")
            self.fail("(2) guard code", f"{verb} guard code ({step}) in {why}",
                      "change guard code only in its own class C or T commit that changes no golden, green against "
                      "the production code of its parent (R3)", commit=c, path=p)

    def check_ratchets(self, c):
        if not any(p == RATCHETS_FILE for _, p in c.changes) or not self.on_base(RATCHETS_FILE):
            return  # untouched, or created by this pull request (S1)
        new_text = self.git.text(c.sha, RATCHETS_FILE)
        if new_text is None:
            return  # deleted: nothing is raised; archtest itself fails without the file
        try:
            new = json.loads(new_text)
        except ValueError as e:
            self.fail("(2) ratchets", f"{RATCHETS_FILE} is not valid JSON: {e}",
                      "restore it and regenerate with UPDATE_RATCHETS=1 go test ./internal/archtest", commit=c,
                      path=RATCHETS_FILE)
            return
        per_parent = []
        for parent in self.judged_against(c):
            old_text = self.git.text(parent, RATCHETS_FILE)
            if old_text is None:
                continue  # deleted earlier in this pull request: the base still judges it
            try:
                old = json.loads(old_text) if old_text else {}
            except ValueError:
                old = {}
            growth = [g for g in ratchet_growth(old, new) if not self.ratchet_exception(c, parent, old, new, g)]
            per_parent.append({g[0]: g for g in growth})
        # Raised or added only when more than in every tree judged against.
        keys = set.intersection(*(set(d) for d in per_parent)) if per_parent else set()
        for key in sorted(keys, key=keypath):
            kp, kind, detail = per_parent[-1][key]
            what = {"added": "adds", "raised": "raises", "shape": "changes the shape of"}[kind]
            self.fail("(2) ratchets", f"{what} ratchets.json entry {keypath(kp)}" + (f" ({detail})" if detail else ""),
                      "a refactor only lowers or removes ratchet entries (R6): change the code so the entry is not "
                      "needed, or regenerate with UPDATE_RATCHETS=1 go test ./internal/archtest, which only tightens",
                      commit=c, path=RATCHETS_FILE)

    def ratchet_exception(self, c, parent, old, new, growth):
        kp, kind, _ = growth
        if kind == "added" and len(kp) == 1 and c.klass == "T" and any(
                matches(p, [RATCHET_RULE_CODE]) for _, p in c.changes):
            return True  # a new rule's key, added with the rule (M5)
        if c.klass != "D" or kind != "added" or not kp:
            return False
        top = kp[0]
        is_new = lambda pkg: (pkg != "(root)" and self.git.is_dir(c.sha, pkg)  # noqa: E731
                              and not self.git.is_dir(parent, pkg))
        new_edges = new.get("import_edges", {}) if isinstance(new, dict) else {}
        old_edges = old.get("import_edges", {}) if isinstance(old, dict) else {}

        def importers(pkg):
            return [k for k, v in new_edges.items() if isinstance(v, list) and pkg in v and k != pkg]

        def old_imports_of(pkgs):
            out = set()
            for k in pkgs:
                out.update(old_edges.get(k, []) if isinstance(old_edges.get(k), list) else [])
            return out

        if top == "import_edges":
            if len(kp) == 2:  # a new importer key: the package the commit creates, with its edges
                pkg = kp[1]
                if not is_new(pkg):
                    return False
                allowed = old_imports_of(importers(pkg))
                return all(is_new(v) or v in allowed for v in new_edges.get(pkg, []))
            if len(kp) == 3:  # a new edge: into the new package, or out of it to what its old home imports
                importer, target = kp[1], kp[2]
                if is_new(target):
                    return True
                return is_new(importer) and target in old_imports_of(importers(importer))
        if top == "client_domain_deps" and len(kp) == 3:
            binary, pkg = kp[1], kp[2]
            old_list = (old.get("client_domain_deps", {}) or {}).get(binary, [])
            return is_new(pkg) and any(pkg in (new_edges.get(k) or []) for k in old_list)
        return False

    def check_lint_allowlists(self, c):
        if not any(p == LINT_ALLOWLIST_FILE for _, p in c.changes):
            return
        base_text = self.git.text(self.base, LINT_ALLOWLIST_FILE) or ""
        frozen = {name for name in LINT_ALLOWLISTS if find_block(base_text, name)}  # S12 created them
        if not frozen:
            return
        new = self.git.text(c.sha, LINT_ALLOWLIST_FILE) or ""
        # A tree without the file (deleted earlier in this pull request) judges
        # nothing; the base, which has it, still does.
        olds = [self.git.text(tree, LINT_ALLOWLIST_FILE) for tree in self.judged_against(c)]
        per_parent = [{(n, k): d for n, k, d in allowlist_growth_keyed(old, new)} for old in olds if old is not None]
        for name, key in sorted(k for k in set.intersection(*(set(d) for d in per_parent)) if k[0] in frozen):
            desc = per_parent[-1][(name, key)]
            self.fail("(2) lint allowlist", f"{desc} in {name}", "S12's allowlists only shrink: fix the import or "
                      "open the stream through the shared hook instead (§6.4 S12)", commit=c,
                      path=LINT_ALLOWLIST_FILE)

    def check_guard_ceilings(self, c):
        for path, names in GUARD_CODE_CEILINGS.items():
            if not any(p == path and s in ("M", "A") for s, p in c.changes):
                continue  # an A is a re-add when the base has the constant
            new = self.git.text(c.sha, path) or ""
            for name in names:
                now = find_constant(new, name)
                if not now:
                    continue  # renamed or gone: the guard-code rule judges the edit
                if not find_constant(self.git.text(self.base, path) or "", name):
                    continue  # created by this pull request
                olds = [self.git.text(tree, path) for tree in self.judged_against(c)]
                before = [find_constant(old, name) for old in olds if old is not None]
                if all(b is None or now[1] > b[1] for b in before):
                    self.fail("(2) guard ceiling", f"raises {name} from {max(b[1] for b in before if b)} to {now[1]}",
                              "this ceiling only falls: fix the code that pushed the count up instead", commit=c,
                              path=path)

    # ---- output

    def row(self, c):
        """(class, result) of a commit for the summary table."""
        klass = "-" if c.clean_merge else (c.klass or (c.trailer_values(CLASS_TRAILER) or ["-"])[0])
        result = "merge" if c.clean_merge else ("FAIL" if c.failed else ("ok" if self.refactor else "-"))
        return klass, result

    def summary(self):
        kind = "refactor" if self.refactor else "not a refactor"
        lines = [f"Refactor guard: {self.git.short(self.base)}..{self.git.short(self.head)} "
                 f"({len(self.commits)} commits; {kind}; labels: {', '.join(self.labels) or 'none'})", ""]
        rows = [("commit", "class", "result", "subject", "notes")]
        for c in self.commits:
            klass, result = self.row(c)
            rows.append((c.short, klass, result, c.subject[:60], "; ".join(n for n in c.notes if n)[:80]))
        widths = [max(len(r[i]) for r in rows) for i in range(4)]
        for r in rows:
            lines.append("  ".join(r[i].ljust(widths[i]) for i in range(4)) + ("  " + r[4] if r[4] else ""))
        lines.append("")
        for n in self.notes:
            lines.append(f"note: {n}")
        for w in self.warnings:
            lines.append(f"warning: {w}")
        if self.failures:
            lines.append(f"{len(self.failures)} failure(s):")
            for f in self.failures:
                lines.append(f.render())
        else:
            lines.append("Refactor guard passed" + ("" if self.refactor else
                                                    " (golden freeze and inline snapshots; class checks run only "
                                                    "on refactor pull requests)"))
        return "\n".join(lines)

    def markdown(self):
        md = [f"## Refactor guard: {'failed' if self.failures else 'passed'}", "",
              f"{'Refactor' if self.refactor else 'Not a refactor'} pull request; labels: "
              f"{', '.join('`' + l + '`' for l in self.labels) or 'none'}.", "",
              "| Commit | Class | Result | Subject |", "|---|---|---|---|"]
        for c in self.commits:
            klass, result = self.row(c)
            md.append(f"| `{c.short}` | {klass} | {result} | {c.subject.replace('|', '/')} |")
        if self.warnings or self.notes:
            md.append("")
            md.extend(f"- {n}" for n in self.notes + self.warnings)
        if self.failures:
            md += ["", "### Failures", ""]
            for f in self.failures:
                md.append("```\n" + f.render() + "\n```")
        return "\n".join(md) + "\n"


def indent(text):
    return "\n".join("      " + line for line in text.rstrip("\n").split("\n"))


def last_line(text):
    """The tool's closing verdict, for the summary table."""
    lines = [line for line in text.strip().split("\n") if line.strip() and not line.startswith("exit status ")]
    return re.sub(r"\b([0-9a-f]{12})[0-9a-f]{28}\b", r"\1", lines[-1]) if lines else ""


def annotate(guard):
    """GitHub Actions annotations, one per failure and warning."""
    def esc(s, prop=False):
        s = s.replace("%", "%25").replace("\r", "%0D").replace("\n", "%0A")
        return s.replace(":", "%3A").replace(",", "%2C") if prop else s

    for f in guard.failures:
        where = f",file={esc(f.path, True)}" if f.path else ""
        print(f"::error title={esc('Refactor guard ' + f.rule, True)}{where}::{esc(f.render())}")
    for w in guard.warnings:
        print(f"::warning title=Refactor guard::{esc(w)}")


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--labels", default="", help="label names, one per line")
    p.add_argument("--label", action="append", default=[], help="a label name (repeatable)")
    p.add_argument("--merge", default="", help="the merge commit (default HEAD, or --head when given)")
    p.add_argument("--base", default="", help="the base (default <merge>^1)")
    p.add_argument("--head", default="", help="the pull request's head (default <merge>^2)")
    p.add_argument("--summary", default=os.environ.get("GITHUB_STEP_SUMMARY", ""),
                   help="append a Markdown summary here (default $GITHUB_STEP_SUMMARY)")
    args = p.parse_args(argv)
    labels = [l.strip() for l in args.labels.split("\n")] + [l.strip() for l in args.label]
    try:
        root = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True,
                              check=True).stdout.strip()
        git = Git(root)
        if args.base or args.head:
            if not (args.base and args.head):
                raise GitError("pass --base and --head together")
            base, head = git.rev(args.base), git.rev(args.head)
            merge = git.rev(args.merge) if args.merge else head
            if not (base and head and merge):
                raise GitError("--base, --head or --merge is not a commit")
        else:
            merge = git.rev(args.merge or "HEAD")
            parents = git.parents(merge) if merge else []
            if len(parents) < 2:
                raise GitError(f"{args.merge or 'HEAD'} is not a merge commit: CI checks out GitHub's merge commit; "
                               "locally, merge the branch into its base with --no-ff, or pass --base <merge-base> "
                               "--head <branch>")
            base, head = parents[0], parents[1]
        guard = Guard(git, base, head, merge, labels)
        ok = guard.run()
    except (GitError, subprocess.CalledProcessError) as e:
        print(f"refactor guard: {e}", file=sys.stderr)
        return 2
    print(guard.summary())
    if os.environ.get("GITHUB_ACTIONS") == "true":
        annotate(guard)
    if args.summary:
        with open(args.summary, "a", encoding="utf-8") as f:
            f.write(guard.markdown())
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
