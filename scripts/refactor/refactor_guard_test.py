"""Tests for refactor_guard.py.

Run: python3 -m unittest scripts/refactor/refactor_guard_test.py

Each test builds a throwaway repository shaped like a pull request: a base
branch, a pull request branch, and the merge commit GitHub checks out
(HEAD^1 the base, HEAD^2 the branch). The class A hashers and S12b's frontend
build are stubbed, so no Go or Node toolchain runs here (BuilderTest drives
the real builder with a stand-in npm); the job itself runs the real ones.
"""

import base64
import hashlib
import io
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stdout

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import refactor_guard as rg  # noqa: E402

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

NOTES = "# Notes\n\n## Unreleased\n\n## 0.1.0 - 2026-01-01\n\n### Bug fixes\n\n- Old fix.\n"
NOTES_WITH_BULLET = NOTES.replace("## Unreleased\n", "## Unreleased\n\n### Bug fixes\n\n- Routes answer faster.\n")

RATCHETS = {
    "about": "ratchets",
    "import_edges": {
        "internal/domain/exports": ["internal/domain/artifacts", "internal/domain/links"],
        "internal/domain/baselines": ["internal/domain/exports"],
        "internal/api": ["internal/domain/exports"],
    },
    "client_domain_deps": {"cmd/agentd": ["internal/domain/exports"]},
    "file_lines": {"internal/api/handlers.go": 3519},
    "counts": {"raw_json_encodes": 249},
    "side_effect_vars": ["internal/domain/quality:placeholderRe"],
}

ESLINT = """// boundaries
const COMPONENTS_IMPORTING_VIEWS = {
  'src/components/ChatterPanel.tsx': ['src/views/TodoList'],
  'src/components/ProjectLayout.tsx': ['src/views/TodoList'],
};

const EVENT_SOURCE_HOOK = 'src/hooks/useEventStream.ts';
const EVENT_SOURCE_SITES = {
  'src/components/NotificationBell.tsx': 1,
  'src/views/InterviewChat.tsx': 1,
};

export default [];
"""

ERROR_CHAINS = """// ratchet
const CEILING = 53;

it(`stays at or below ${CEILING}`, () => {});
"""

SIZE_BUDGET = """// ratchet
const FILE_BUDGET = 600;
const OVER_1000 = 5;

const FILE_CEILINGS = {
  'api/client.ts': 3036,
  'views/Login.tsx': 778,
};

const COMPONENT_CEILINGS = {
  'Login': 733,
};

it(`files stay within ${FILE_BUDGET}`, () => {});
"""

GUARD_PY = """X2B_CALL_SHAPE_CHANGES = [
]

OTHER = 1
"""


def trailers(klass=None, **extra):
    lines = []
    if klass:
        lines.append(f"Refactor-Class: {klass}")
    for key, values in extra.items():
        for v in values if isinstance(values, list) else [values]:
            lines.append(f"{key.replace('_', '-')}: {v}")
    lines.append("Signed-off-by: T <t@example.com>")
    return "\n".join(lines)


class StubHashers:
    """Stands in for declhash and tsdeclhash/tsmovecheck."""

    def __init__(self, go_ok=True, ts_ok=True):
        self.go_ok, self.ts_ok, self.calls = go_ok, ts_ok, []

    def go(self, git, parent, commit, dirs):
        self.calls.append(("go", dirs))
        if self.go_ok:
            return True, "declhash: 3 declarations identical\n"
        return False, "changed\tp\tF\ndeclhash: 1 of 3 declarations differ\n"

    def ts(self, git, parent, commit):
        self.calls.append(("ts",))
        return self.ts_ok, "tsmovecheck: a pure move\n" if self.ts_ok else "tsmovecheck: not a pure move\n"


def content_hash(data):
    """Eight characters of the alphabet Vite's chunk hashes use."""
    return base64.urlsafe_b64encode(hashlib.sha256(data).digest()).decode()[:8]


TYPE_ONLY_LINE = re.compile(r"^(export (type|interface) |import type )")


class StubBuilder:
    """Stands in for S12b's frontend build: "builds" a revision from its
    frontend/src tree, as Vite would in miniature. Stylesheets under
    frontend/src/views/lazy/ make a lazy chunk, the rest the entry's; each
    chunk's CSS is its stylesheets in path order (an import's order, here),
    and its JS the TypeScript with the lines TypeScript erases left out; the
    lazy chunk's JS imports the entry's by its hashed name. Files are named
    <chunk>-<content hash>.<ext>, with a .map beside the JS that embeds the
    sources, as Vite's do. A frontend/postcss.config.js is its PostCSS: each
    `remove <selector>` line drops the stylesheet lines that start with it.
    rename={label: {chunk: name}} names a chunk otherwise in that build, as
    the bundler names a shared chunk after one of the modules in it."""

    def __init__(self, fail=None, empty=None, rename=None):
        self.fail, self.empty, self.calls, self.cleaned = fail, empty, [], 0
        self.rename = rename or {}
        self.tmp = []

    def build(self, git, rev, label):
        self.calls.append(label)
        if self.fail == label:
            return None, "npm ERR! the build failed\n"
        d = tempfile.mkdtemp(prefix="stub-build-")
        self.tmp.append(d)
        assets = os.path.join(d, "build", "assets")
        os.makedirs(assets)
        if self.empty == label:
            return assets, "built nothing\n"
        files = git.out("ls-tree", "-r", "--name-only", rev, "--", "frontend/src").split("\n")
        postcss = (git.show(rev, "frontend/postcss.config.js") or b"").decode()
        removed = tuple(m.encode() for m in re.findall(r"^remove (\S+)$", postcss, re.M))
        chunks = {}
        for f in sorted(x for x in files if x):
            chunk = "Lazy" if f.startswith("frontend/src/views/lazy/") else "index"
            data = git.show(rev, f)
            if f.endswith(".css"):
                if removed:
                    data = b"".join(l for l in data.splitlines(True) if not l.startswith(removed))
                chunks.setdefault((chunk, ".css"), []).append(data)
            elif re.search(r"\.tsx?$", f) and not re.search(r"\.test\.tsx?$", f) and "/arch/" not in f:
                kept = [l for l in data.decode().split("\n") if not TYPE_ONLY_LINE.match(l)]
                chunks.setdefault((chunk, ".js"), []).append("\n".join(kept).encode())
                chunks.setdefault((chunk, ".js.map"), []).append(data)  # embeds the sources
        entry_js = "index-" + content_hash(b"".join(chunks.get(("index", ".js"), []))) + ".js"
        if ("Lazy", ".js") in chunks:
            chunks[("Lazy", ".js")].insert(0, f'import"./{entry_js}";'.encode())
        for (chunk, ext), parts in chunks.items():
            data = b"".join(parts)
            stem = (self.rename.get(label, {}).get(chunk, chunk) + "-" +
                    content_hash(b"".join(chunks.get((chunk, ".js"), [])) if ext == ".js.map" else data))
            with open(os.path.join(assets, stem + ext), "wb") as f:
                f.write(data)
        return assets, f"built {label}\n"

    def cleanup(self):
        self.cleaned += 1
        for d in self.tmp:
            shutil.rmtree(d, ignore_errors=True)


@unittest.skipUnless(shutil.which("git"), "git is not installed")
class RepoTest(unittest.TestCase):
    """A throwaway repository with a base branch `main` and a pull request
    branch `pr`."""

    def setUp(self):
        self.dir = tempfile.mkdtemp(prefix="refactor-guard-test-")
        self.addCleanup(shutil.rmtree, self.dir, True)
        self.git("init", "-q", "-b", "main")
        self.write({
            "RELEASE_NOTES.md": NOTES,
            "internal/api/testdata/routes.txt": "GET /a\n",
            "internal/api/testdata/route_guards.txt": "GET /a: session\nGET /b: -\n",
            "internal/api/handlers.go": "package api\n\nfunc A() {}\n",
            "internal/api/handlers_test.go": "package api\n",
            "internal/api/route_binding_test.go": "package api\n\n// guard\n",
            "internal/archtest/ratchets.json": json.dumps(RATCHETS, indent=2) + "\n",
            "internal/archtest/archtest_test.go": "package archtest\n",
            "internal/domain/exports/export.go": "package exports\n",
            "internal/tools/declhash/main.go": "package main\n",
            "frontend/eslint.config.js": ESLINT,
            "frontend/src/views/App.tsx": "export const App = 1;\n",
            "frontend/src/index.css": ".button { color: red; }\n",
            "frontend/src/views/ProjectList.css": ".button { color: blue; }\n",
            "frontend/src/views/lazy/Graph.css": ".graph { color: green; }\n",
            "frontend/src/views/lazy/Graph.tsx": "export const Graph = 2;\n",
            "frontend/src/arch/routeTree.test.ts": "test('x', () => {});\n",
            "frontend/src/arch/errorChains.test.ts": ERROR_CHAINS,
            "frontend/src/arch/sizeBudget.test.ts": SIZE_BUDGET,
            "frontend/src/arch/testdata/fixture.json": "{}\n",
            "internal/domain/exports/testdata/in.json": "{}\n",
            "scripts/refactor/refactor_guard.py": GUARD_PY,
            "go.mod": "module example.com/x\n",
            "frontend/package.json": "{\"name\": \"x\"}\n",
            "frontend/package-lock.json": "{\"lockfileVersion\": 3}\n",
            "examples/x/project.json": "{}\n",
            "Dockerfile.api": "RUN go build -o server cmd/server/main.go\n",
            "docs/guide.md": "# Guide\n",
        })
        self.commit("base")
        self.git("checkout", "-q", "-b", "pr")

    # ---- repository helpers

    def git(self, *args, may_fail=False):
        p = subprocess.run(["git", "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false",
                            "-c", "core.hooksPath=/dev/null", "-c", "gc.auto=0", "-c", "maintenance.auto=false"]
                           + list(args), cwd=self.dir, capture_output=True, text=True)
        if p.returncode != 0 and not may_fail:
            raise AssertionError(f"git {' '.join(args)}: {p.stderr}")
        return p.stdout.strip()

    def write(self, files):
        for path, text in files.items():
            full = os.path.join(self.dir, path)
            if text is None:
                os.remove(full)
                continue
            os.makedirs(os.path.dirname(full), exist_ok=True)
            with open(full, "w") as f:
                f.write(text)

    def commit(self, subject, files=None, body=""):
        if files:
            self.write(files)
        self.git("add", "-A")
        self.git("commit", "-q", "--allow-empty", "-m", subject + ("\n\n" + body if body else ""))
        return self.git("rev-parse", "HEAD")

    def merged(self):
        """GitHub's merge commit of pr into main; returns (base, head, merge)."""
        head = self.git("rev-parse", "pr")
        self.git("checkout", "-q", "main")
        self.git("checkout", "-q", "-b", "merge")
        self.git("merge", "-q", "--no-ff", "--no-edit", "pr")
        return self.git("rev-parse", "HEAD^1"), head, self.git("rev-parse", "HEAD")

    def guard(self, *labels, hashers=None, builder=None):
        base, head, merge = self.merged()
        self.builder = builder or StubBuilder()
        g = rg.Guard(rg.Git(self.dir), base, head, merge, list(labels), hashers or StubHashers(), self.builder)
        g.run()
        return g

    def rules(self, g):
        return [f.rule for f in g.failures]

    def assertPasses(self, g):
        self.assertEqual(g.failures, [], "\n".join(f.render() for f in g.failures))

    def assertFailsWith(self, g, rule, text=""):
        hits = [f for f in g.failures if f.rule == rule and text in f.render()]
        self.assertTrue(hits, f"no {rule!r} failure mentioning {text!r}; got:\n" +
                        "\n".join(f.render() for f in g.failures))
        return hits[0]


REFACTOR = ("refactor", "no-release-notes")


class DataTest(unittest.TestCase):
    def test_the_golden_list_has_21_entries(self):
        # The plan's 20, plus S15a's run failure goldens.
        self.assertEqual(len(rg.GOLDEN_LIST), 21)

    def test_globs(self):
        cases = [
            ("internal/archtest/**", "internal/archtest/ratchets.json", True),
            ("internal/archtest/**", "internal/archtestx/a.go", False),
            ("**/testdata/**", "internal/api/testdata/routes.txt", True),
            ("**/testdata/**", "testdata/x", True),
            ("**/testdata/**", "internal/api/testdatax/y", False),
            ("frontend/src/**/__snapshots__/**", "frontend/src/arch/__snapshots__/routeTree.txt", True),
            ("frontend/src/**/__snapshots__/**", "frontend/src/__snapshots__/a.txt", True),
            ("cmd/*/testdata/cli/**", "cmd/agentd/testdata/cli/help.txt", True),
            ("cmd/*/testdata/cli/**", "cmd/a/b/testdata/cli/help.txt", False),
            ("**/*.md", "README.md", True),
            ("**/*.md", "internal/archtest/README.md", True),
            ("Dockerfile*", "Dockerfile.api", True),
            ("Dockerfile*", "frontend/Dockerfile.prod", False),
            ("internal/archtest/*.go", "internal/archtest/x/y.go", False),
            ("**/*_test.go", "a_test.go", True),
        ]
        for pattern, path, want in cases:
            with self.subTest(pattern=pattern, path=path):
                self.assertEqual(rg.matches(path, [pattern]), want)

    def test_every_merged_golden_is_on_master(self):
        # Each entry of a merged guard step still matches a tracked golden, so
        # renaming a golden or its directory cannot drop it from the list
        # unnoticed. A new fixture beside a golden is not one, so this does
        # not list every file under those directories.
        merged = {"I1, pre-S2", "S2", "S3", "S4", "S5", "S6", "S6, S13", "S7", "S8", "S9", "S10", "S12, S12b, S16",
                  "S12b", "S15a"}
        files = subprocess.run(["git", "ls-files"], cwd=REPO, capture_output=True, text=True,
                               check=True).stdout.split("\n")
        files = [f for f in files if f and not f.endswith(".gitattributes")]
        entries = [(step, what, patterns) for step, what, patterns in rg.GOLDEN_LIST if step in merged]
        self.assertEqual(len(entries), 21)
        for step, what, patterns in entries:
            with self.subTest(step=step, golden=what):
                self.assertTrue(any(rg.matches(f, patterns) for f in files),
                                f"no tracked file matches {patterns}: rename the entry with its golden")
                if step == "S10":
                    # S10's entry names two goldens; each must still exist.
                    for p in patterns:
                        self.assertTrue(any(rg.matches(f, [p]) for f in files), f"no tracked file matches {p}")

    def test_merged_guard_code_exists(self):
        # A literal guard-code path of a merged step that no longer exists
        # would protect nothing; rename it here in the same commit.
        merged = {"S1", "I1, S2", "S3", "S4a", "S4b", "S5a-S5e", "S6", "S7", "S8", "S9", "S10", "S11", "S12", "S12b", "S13",
                  "S14a", "S14b", "S14c", "S14d", "S14e", "S14f", "S15a", "S15b"}
        for step, patterns in rg.GUARD_CODE:
            for p in patterns:
                if step in merged and "*" not in p:
                    with self.subTest(step=step, path=p):
                        self.assertTrue(os.path.isfile(os.path.join(REPO, p)), p)

    def test_merged_tour_slices_are_guarded(self):
        # Each merged slice of the API tour (plan S5a-S5e) keeps its area
        # goldens and its coverage.txt under the S5 golden entry, and each
        # golden has its area, cmd/server/tour_<slice>_<key>_test.go, which
        # is guard code of the S5a-S5e row like the framework: a refactor may
        # add an area, but edits one only in a class C or T commit. A slice
        # joins this set in the class T commit of the step that merges it,
        # and the framework files it adds join the list below, as do the
        # tests it pins below the API (S5d: the stream's replay draining
        # every page of a run's log, which no request reaches). The union of
        # the slices' coverage (S5e), which holds the plan's 90% floor, is a
        # golden of the S5 entry too.
        merged = {"S5a", "S5b", "S5c", "S5d", "S5e"}
        files = set(subprocess.run(["git", "ls-files"], cwd=REPO, capture_output=True, text=True,
                                   check=True).stdout.split("\n"))
        for f in ["cmd/server/tour_test.go", "cmd/server/tour_normalise_test.go", "cmd/server/tour_bodies_test.go",
                  "cmd/server/tour_fixtures_test.go", "cmd/server/tour_stream_test.go",
                  "cmd/server/tour_accounts_test.go", "cmd/server/tour_mail_test.go",
                  "cmd/server/tour_standin_test.go", "cmd/server/tour_worker_test.go",
                  "cmd/server/tour_matrix_test.go", "cmd/server/tour_matrix_golden_test.go",
                  "cmd/server/tour_matrix_cast_test.go", "cmd/server/tour_matrix_check_test.go",
                  "cmd/server/tour_coverage_union_test.go",
                  "internal/api/run_stream_replay_test.go"]:
            with self.subTest(framework=f):
                self.assertIn(f, files)
                self.assertEqual(rg.guard_code_step(f), "S5a-S5e")
        union = "cmd/server/testdata/tour/coverage.txt"
        with self.subTest(union=union):
            self.assertIn(union, files)
            self.assertEqual((rg.golden_entry(union) or ("none",))[0], "S5")
        for step in sorted(merged):
            tour_slice = step.lower()
            prefix = "cmd/server/testdata/tour/%s/" % tour_slice
            goldens = sorted(f for f in files if f.startswith(prefix) and f.endswith(".json"))
            with self.subTest(slice=tour_slice):
                self.assertTrue(goldens, "no golden under " + prefix)
                self.assertIn(prefix + "coverage.txt", files)
                for g in goldens + [prefix + "coverage.txt"]:
                    self.assertEqual((rg.golden_entry(g) or ("none",))[0], "S5", g)
                areas = sorted(f for f in files
                               if re.fullmatch(r"cmd/server/tour_%s_[a-z0-9_]+_test\.go" % tour_slice, f))
                want = ["cmd/server/tour_%s_%s_test.go" % (tour_slice, os.path.basename(g)[:-len(".json")])
                        for g in goldens]
                self.assertEqual(areas, want, "each area golden needs its area file, and each area file its golden")
                for a in areas:
                    self.assertEqual(rg.guard_code_step(a), "S5a-S5e", a)

    def test_merged_s8_writers_are_guarded(self):
        # S8's writers outside internal/archtest are guard code of its row by
        # file name, wherever they sit under cmd/ and internal/: each
        # command's CLI snapshot and the harness beside it, and each getter
        # package's parse-table writer and its copy of the shared helpers.
        # A command or getter package that gains one is covered with no edit
        # here, so nothing counts them; instead the four commands of I14 keep
        # their snapshot by path, every writer keeps its helpers beside it,
        # and each pattern of the row still matches a file.
        files = subprocess.run(["git", "ls-files"], cwd=REPO, capture_output=True, text=True,
                               check=True).stdout.split("\n")
        names = ("cli_test.go", "cli_harness_test.go", "env_parse_test.go", "env_parse_helpers_test.go")
        writers = [f for f in files if os.path.basename(f) in names and f.startswith(("cmd/", "internal/"))]
        for f in writers:
            with self.subTest(path=f):
                self.assertEqual(rg.guard_code_step(f), "S8")
        for command in ("agentd", "openv-connector", "openv-mcp", "openv-vapid"):
            for name in ("cli_test.go", "cli_harness_test.go"):
                self.assertIn(f"cmd/{command}/{name}", writers)
        parse = {os.path.dirname(f) for f in writers if os.path.basename(f) == "env_parse_test.go"}
        helpers = {os.path.dirname(f) for f in writers if os.path.basename(f) == "env_parse_helpers_test.go"}
        self.assertTrue(parse)
        self.assertEqual(parse, helpers)
        row = dict(rg.GUARD_CODE)["S8"]
        for p in row:
            with self.subTest(pattern=p):
                self.assertTrue(any(rg.matches(f, [p]) for f in files), f"{p} matches no tracked file")

    def test_s8_row_covers_a_new_getter_package(self):
        # A feature pull request that adds a getter in a package with none
        # adds its parse writer there, and a command its CLI snapshot: both
        # are S8 guard code with no edit to the row.
        for path in ("internal/newpkg/env_parse_test.go", "internal/domain/newpkg/env_parse_helpers_test.go",
                     "cmd/newcmd/cli_test.go", "cmd/newcmd/env_parse_test.go"):
            with self.subTest(path=path):
                self.assertEqual(rg.guard_code_step(path), "S8")
        for path in ("cmd/newcmd/sub/cli_test.go", "frontend/env_parse_test.go", "internal/archtest/env_parse_test.go"):
            with self.subTest(path=path):
                self.assertNotEqual(rg.guard_code_step(path), "S8")

    def test_s10_notification_content_goldens(self):
        # Every notification type's four goldens are on the list under S10's
        # entry, as is the bell's deep-link table, which S10's entry claims
        # ahead of S12's frontend snapshots (S12's own stay S12's), so a pull
        # request that changes what a notification delivers needs a release
        # note and a refactor (X6 above all) may not change it. The tests
        # that write and read them are S10's guard code; the notifier's other
        # tests and its production files are not.
        files = subprocess.run(["git", "ls-files", "internal/notify/testdata/notifications"], cwd=REPO,
                               capture_output=True, text=True, check=True).stdout.split()
        goldens = [f for f in files if not f.endswith(".gitattributes")]
        types = sorted({f.split("/")[4] for f in goldens})
        self.assertGreaterEqual(len(types), 12, types)
        for t in types:
            with self.subTest(type=t):
                self.assertEqual(sorted(os.path.basename(f) for f in goldens if f.split("/")[4] == t),
                                 ["email.txt", "push.json", "row.json", "sse.txt"])
        for path in goldens + ["frontend/src/components/__snapshots__/NotificationBell.paths.txt"]:
            with self.subTest(path=path):
                self.assertTrue(os.path.isfile(os.path.join(REPO, path)), path)
                self.assertEqual(rg.golden_entry(path), ("S10", "notification content"))
        self.assertEqual(rg.golden_entry("frontend/src/arch/__snapshots__/deepLinks.txt"),
                         ("S12, S12b, S16", "frontend file snapshots"))
        for path in ("internal/notify/notification_content_test.go",
                     "internal/notify/notification_content_harness_test.go",
                     "internal/domain/notifications/content_golden_test.go",
                     "frontend/src/components/NotificationBell.paths.test.tsx"):
            with self.subTest(path=path):
                self.assertTrue(os.path.isfile(os.path.join(REPO, path)), path)
                self.assertEqual(rg.guard_code_step(path), "S10")
        for path in ("internal/notify/notifier_test.go", "internal/notify/email.go", "internal/notify/stable.go",
                     "internal/domain/notifications/notifications.go", "frontend/src/components/NotificationBell.tsx",
                     "frontend/src/components/NotificationBell.test.tsx"):
            with self.subTest(path=path):
                self.assertIsNone(rg.guard_code_step(path))

    def test_s11_automation_launch_characterization(self):
        # S11 pins the scheduler, the trigger matcher and run-now's copy with
        # no golden: its six test files are its guard code, so a refactor
        # (M4's and M7's moves among them) may not relax them outside a
        # class C or T commit. The matcher's earlier regression test, the
        # other tests beside them and the production files are not, and S11
        # adds no golden entry: the golden list keeps 21.
        for path in ("internal/scheduler/scheduler_test.go", "internal/scheduler/scheduler_harness_test.go",
                     "internal/automation/matcher_test.go", "internal/automation/matcher_harness_test.go",
                     "internal/api/automation_run_now_test.go",
                     "internal/persistence/postgres/scheduler_claim_test.go"):
            with self.subTest(path=path):
                self.assertTrue(os.path.isfile(os.path.join(REPO, path)), path)
                self.assertEqual(rg.guard_code_step(path), "S11")
        for path in ("internal/automation/triggers_test.go", "internal/automation/triggers.go",
                     "internal/scheduler/scheduler.go", "internal/api/agent_handlers.go",
                     "internal/api/launch_run_token_test.go",
                     "internal/persistence/postgres/automation_repository_test.go",
                     "internal/persistence/postgres/automation_repository.go"):
            with self.subTest(path=path):
                self.assertIsNone(rg.guard_code_step(path))
        self.assertNotIn("S11", {step for step, _, _ in rg.GOLDEN_LIST})
        self.assertEqual(len(rg.GOLDEN_LIST), 21)

    def test_s15a_run_failure_goldens(self):
        # Both of S15a's goldens are on the list under their own entry, not
        # S7's worker wire beside them, so a pull request that changes what
        # the runner reports for a failed run needs a release note. The
        # tests that write them, the other S15a tests and their shared
        # stand-ins are guard code of S15a's row, so M15a's class B commit
        # cannot relax what proves it (M15b, class E, names them as its
        # characterization); the rest of the runner's tests are not, and S7's
        # wire test stays S7's.
        files = subprocess.run(["git", "ls-files", "internal/runner/testdata/run_failures"], cwd=REPO,
                               capture_output=True, text=True, check=True).stdout.split()
        goldens = [f for f in files if not f.endswith(".gitattributes")]
        self.assertEqual(sorted(os.path.basename(f) for f in goldens), ["classes.txt", "outcomes.txt"])
        for path in goldens:
            with self.subTest(path=path):
                self.assertEqual(rg.golden_entry(path), ("S15a", "run failure outcomes and classes"))
        self.assertEqual(rg.golden_entry("internal/runner/testdata/wire/finish.json"), ("S7", "worker wire"))
        for path in ("internal/runner/run_failures_test.go", "internal/runner/run_slots_test.go",
                     "internal/runner/signin_claim_test.go", "internal/runner/pool_lease_test.go",
                     "internal/runner/fakeapi_test.go"):
            with self.subTest(path=path):
                self.assertTrue(os.path.isfile(os.path.join(REPO, path)), path)
                self.assertEqual(rg.guard_code_step(path), "S15a")
        for path in ("internal/runner/worker_test.go", "internal/runner/worker.go", "internal/runner/pool.go"):
            with self.subTest(path=path):
                self.assertIsNone(rg.guard_code_step(path))
        self.assertEqual(rg.guard_code_step("internal/runner/wire_golden_test.go"), "S7")

    def test_s15b_repository_round_trips(self):
        # S15b pins the team, work item, project, agent and member
        # repositories with no golden: its five round-trip files and the
        # helpers they share are its guard code, so M12's class A and B
        # commits and X13's class E ones may not relax them. The package's
        # earlier tests beside them (the board order, the malformed-id and
        # time-zone tests, the test database), the repositories themselves
        # and the other steps' Postgres tests are not S15b's, and S15b adds
        # no golden entry: the golden list keeps 21.
        mine = ["internal/persistence/postgres/repository_roundtrip_helpers_test.go"] + [
            f"internal/persistence/postgres/{r}_repository_roundtrip_test.go"
            for r in ("team", "workitem", "project", "agent", "member")]
        files = subprocess.run(["git", "ls-files", "internal/persistence/postgres"], cwd=REPO,
                               capture_output=True, text=True, check=True).stdout.split()
        self.assertEqual(sorted(f for f in files if rg.guard_code_step(f) == "S15b"), sorted(mine))
        for path in mine:
            with self.subTest(path=path):
                self.assertTrue(os.path.isfile(os.path.join(REPO, path)), path)
                self.assertEqual(rg.guard_code_step(path), "S15b")
        for path in ("internal/persistence/postgres/workitem_repository_test.go",
                     "internal/persistence/postgres/malformed_id_test.go",
                     "internal/persistence/postgres/timestamptz_test.go",
                     "internal/persistence/postgres/testdb_test.go",
                     "internal/persistence/postgres/team_repository.go",
                     "internal/persistence/postgres/workitem_repository.go",
                     "internal/persistence/postgres/project_repository.go",
                     "internal/persistence/postgres/project_info_repository.go",
                     "internal/persistence/postgres/agent_repository.go",
                     "internal/persistence/postgres/member_repository.go"):
            with self.subTest(path=path):
                self.assertIsNone(rg.guard_code_step(path))
        self.assertEqual(rg.guard_code_step("internal/persistence/postgres/export_roundtrip_test.go"), "S9")
        self.assertEqual(rg.guard_code_step("internal/persistence/postgres/scheduler_claim_test.go"), "S11")
        self.assertNotIn("S15b", {step for step, _, _ in rg.GOLDEN_LIST})
        self.assertEqual(len(rg.GOLDEN_LIST), 21)

    def test_s9_formats_payloads_and_import_fields(self):
        # Every one of S9's goldens is under S9's single golden entry, with
        # its three patterns: the format goldens of the API and the export
        # round trip (testdata/formats/), the proposal payloads and the
        # import-field classification. The round trip has a golden for each
        # document under docs/exports/ and none other, and those documents
        # are S9's frozen data. The tests that write the goldens are S9's
        # guard code, so P1's class D commit cannot relax what proves it
        # (X14b, class E, names them as its characterization); the other
        # tests beside them, and the code they pin, are not.
        s9 = ("S9", "export, import and report formats")
        files = subprocess.run(["git", "ls-files"], cwd=REPO, capture_output=True, text=True,
                               check=True).stdout.split("\n")
        files = [f for f in files if f and not f.endswith(".gitattributes")]
        formats = [f for f in files if f.startswith("internal/api/testdata/formats/")]
        payloads = [f for f in files if f.startswith("internal/api/testdata/proposal_payloads/")]
        roundtrip = [f for f in files if f.startswith("internal/persistence/postgres/testdata/formats/")]
        docs = [f for f in files if f.startswith("docs/exports/") and f.endswith(".json")]
        self.assertTrue(formats)
        self.assertEqual(sorted(os.path.basename(f) for f in payloads),
                         ["create_artifact.txt", "create_link.txt", "delete_artifact.txt", "delete_link.txt",
                          "record_test_result.txt", "update_artifact.txt"])
        self.assertEqual(len(docs), 4)
        self.assertEqual(sorted(os.path.basename(f) for f in roundtrip),
                         sorted(os.path.basename(f)[:-len(".json")] + ".txt" for f in docs))
        for path in formats + payloads + roundtrip + ["internal/domain/exports/testdata/import_fields.txt"]:
            with self.subTest(path=path):
                self.assertIn(path, files)
                self.assertEqual(rg.golden_entry(path), s9)
        for path in docs:
            with self.subTest(path=path):
                self.assertIsNone(rg.golden_entry(path))
                self.assertIn(("S9", "docs/exports/*.json"),
                              [(step, p) for step, p in rg.FROZEN_DATA if rg.matches(path, [p])])
        writers = ("internal/api/formats_fixture_test.go", "internal/api/formats_views_test.go",
                   "internal/api/formats_golden_test.go", "internal/api/proposal_payloads_test.go",
                   "internal/domain/exports/import_fields_test.go",
                   "internal/persistence/postgres/export_roundtrip_test.go")
        for path in writers:
            with self.subTest(path=path):
                self.assertTrue(os.path.isfile(os.path.join(REPO, path)), path)
                self.assertEqual(rg.guard_code_step(path), "S9")
        for path in ("internal/api/export_handlers_test.go", "internal/api/vv_result_handler_test.go",
                     "internal/domain/exports/import_test.go", "internal/domain/exports/export.go",
                     "internal/domain/reports/pdf_report.go", "internal/persistence/postgres/testdb_test.go"):
            with self.subTest(path=path):
                self.assertIsNone(rg.guard_code_step(path))
        self.assertEqual(rg.guard_code_step("internal/persistence/postgres/migration_freeze_test.go"), "S3")

    def test_merged_s14d_generator_is_guarded(self):
        # S14d's generator (internal/tools/liftmigrations: the tool, its
        # tests, its fixture and goldens) is guard code of its row, every
        # tracked file of it, so M10's class A and B commits cannot edit what
        # proves them; the sibling tools are not S14d's (declmove is no guard
        # code, movecheck is S14c's), and neither is M10's declmove spec,
        # which its class A commit adds.
        files = subprocess.run(["git", "ls-files"], cwd=REPO, capture_output=True, text=True,
                               check=True).stdout.split("\n")
        mine = [f for f in files if f.startswith("internal/tools/liftmigrations/")]
        for f in ("internal/tools/liftmigrations/main.go", "internal/tools/liftmigrations/lift.go",
                  "internal/tools/liftmigrations/verify.go"):
            self.assertIn(f, mine)
        for f in mine + ["internal/tools/liftmigrations/main_test.go", "internal/tools/liftmigrations/worktree_test.go",
                         "internal/tools/liftmigrations/testdata/fixture/migrations.go",
                         "internal/tools/liftmigrations/testdata/want/lift/migrations.go.golden"]:
            with self.subTest(path=f):
                self.assertEqual(rg.guard_code_step(f), "S14d")
        for f in ("internal/tools/declmove/main.go", "internal/tools/movecheck/main.go",
                  "internal/tools/declmove/specs/M10.json", "internal/persistence/postgres/migrations.go",
                  "internal/tools/liftmigrationsx/main.go"):
            with self.subTest(path=f):
                self.assertNotEqual(rg.guard_code_step(f), "S14d")
        self.assertTrue(rg.matches("internal/tools/declmove/specs/M10.json", rg.A_PATHS))

    def test_s14e_row_covers_splittools(self):
        # splittools's sources and tests are guard code of S14e's row, the
        # pattern matches tracked files, and a Go file added beside them is
        # covered with no edit to the row. M11a's spec is not guard code, so
        # the class B commit that regenerates M11a on a newer master may edit
        # it; the fixtures under testdata/ are frozen data instead.
        files = subprocess.run(["git", "ls-files", "internal/tools/splittools"], cwd=REPO, capture_output=True,
                               text=True, check=True).stdout.split()
        sources = [f for f in files if f.endswith(".go") and "/testdata/" not in f]
        self.assertTrue(sources)
        for path in sources + ["internal/tools/splittools/main_test.go", "internal/tools/splittools/new.go"]:
            with self.subTest(path=path):
                self.assertEqual(rg.guard_code_step(path), "S14e")
        for path in ("internal/tools/splittools/specs/M11a.json", "internal/tools/splittools/testdata/fixture/table.go",
                     "internal/tools/splittools/testdata/d11dee8/tools.go", "internal/tools/declmove/main.go"):
            with self.subTest(path=path):
                self.assertIsNone(rg.guard_code_step(path))
        self.assertTrue(rg.matches("internal/tools/splittools/testdata/d11dee8/tools.go", [p for _, p in rg.FROZEN_DATA]))

    def test_s12b_row_covers_its_guards_and_goldens(self):
        # S12b's cascade test sits in src/arch among S12's guards, so its row
        # comes before S12's and names it; bundle-check and its test are its
        # own; the cascade snapshot and the bundle shape are goldens. The
        # build identity's notion of a shipped frontend file leaves out what
        # never reaches the build.
        files = subprocess.run(["git", "ls-files", "frontend/src/arch", "frontend/scripts"], cwd=REPO,
                               capture_output=True, text=True, check=True).stdout.split()
        for path in ("frontend/src/arch/cssOrder.test.ts", "frontend/src/arch/sizeBudget.test.ts",
                     "frontend/scripts/bundle-check.mjs", "frontend/scripts/bundle-check.test.mjs"):
            with self.subTest(path=path):
                self.assertIn(path, files)
                self.assertEqual(rg.guard_code_step(path), "S12b")
        self.assertEqual(rg.guard_code_step("frontend/src/arch/routeTree.test.ts"), "S12")
        for path, step in (("frontend/src/arch/__snapshots__/cssOrder.txt", "S12, S12b, S16"),
                           ("frontend/scripts/testdata/bundle-shape.json", "S12b")):
            with self.subTest(golden=path):
                self.assertIn(path, files)
                self.assertEqual(rg.golden_entry(path)[0], step)
        for path, ships in (("frontend/src/App.tsx", True), ("frontend/src/index.css", True),
                            ("frontend/src/generated/contract.ts", True), ("frontend/src/App.test.tsx", False),
                            ("frontend/src/arch/repo.ts", False), ("frontend/src/test/mockApi.ts", False),
                            ("frontend/src/arch/__snapshots__/cssOrder.txt", False), ("frontend/src/vite-env.d.ts", False),
                            ("frontend/src/x/testdata/a.json", False), ("frontend/scripts/bundle-check.mjs", False),
                            ("frontend/vite.config.ts", False)):
            with self.subTest(ships=path):
                self.assertEqual(rg.ships_in_frontend(path), ships)

    def test_s13_row_covers_its_guards_and_goldens(self):
        # S13's Go vocabulary writer and its vitest parity test are S13's
        # guard code, the vitest named ahead of S12's src/arch row beside it;
        # what they pin, contracts/vocab.json and the allowed differences, are
        # goldens of the cross-language contracts entry S6 opened, so a class
        # C commit may add them but nothing in a refactor may change them.
        # S6's contract and S12's helpers stay theirs.
        files = subprocess.run(["git", "ls-files", "contracts", "internal/vocabparity", "frontend/src/arch"], cwd=REPO,
                               capture_output=True, text=True, check=True).stdout.split()
        for path in ("internal/vocabparity/vocab_test.go", "frontend/src/arch/vocabParity.test.ts"):
            with self.subTest(path=path):
                self.assertIn(path, files)
                self.assertEqual(rg.guard_code_step(path), "S13")
                self.assertTrue(rg.is_test_file(path), path)
        for path, step in (("frontend/src/arch/sseListeners.test.ts", "S12"), ("frontend/src/arch/repo.ts", "S12"),
                           ("internal/api/sse_contract_test.go", "S6")):
            with self.subTest(path=path):
                self.assertEqual(rg.guard_code_step(path), step)
        for path in ("contracts/vocab.json", "contracts/vocab-allowed-diffs.json", "contracts/sse-events.json"):
            with self.subTest(golden=path):
                self.assertIn(path, files)
                self.assertEqual(rg.golden_entry(path), ("S6, S13", "cross-language contracts"))
                self.assertTrue(rg.matches(path, rg.C_ADDED_ONLY), path)

    def test_chunk_names_and_differences(self):
        self.assertEqual(rg.chunk_name("ModuleView-BtaBQDBL.css"), "ModuleView.css")
        self.assertEqual(rg.chunk_name("ActivityLog-DhOwu-2u.js"), "ActivityLog.js")
        self.assertEqual(rg.chunk_name("ag-theme-quartz-_KkkAD1f.js"), "ag-theme-quartz.js")
        self.assertEqual(rg.chunk_name("plain.css"), "plain.css")
        d = rg.first_difference(b".a{}\n.b{}", b".a{}\n.c{}")
        self.assertIn("first difference at byte 6 (line 2)", d)
        self.assertIn("base: '.a{}\\n.b{}'", d)
        tmp = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, tmp, True)
        base, head = os.path.join(tmp, "base"), os.path.join(tmp, "head")
        for d_, files in ((base, {"index-AAAAAAAA.css": b"x", "Graph-BBBBBBBB.css": b"g", "a-CCCCCCCC.js": b"1",
                                  "a-CCCCCCCC.js.map": b"m", "Gone-DDDDDDDD.css": b"z", "Same-EEEEEEEE.css": b"s",
                                  "ag-theme-quartz-Bq7q_68f.css": b"q", "Twice-JJJJJJJJ.css": b"t"}),
                          (head, {"index-AAAAAAAA.css": b"x", "Graph-FFFFFFFF.css": b"G", "a-CCCCCCCC.js": b"2",
                                  "a-CCCCCCCC.js.map": b"M", "New-GGGGGGGG.css": b"n", "Same-EEEEEEEE.css": b"s",
                                  "agGridTheme-Bq7q_68f.css": b"q", "Copy-KKKKKKKK.css": b"t",
                                  "Other-JJJJJJJJ.css": b"t"})):
            os.makedirs(d_)
            for name, data in files.items():
                with open(os.path.join(d_, name), "wb") as f:
                    f.write(data)
        css = rg.compare_assets(base, head, (".css",))
        self.assertEqual([c for c, _, _ in css], ["Copy.css", "Gone.css", "Graph.css", "New.css", "Twice.css",
                                                  "ag-theme-quartz.css"])
        detail = {c: (d, k) for c, d, k in css}
        self.assertEqual(detail["Graph.css"], ("Graph-BBBBBBBB.css -> Graph-FFFFFFFF.css: 1 -> 1 bytes, chunk file names "
                                               "aside first difference at byte 0 (line 1):\n"
                                               "        base: 'g'\n        head: 'G'", "content"))
        self.assertEqual(detail["Gone.css"], ("Gone-DDDDDDDD.css is emitted only by the base: no file of the head's "
                                              "pairs with it by name, chunk or bytes", "only"))
        self.assertEqual(detail["New.css"], ("New-GGGGGGGG.css is emitted only by the head: no file of the base's "
                                             "pairs with it by name, chunk or bytes", "only"))
        # The bundler names a chunk several modules share after one of them:
        # a refactor that changes which renames it with every byte kept. That
        # pairs by bytes, the same content hash first, and is no difference;
        # a second head file with those bytes is still one only the head emits.
        self.assertEqual(detail["ag-theme-quartz.css"], ("ag-theme-quartz-Bq7q_68f.css -> agGridTheme-Bq7q_68f.css: the "
                                                         "same bytes under another chunk's name", "renamed"))
        self.assertEqual(detail["Twice.css"], ("Twice-JJJJJJJJ.css -> Other-JJJJJJJJ.css: the same bytes under another "
                                               "chunk's name", "renamed"))
        self.assertEqual(detail["Copy.css"], ("Copy-KKKKKKKK.css is emitted only by the head: no file of the base's "
                                              "pairs with it by name, chunk or bytes", "only"))
        both = rg.compare_assets(base, head, (".css", ".js"))
        self.assertIn(("a.js", "a-CCCCCCCC.js differs: 1 -> 1 bytes, chunk file names aside first difference at "
                                "byte 0 (line 1):\n        base: '1'\n        head: '2'", "content"), both)
        self.assertNotIn("a.js.map", [c for c, _, _ in both], ".map files embed the sources and are left out")
        # A chunk whose only change is the hashed name of a chunk it loads
        # echoes a change made elsewhere; one with a change of its own is
        # reported by the first byte that differs once those names are set aside.
        for d_, entry, lazy in ((base, b'import{a}from"./Graph-BBBBBBBB.js";x()', b'import"./index-AAAAAAAA.css";1'),
                                (head, b'import{a}from"./Graph-FFFFFFFF.js";x()', b'import"./index-AAAAAAAA.css";2')):
            for name, data in (("index-HHHHHHHH.js", entry), ("Lazy-IIIIIIII.js", lazy)):
                with open(os.path.join(d_, name), "wb") as f:
                    f.write(data)
        kinds = {c: k for c, _, k in rg.compare_assets(base, head, (".js",))}
        self.assertEqual(kinds, {"a.js": "content", "index.js": "references", "Lazy.js": "content"})
        self.assertEqual(rg.without_hashes(b'm.f=["assets/ModuleView-DTh24cR5.js","assets/ag-theme-quartz-Bq7q_68f.css"]'),
                         b'm.f=["assets/ModuleView.js","assets/ag-theme-quartz.css"]')

    def test_s14c_row_covers_stageextract_and_movecheck(self):
        # stageextract's and movecheck's sources and tests are guard code of
        # S14c's row: stageextract generates M4 and movecheck -flatten -base
        # proves it, so M4's class B commit may edit neither. A Go file added
        # beside them is covered with no edit to the row. M4's spec is not
        # guard code, so the pull request that regenerates M4 on a newer
        # master may edit it; the fixtures under testdata/ are frozen data.
        for tool in ("stageextract", "movecheck"):
            files = subprocess.run(["git", "ls-files", "internal/tools/" + tool], cwd=REPO, capture_output=True,
                                   text=True, check=True).stdout.split()
            sources = [f for f in files if f.endswith(".go") and "/testdata/" not in f]
            self.assertTrue(sources)
            for path in sources + ["internal/tools/%s/main_test.go" % tool, "internal/tools/%s/new.go" % tool]:
                with self.subTest(path=path):
                    self.assertEqual(rg.guard_code_step(path), "S14c")
        for path in ("internal/tools/stageextract/specs/M4.json", "internal/tools/stageextract/testdata/fixture/main.go",
                     "internal/tools/stageextract/testdata/want/main.go.golden",
                     "internal/tools/movecheck/testdata/normalise/base/main.go", "internal/tools/declmove/main.go",
                     "cmd/server/main.go", "cmd/server/wire_config.go"):
            with self.subTest(path=path):
                self.assertIsNone(rg.guard_code_step(path))
        self.assertTrue(rg.matches("internal/tools/stageextract/testdata/want/main.go.golden",
                                   [p for _, p in rg.FROZEN_DATA]))

    def test_s14f_row_covers_tsdeclmove(self):
        # tsdeclmove and its tests are guard code of S14f's row (a module
        # added beside them under the same name is covered with no edit to
        # the row), and S14a's TS proofs stay S14a's. F1's spec is not guard
        # code, so F1 may adjust it when it regenerates the move (R4); the
        # fixture, its goldens and the d11dee8 copy under testdata/ are
        # frozen data instead. The tool and the spec are class T, the tests
        # class C.
        self.assertTrue(os.path.isfile(os.path.join(REPO, "frontend/scripts/tsdeclmove.mjs")))
        for path in ("frontend/scripts/tsdeclmove.mjs", "frontend/scripts/tsdeclmove.test.mjs"):
            with self.subTest(path=path):
                self.assertEqual(rg.guard_code_step(path), "S14f")
        for path in ("frontend/scripts/tsdeclhash.mjs", "frontend/scripts/tsmovecheck.mjs"):
            with self.subTest(path=path):
                self.assertEqual(rg.guard_code_step(path), "S14a")
        for path in ("frontend/scripts/specs/F1.json", "frontend/scripts/testdata/tsdeclmove/fixture/src/api/client.ts",
                     "frontend/scripts/testdata/tsdeclmove/want/src/api/client.ts",
                     "frontend/scripts/testdata/tsdeclmove/d11dee8/src/api/client.ts", "frontend/src/api/client.ts"):
            with self.subTest(path=path):
                self.assertIsNone(rg.guard_code_step(path))
        self.assertTrue(rg.matches("frontend/scripts/testdata/tsdeclmove/d11dee8/src/api/client.ts",
                                   [p for _, p in rg.FROZEN_DATA]))
        self.assertTrue(rg.is_test_file("frontend/scripts/tsdeclmove.test.mjs"))
        for path in ("frontend/scripts/tsdeclmove.mjs", "frontend/scripts/specs/F1.json"):
            with self.subTest(path=path):
                self.assertTrue(rg.matches(path, rg.T_PATHS))
                self.assertFalse(rg.matches(path, rg.A_PATHS))

    def test_classification(self):
        self.assertEqual(rg.guard_code_step("internal/archtest/graph_test.go"), "S1")
        self.assertIsNone(rg.guard_code_step("internal/archtest/ratchets.json"))
        self.assertEqual(rg.guard_code_step("frontend/src/arch/repo.ts"), "S12")
        self.assertIsNone(rg.guard_code_step("internal/api/handlers_test.go"))
        self.assertTrue(rg.is_test_file("e2e/tests/smoke.spec.ts"))
        self.assertTrue(rg.is_test_file("frontend/scripts/tsdeclhash.test.mjs"))
        self.assertFalse(rg.is_test_file("frontend/src/api/client.ts"))
        self.assertEqual(rg.golden_entry("internal/notify/testdata/notifications/invite.txt")[0], "S10")
        self.assertEqual(rg.golden_entry("internal/domain/exports/testdata/formats/x.csv")[0], "S9")
        self.assertIsNone(rg.golden_entry("frontend/src/arch/testdata/backendDeepLinks.json"))

    # The next three read live files that later steps shrink in commits that
    # may not edit this file (class E may not edit guard code, and is alone in
    # its pull request): X4b and X15b-X15e remove allowlist entries, X2b fills
    # X2B_CALL_SHAPE_CHANGES, M4 drops cmd/server/main.go from ratchets.json.
    # So they check the shape and pick their entries at run time.

    def test_real_lint_allowlists_parse(self):
        with open(os.path.join(REPO, rg.LINT_ALLOWLIST_FILE)) as f:
            text = f.read()
        lines = text.split("\n")
        entry = re.compile(r"""^\s*(['"])([^'"]+)\1\s*:""")
        for name, kind in rg.LINT_ALLOWLISTS.items():
            with self.subTest(allowlist=name):
                span = rg.find_block(text, name)
                self.assertIsNotNone(span, f"{rg.LINT_ALLOWLIST_FILE} has no {name} literal")
                self.assertIsNone(rg.read_literal(text, name, kind)[1], f"{name} holds an entry the guard cannot read")
                entries = rg.parse_allowlist(text, name, kind)
                keyed = [i for i in range(span[0], span[1] + 1) if entry.match(lines[i])]
                self.assertEqual(len(entries), len(keyed), f"an entry of {name} does not parse: {entries}")
                if kind == "count":
                    self.assertTrue(all(n >= 1 for n in entries.values()), entries)
                else:
                    self.assertTrue(all(v and all(t.startswith("src/views/") for t in v) for v in entries.values()),
                                    entries)
                if not keyed:
                    continue  # emptied (X4b, X15e): nothing left to remove
                i = keyed[0]
                key = entry.match(lines[i]).group(2)
                # Removing an entry leaves the rest of the file as it was.
                shrunk = "\n".join(lines[:i] + lines[i + 1:])
                self.assertEqual(rg.blank_blocks(shrunk, rg.LINT_ALLOWLISTS),
                                 rg.blank_blocks(text, rg.LINT_ALLOWLISTS))
                self.assertEqual(rg.allowlist_growth(text, shrunk), [])
                self.assertEqual(rg.allowlist_growth(shrunk, text), [(name, f"adds '{key}'")])
                if kind == "count":
                    n = entries[key]
                    grown = "\n".join(lines[:i] + [lines[i].replace(f": {n}", f": {n + 1}", 1)] + lines[i + 1:])
                    self.assertEqual(rg.allowlist_growth(text, grown),
                                     [(name, f"raises '{key}' from {n} to {n + 1}")])

    def test_read_literal_reads_one_form(self):
        def read(body, kind="count"):
            return rg.read_literal("const X = {" + body + "};\nconst Y = 1;\n", "X", kind)

        self.assertEqual(read(""), ({}, None))
        self.assertEqual(read(" 'a': 1, \"b\": 20 "), ({"a": 1, "b": 20}, None))
        self.assertEqual(read("\n  // note\n\n  'a': 1, // was 2\n  'b': 0\n"), ({"a": 1, "b": 0}, None))
        self.assertEqual(read("\n  'a': ['x', \"y\"],\n  'b': [],\n  'c': [\n    'z',\n  ],\n", "list"),
                         ({"a": ["x", "y"], "b": [], "c": ["z"]}, None))
        for stray in ("a: 1,", "['a']: 1,", "...B,", "'a': 1 + 1,", "'a': 0x10,", "'a': 010,", "'a': 1_000,",
                      "'a': 1e3,", "'a': B,", "'a': 1 /* x */,", "'a\\u0062': 1,", "'a': 1 'b': 2,"):
            with self.subTest(stray=stray):
                self.assertEqual(read(f"\n  'k': 3,\n  {stray}\n  'm': 4,\n"), ({"k": 3, "m": 4}, stray))
        for stray in ("'a': ['x', ...B],", "'a': ['x'].concat(['y']),", "'a': B,", "'a': ['x' + 'y'],"):
            with self.subTest(stray=stray):
                self.assertEqual(read(f"\n  {stray}\n", "list"), ({}, stray))
        self.assertEqual(rg.read_literal("const X = {\n  'a': 1,\n} as const;\n\nfunction f() {\n}\n", "X",
                                         "count"), ({"a": 1}, "} as const;"))
        self.assertEqual(rg.read_literal("const Y = 1;\n", "X", "count"), ({}, None))

    def test_own_x2b_block_is_found(self):
        with open(os.path.join(REPO, "scripts/refactor/refactor_guard.py")) as f:
            text = f.read()
        span = rg.find_block(text, "X2B_CALL_SHAPE_CHANGES")
        self.assertIsNotNone(span)
        lines = text.split("\n")
        # Filled the way X2b fills it, whatever it holds by then.
        filled = "\n".join(lines[:span[0]] + ["X2B_CALL_SHAPE_CHANGES = [", '    ("GET /x: -", "GET /x: session"),',
                                              "]"] + lines[span[1] + 1:])
        self.assertNotEqual(filled, text)
        self.assertEqual(rg.blank_blocks(filled, ["X2B_CALL_SHAPE_CHANGES"]),
                         rg.blank_blocks(text, ["X2B_CALL_SHAPE_CHANGES"]))
        # The job reads the list from the pull request's copy of this file.
        self.assertIsInstance(rg.x2b_changes_in(text), list)
        self.assertEqual(rg.x2b_changes_in(filled), [("GET /x: -", "GET /x: session")])

    def test_ratchet_growth(self):
        with open(os.path.join(REPO, rg.RATCHETS_FILE)) as f:
            real = json.load(f)
        self.assertEqual(rg.ratchet_growth(real, real), [])
        raised = json.loads(json.dumps(real))
        raised["about"] = "changed text is fine"
        for key in list(raised["file_lines"])[:1]:
            del raised["file_lines"][key]  # removing is fine
        want = {("file_lines.internal/zz_not_a_package/new.go", "added")}
        raised["file_lines"]["internal/zz_not_a_package/new.go"] = 900
        for count in list(raised["counts"])[:1]:
            raised["counts"][count] += 1
            want.add((f"counts.{count}", "raised"))
        for pkg in list(raised["import_edges"])[:1]:
            raised["import_edges"][pkg].append("internal/zz_not_a_package")
            want.add((f"import_edges.{pkg}.internal/zz_not_a_package", "added"))
        got = {(rg.keypath(kp), kind) for kp, kind, _ in rg.ratchet_growth(real, raised)}
        self.assertEqual(got, want)


class WorkflowTest(unittest.TestCase):
    """The job's contract (plan §6.4 S14b), pinned in the workflow's text."""

    def read(self, path):
        with open(os.path.join(REPO, path)) as f:
            return f.read()

    def test_the_job(self):
        wf = self.read(".github/workflows/refactor-guard.yml")
        for line in [
            "    types: [opened, synchronize, reopened, labeled, unlabeled, ready_for_review]",
            "  group: refactor-guard-${{ github.event.pull_request.number }}",
            "  cancel-in-progress: true",
            "  contents: read",
            "  pull-requests: read",
            "    name: Refactor guard",
            "          fetch-depth: 0",
            "          go-version-file: go.mod",
            "        run: python3 -m unittest scripts/refactor/refactor_guard_test.py",
            "          labels=$(gh api \"repos/$REPO/pulls/$PR\" --jq '.labels[].name')",
            # The base's copy judges the pull request (its own when the base has none).
            "          guard=scripts/refactor/refactor_guard.py",
            "          if git cat-file -e \"HEAD^1:$guard\" 2>/dev/null; then",
            "            git show \"HEAD^1:$guard\" > \"$RUNNER_TEMP/base/$guard\"",
            "              git show HEAD^1:scripts/release_notes.py > \"$RUNNER_TEMP/base/scripts/release_notes.py\"",
            "            guard=\"$RUNNER_TEMP/base/$guard\"",
            "          python3 \"$guard\" --labels \"$LABELS\"",
            "          UPDATE_RATCHETS=1 go test -count=1 -run '^TestArchitecture$' ./internal/archtest",
            "            if ! git diff --exit-code -- internal/archtest/ratchets.json; then",
            "            git diff --quiet -- internal/archtest/ratchets.json || echo \"::warning "
            "file=internal/archtest/ratchets.json::ratchets.json can be tightened; run UPDATE_RATCHETS=1 go test "
            "./internal/archtest and commit it\"",
        ]:
            with self.subTest(line=line.strip()):
                self.assertTrue(line + "\n" in wf, f"refactor-guard.yml no longer has the line: {line.strip()}")
        self.assertFalse("pull_request_target" in wf, "the job must not run with the base's write token")

    def test_the_job_installs_the_frontend_for_the_build_identity(self):
        # The build identity needs frontend/node_modules: the job installs
        # them when the pull request is a refactor touching a shipped file
        # under frontend/src or a Vite or PostCSS config beside package.json,
        # the same files feeds_frontend_build() names.
        wf = self.read(".github/workflows/refactor-guard.yml")
        for line in [
            "            shipped=$(git diff --no-renames --name-only HEAD^1 HEAD -- frontend/src \\",
            "              | grep -Ev '\\.(test|spec)\\.tsx?$|\\.test\\.mjs$|_test\\.(go|py)$|^frontend/src/(arch|test)/|/testdata/|/__snapshots__/|\\.d\\.ts$' || true)",
            "            configs=$(git diff --no-renames --name-only HEAD^1 HEAD -- frontend \\",
            "              | grep -E '^frontend/(vite\\.config\\.|postcss\\.config\\.|\\.postcssrc)[^/]*$' || true)",
            "          if [ -n \"$shipped$configs\" ]; then build=true; else build=false; fi",
            "          echo \"build=$build\" >> \"$GITHUB_OUTPUT\"",
            "        if: steps.labels.outputs.ts == 'true' || steps.labels.outputs.build == 'true'",
        ]:
            with self.subTest(line=line.strip()):
                self.assertTrue(line + "\n" in wf, f"refactor-guard.yml no longer has the line: {line.strip()}")
        # The shell's filters keep exactly the files the guard builds for:
        # the first drops what does not ship from git's list of frontend/src,
        # the second keeps the configs from its list of frontend/.
        shipped = re.compile(re.search(r"grep -Ev '([^']+)'", wf).group(1))
        configs = re.compile(re.search(r"grep -E '([^']+)'", wf).group(1))
        for path in ("frontend/src/App.tsx", "frontend/src/index.css", "frontend/src/generated/contract.ts",
                     "frontend/src/a/b.spec.mjs", "frontend/src/App.test.tsx", "frontend/src/a.spec.ts",
                     "frontend/src/arch/repo.ts", "frontend/src/test/mockApi.ts", "frontend/src/x/testdata/a.json",
                     "frontend/src/arch/__snapshots__/a.txt", "frontend/src/vite-env.d.ts", "frontend/src/x.test.mjs",
                     "frontend/src/architecture.ts", "frontend/src/tests/x.ts", "frontend/src/postcss.config.js",
                     "frontend/postcss.config.js", "frontend/postcss.config.mts", "frontend/.postcssrc",
                     "frontend/.postcssrc.json", "frontend/vite.config.js", "frontend/vite.config.ts",
                     "frontend/vite.config.test.mjs", "frontend/tsconfig.json", "frontend/.env.production",
                     "frontend/eslint.config.js", "frontend/scripts/postcss.config.js", "frontend/a/vite.config.js",
                     "frontend/package.json", "postcss.config.js", "docs/.postcssrc"):
            with self.subTest(filter=path):
                kept = ((path.startswith("frontend/src/") and not shipped.search(path)) or
                        (path.startswith("frontend/") and bool(configs.search(path))))
                self.assertEqual(kept, rg.feeds_frontend_build(path))
                if path.startswith("frontend/src/"):
                    self.assertEqual(not shipped.search(path), rg.ships_in_frontend(path))
        for path, feeds in (("frontend/postcss.config.js", True), ("frontend/.postcssrc.yml", True),
                            ("frontend/vite.config.mjs", True), ("frontend/tsconfig.json", False),
                            ("frontend/.env", False), ("frontend/src/App.test.tsx", False)):
            with self.subTest(feeds=path):
                self.assertEqual(rg.feeds_frontend_build(path), feeds)

    def test_bundle_check_runs_after_the_build(self):
        # S12b's bundle-check reads what `npm run build` wrote, in CI's
        # frontend job and in make check.
        ci = self.read(".github/workflows/ci.yml")
        self.assertRegex(ci, r"run: npm run build\n\n(?:      #[^\n]*\n)*      - name: Bundle shape \(S12b\)\n"
                             r"        run: node scripts/bundle-check\.mjs\n")
        self.assertIn("\tcd frontend && npm run build\n\tcd frontend && node scripts/bundle-check.mjs\n",
                      self.read("Makefile"))

    def test_the_tests_run_where_classify_commits_test_runs(self):
        for path in (".github/workflows/ci.yml", "Makefile"):
            with self.subTest(path=path):
                self.assertTrue("scripts/refactor/classify_commits_test.py scripts/refactor/refactor_guard_test.py"
                                in self.read(path), f"{path} no longer runs refactor_guard_test.py")


class GoldenTest(RepoTest):
    def test_non_refactor_golden_change_needs_a_note(self):
        self.commit("feature", {"internal/api/testdata/routes.txt": "GET /a\nGET /b\n"})
        f = self.assertFailsWith(self.guard("no-release-notes"), "(1) golden freeze", "without a release note")
        self.assertEqual(f.path, "internal/api/testdata/routes.txt")
        self.assertEqual(f.commit[1], "feature")

    def test_non_refactor_golden_change_with_a_note_passes(self):
        self.commit("feature", {"internal/api/testdata/routes.txt": "GET /a\nGET /b\n",
                                "RELEASE_NOTES.md": NOTES_WITH_BULLET})
        self.assertPasses(self.guard())

    def test_behavior_change_label_passes(self):
        self.commit("test-only", {"internal/api/testdata/routes.txt": "GET /a\nGET /b\n"})
        self.assertPasses(self.guard("no-release-notes", "behavior-change"))

    def test_adding_a_golden_passes(self):
        self.commit("new golden", {"internal/mcp/testdata/new.json": "{}\n"})
        self.assertPasses(self.guard("no-release-notes"))

    def test_renamed_golden_counts_as_a_delete(self):
        self.git("mv", "internal/api/testdata/routes.txt", "internal/api/testdata/routes2.txt")
        self.commit("rename")
        self.assertFailsWith(self.guard("no-release-notes"), "(1) golden freeze", "deleted a golden")

    def test_refactor_golden_change_fails_even_with_a_note(self):
        self.commit("move", {"internal/api/testdata/routes.txt": "GET /b\n", "RELEASE_NOTES.md": NOTES_WITH_BULLET},
                    trailers("B"))
        g = self.guard(*REFACTOR)
        self.assertFailsWith(g, "(1) golden freeze", "in a refactor pull request")
        self.assertFailsWith(g, "(2) protected path", "RELEASE_NOTES")

    def test_refactor_with_behavior_change_fails(self):
        self.commit("docs", {"docs/guide.md": "# Guide\n\nMore.\n"}, trailers("T"))
        self.assertFailsWith(self.guard("refactor:tooling", "behavior-change"), "(1) golden freeze", "both")

    def test_s13_allowed_differences_are_a_golden_not_an_allowlist(self):
        # contracts/vocab-allowed-diffs.json only shrinks as drift is fixed,
        # but fixing one is a behavior change: removing an entry needs a
        # release note like any golden change, and a refactor pull request
        # may not make it, unlike an entry of S12's lint allowlists.
        allowed = ('{\n  "diffs": [\n    { "vocabulary": "plans", "copy": "PLANS", "item": "free", "side": "go" },\n'
                   '    { "vocabulary": "plans", "copy": "PLANS", "item": "team", "side": "go" }\n  ]\n}\n')
        self.git("checkout", "-q", "main")
        self.commit("S13", {"contracts/vocab.json": "{}\n", "contracts/vocab-allowed-diffs.json": allowed})
        self.git("checkout", "-q", "pr")
        self.git("reset", "-q", "--hard", "main")
        shrunk = {"contracts/vocab-allowed-diffs.json": allowed.replace(
            '    { "vocabulary": "plans", "copy": "PLANS", "item": "team", "side": "go" }\n', "").replace('"go" },', '"go" }')}
        self.commit("fix", shrunk, trailers("C"))
        f = self.assertFailsWith(self.guard(*REFACTOR, "refactor:test"), "(1) golden freeze", "in a refactor pull request")
        self.assertEqual(f.path, "contracts/vocab-allowed-diffs.json")

    def test_s13_allowed_differences_shrink_with_a_release_note(self):
        allowed = '{\n  "diffs": [\n    { "vocabulary": "plans", "copy": "PLANS", "item": "team", "side": "go" }\n  ]\n}\n'
        self.git("checkout", "-q", "main")
        self.commit("S13", {"contracts/vocab.json": "{}\n", "contracts/vocab-allowed-diffs.json": allowed})
        self.git("checkout", "-q", "pr")
        self.git("reset", "-q", "--hard", "main")
        self.commit("fix", {"contracts/vocab-allowed-diffs.json": '{\n  "diffs": []\n}\n'})
        self.assertFailsWith(self.guard("no-release-notes"), "(1) golden freeze", "without a release note")
        self.git("checkout", "-q", "pr")
        self.git("branch", "-q", "-D", "merge")
        self.commit("note", {"RELEASE_NOTES.md": NOTES_WITH_BULLET})
        self.assertPasses(self.guard())

    def test_inline_snapshot_fails_on_any_pull_request(self):
        self.commit("test", {"frontend/src/views/App.test.tsx": "expect(x).toMatchInlineSnapshot(`1`);\n"})
        f = self.assertFailsWith(self.guard("no-release-notes"), "K16 inline snapshot", "toMatchInlineSnapshot")
        self.assertEqual(f.path, "frontend/src/views/App.test.tsx")

    def test_inline_snapshot_found_whatever_the_color_config(self):
        # make check runs with the user's git config.
        self.git("config", "color.ui", "always")
        self.git("config", "color.diff", "always")
        self.commit("test", {"frontend/src/views/App.test.tsx": "expect(x).toMatchInlineSnapshot(`1`);\n"})
        self.assertFailsWith(self.guard("no-release-notes"), "K16 inline snapshot", "toMatchInlineSnapshot")

    def test_x2b_exception_is_empty_until_filled(self):
        self.commit("x2b", {"internal/api/testdata/route_guards.txt": "GET /a: session\nGET /b: session\n"},
                    trailers("E", Refactor_Characterization="internal/api/handlers_test.go"))
        self.assertFailsWith(self.guard(*REFACTOR), "(1) golden freeze", "route_guards")

    X2B_FILLED = GUARD_PY.replace("X2B_CALL_SHAPE_CHANGES = [\n]",
                                  'X2B_CALL_SHAPE_CHANGES = [\n    ("GET /b: -", "GET /b: session"),\n]')

    def test_x2b_listed_lines_are_exempt_in_class_e(self):
        # X2b fills the list in its own class E commit, and the job, which
        # runs the base's copy of the script, reads it from the pull request.
        self.commit("x2b", {"internal/api/testdata/route_guards.txt": "GET /a: session\nGET /b: session\n",
                            "scripts/refactor/refactor_guard.py": self.X2B_FILLED},
                    trailers("E", Refactor_Characterization="internal/api/handlers_test.go"))
        g = self.guard(*REFACTOR)
        self.assertPasses(g)
        self.assertTrue(any("X2b" in n for n in g.notes))

    def test_x2b_list_is_the_pull_requests_not_the_running_scripts(self):
        self.addCleanup(setattr, rg, "X2B_CALL_SHAPE_CHANGES", rg.X2B_CALL_SHAPE_CHANGES)
        rg.X2B_CALL_SHAPE_CHANGES = [("GET /b: -", "GET /b: session")]
        self.commit("x2b", {"internal/api/testdata/route_guards.txt": "GET /a: session\nGET /b: session\n"},
                    trailers("E", Refactor_Characterization="internal/api/handlers_test.go"))
        self.assertFailsWith(self.guard(*REFACTOR), "(1) golden freeze", "route_guards")

    def test_x2b_exception_does_not_cover_other_lines(self):
        self.commit("x2b", {"internal/api/testdata/route_guards.txt": "GET /a: -\nGET /b: session\n",
                            "scripts/refactor/refactor_guard.py": self.X2B_FILLED},
                    trailers("E", Refactor_Characterization="internal/api/handlers_test.go"))
        self.assertFailsWith(self.guard(*REFACTOR), "(1) golden freeze", "route_guards")

    def test_x2b_list_that_does_not_parse_counts_as_empty(self):
        self.commit("x2b", {"internal/api/testdata/route_guards.txt": "GET /a: session\nGET /b: session\n",
                            "scripts/refactor/refactor_guard.py": self.X2B_FILLED.replace('"),', '")+1,')},
                    trailers("E", Refactor_Characterization="internal/api/handlers_test.go"))
        g = self.guard(*REFACTOR)
        self.assertFailsWith(g, "(1) golden freeze", "route_guards")
        self.assertTrue(any("X2B_CALL_SHAPE_CHANGES" in w for w in g.warnings), g.warnings)


class ClassTest(RepoTest):
    def test_missing_trailer(self):
        self.commit("tidy", {"docs/guide.md": "# Guide\n\nx\n"})
        f = self.assertFailsWith(self.guard(*REFACTOR), "R2 class trailer", "no Refactor-Class")
        self.assertEqual(f.commit[1], "tidy")

    def test_class_line_outside_the_trailer_block(self):
        self.commit("tidy", {"docs/guide.md": "# Guide\n\nx\n"},
                    "Refactor-Class: T\n\nSigned-off-by: T <t@example.com>")
        f = self.assertFailsWith(self.guard(*REFACTOR), "R2 class trailer", "outside its trailer block")
        self.assertNotIn("has no Refactor-Class", f.message)

    def test_unknown_and_double_class(self):
        self.commit("one", {"docs/guide.md": "a\n"}, trailers("X"))
        self.commit("two", {"docs/guide.md": "b\n"}, "Refactor-Class: T\nRefactor-Class: C\nSigned-off-by: T <t@e>")
        g = self.guard(*REFACTOR)
        self.assertFailsWith(g, "R2 class trailer", "unknown class 'X'")
        self.assertFailsWith(g, "R2 class trailer", "declares 2 classes")

    def test_class_c(self):
        self.commit("tests", {"internal/api/new_test.go": "package api\n", "frontend/src/arch/extra.ts": "x\n",
                              "internal/api/testdata/new.txt": "x\n", "e2e/tests/a.spec.ts": "x\n"}, trailers("C"))
        self.assertPasses(self.guard("refactor", "refactor:test"))

    def test_class_c_rejects_production_code_and_modified_data(self):
        self.commit("tests", {"internal/api/handlers.go": "package api\n\nfunc A() { _ = 1 }\n",
                              "internal/domain/exports/testdata/in.json": "{\"a\":1}\n",
                              "frontend/src/arch/testdata/fixture.json": "{\"a\":1}\n"}, trailers("C"))
        g = self.guard("refactor", "refactor:test")
        self.assertFailsWith(g, "class C", "internal/api/handlers.go")
        self.assertFailsWith(g, "class C", "exports/testdata/in.json: a test-only commit may add files")
        # S12's helpers are class C, but their fixtures are still frozen data.
        self.assertFailsWith(g, "(2) frozen data", "arch/testdata/fixture.json")
        self.assertFailsWith(g, "(2) frozen data", "exports/testdata/in.json")

    def test_class_t(self):
        self.commit("tooling", {"docs/guide.md": "x\n", ".github/workflows/x.yml": "on: push\n",
                                "scripts/refactor/new.py": "x\n",
                                "internal/tools/declhash/main.go": "package main\n\n"}, trailers("T"))
        self.assertPasses(self.guard("refactor", "refactor:tooling"))

    def test_class_t_type_only_typescript_passes_the_build_identity(self):
        # X5's type-only assertions: TypeScript under frontend/src that the
        # build erases, proved by identical JS and CSS from base and head.
        self.commit("types", {"frontend/src/api/wireCompat.ts": "export type X = 1;\nexport interface Y { a: X }\n",
                              "frontend/src/views/App.tsx": "import type { X } from '../api/wireCompat';\n"
                                                            "export const App = 1;\n"}, trailers("T"))
        g = self.guard("refactor", "refactor:tooling", "no-release-notes")
        self.assertPasses(g)
        self.assertEqual(self.builder.calls, ["base", "head"])
        self.assertEqual(self.builder.cleaned, 1)
        self.assertTrue(any("same 4 *.css and *.js file(s)" in n for n in g.notes), g.notes)

    def test_class_t_typescript_that_changes_the_build_fails(self):
        self.commit("types", {"frontend/src/api/wireCompat.ts": "export type X = 1;\nexport const k = 2;\n",
                              "internal/api/handlers.go": "package api\n\nfunc B() {}\n"}, trailers("T"))
        g = self.guard("refactor", "refactor:tooling")
        f = self.assertFailsWith(g, "S12b build identity", "index.js")
        self.assertIn("type-only (class T) change must leave the emitted JavaScript and CSS byte-identical", f.fix)
        self.assertEqual(f.path, "frontend/build/assets/index.js")
        self.assertFailsWith(g, "class T", "handlers.go")
        self.assertFalse(any("index.css" in x.render() for x in g.failures), "the CSS did not change")
        # The lazy chunk loads the entry by its hashed name: an echo, not a change of its own.
        echo = self.assertFailsWith(g, "S12b build identity", "differ only in the hashed file names")
        self.assertIn("Lazy.js", echo.message)
        self.assertEqual(echo.fix, "fix the difference above; these follow from it")
        self.assertFalse(any(x.path == "frontend/build/assets/Lazy.js" for x in g.failures))

    def test_class_t_leaves_frontend_tests_and_test_helpers_to_class_c(self):
        # The build identity proves what the production build erases; it never
        # builds a test, src/arch or src/test, so their edits are class C's,
        # whose reviewer reads the assertions. A declaration file is erased
        # whole, so class T may change it.
        for path in ("frontend/src/views/App.test.tsx", "frontend/src/arch/repo.ts", "frontend/src/test/mockApi.ts",
                     "frontend/src/x/testdata/fixture.ts"):
            with self.subTest(path=path):
                self.setUp()
                self.commit("types", {path: "export type X = 1;\n"}, trailers("T"))
                g = self.guard("refactor", "refactor:tooling")
                f = self.assertFailsWith(g, "class T", path)
                self.assertIn("this file is a test or test-only code, which is class C", f.message)
                self.assertEqual(self.builder.calls, [])
        self.setUp()
        self.commit("types", {"frontend/src/vite-env.d.ts": "declare const X: 1;\n"}, trailers("T"))
        self.assertPasses(self.guard("refactor", "refactor:tooling"))
        self.assertEqual(self.builder.calls, [])

    def test_class_t_rejects_other_frontend_src_files(self):
        self.commit("style", {"frontend/src/index.css": ".button { color: red }\n"}, trailers("T"))
        g = self.guard("refactor", "refactor:tooling")
        self.assertIn("not TypeScript", self.assertFailsWith(g, "class T", "frontend/src/index.css").render())

    def test_class_t_type_only_mixed_with_a_build_change_fails(self):
        # The identity is the pull request's, base against head: a commit that
        # changes the JavaScript sinks a type-only commit beside it.
        self.commit("types", {"frontend/src/api/wireCompat.ts": "export type X = 1;\n"}, trailers("T"))
        self.commit("extract", {"frontend/src/views/App.tsx": "export const App = 1 + 0;\n"}, trailers("B"))
        g = self.guard(*REFACTOR)
        self.assertIn("pull request of its own", self.assertFailsWith(g, "S12b build identity", "index.js").fix)

    def test_class_a_runs_the_hashers(self):
        self.commit("move", {"internal/api/handlers.go": "package api\n", "internal/api/moved.go":
                             "package api\n\nfunc A() {}\n", "frontend/src/views/App.tsx": "export const App = 1;\n\n"},
                    trailers("A"))
        h = StubHashers()
        self.assertPasses(self.guard("refactor", "refactor:move", hashers=h))
        self.assertEqual(h.calls, [("go", ["internal/api"]), ("ts",)])

    def test_class_a_fails_when_a_declaration_differs(self):
        self.commit("move", {"internal/api/handlers.go": "package api\n\nfunc A() { _ = 2 }\n"}, trailers("A"))
        self.assertFailsWith(self.guard("refactor", "refactor:move", hashers=StubHashers(go_ok=False)), "class A",
                             "declhash differs")

    def test_class_a_rejects_other_files(self):
        self.commit("move", {"docs/guide.md": "moved\n"}, trailers("A"))
        self.assertFailsWith(self.guard("refactor", "refactor:move"), "class A", "only Go and frontend/src")

    def test_class_b_rejects_goldens_and_guard_code(self):
        self.commit("extract", {"internal/api/handlers.go": "package api\n\nfunc A() { b() }\n\nfunc b() {}\n",
                                "internal/api/route_binding_test.go": "package api\n\n// changed guard\n"},
                    trailers("B"))
        f = self.assertFailsWith(self.guard(*REFACTOR), "(2) guard code", "route_binding_test.go")
        self.assertIn("class B commit", f.message)

    def test_guard_code_in_c_passes_unless_a_golden_changes_with_it(self):
        self.commit("guard", {"internal/api/route_binding_test.go": "package api\n\n// better\n"}, trailers("C"))
        self.assertPasses(self.guard("refactor", "refactor:test"))

    def test_guard_code_in_c_with_a_golden(self):
        self.commit("guard", {"internal/api/route_binding_test.go": "package api\n\n// better\n",
                              "internal/api/testdata/routes.txt": "GET /z\n"}, trailers("C"))
        g = self.guard("refactor", "refactor:test")
        self.assertFailsWith(g, "(2) guard code", "also modifies or deletes a golden")
        self.assertFailsWith(g, "(1) golden freeze")

    def test_a_guard_step_refines_what_it_added(self):
        # S2, S6, S7 and S12 each added a golden and its test in one commit
        # and refined both in a later one: neither was frozen yet.
        self.commit("pin", {"internal/mcp/testdata/tools.json": "[]\n", "internal/mcp/golden_test.go": "package mcp\n"},
                    trailers("C"))
        self.commit("tighten", {"internal/mcp/testdata/tools.json": "[1]\n",
                                "internal/mcp/golden_test.go": "package mcp\n\n// tighter\n"}, trailers("C"))
        self.assertPasses(self.guard("refactor", "refactor:test"))

    def test_class_b_golden(self):
        self.commit("extract", {"internal/api/testdata/routes.txt": "GET /z\n"}, trailers("B"))
        self.assertFailsWith(self.guard(*REFACTOR), "class B", "changes a golden")

    def test_protected_paths(self):
        self.commit("bump", {"go.mod": "module example.com/x\n\ngo 1.25\n", "Dockerfile.api": "RUN go build ./x\n"},
                    trailers("T"))
        g = self.guard("refactor", "refactor:tooling")
        self.assertFailsWith(g, "(2) protected path", "go.mod")
        # Dockerfile.api is protected only once the base builds by package path (M1).
        self.assertFalse(any(f.path == "Dockerfile.api" for f in g.failures))

    def test_protected_package_files_and_templates(self):
        self.commit("deps", {"frontend/package.json": "{\"name\": \"x\", \"dependencies\": {\"left-pad\": \"1\"}}\n",
                             "frontend/package-lock.json": "{\"lockfileVersion\": 3, \"packages\": {}}\n",
                             "examples/x/project.json": "{\"name\": \"x\"}\n"}, trailers("B"))
        g = self.guard(*REFACTOR)
        for path in ("frontend/package.json", "frontend/package-lock.json", "examples/x/project.json"):
            with self.subTest(path=path):
                self.assertEqual(self.assertFailsWith(g, "(2) protected path", path).path, path)
        # Only a refactor is held to them.
        other = rg.Guard(g.git, g.base, g.head, g.merge, ["no-release-notes"], StubHashers())
        other.run()
        self.assertPasses(other)

    def test_dockerfile_protected_after_m1(self):
        self.git("checkout", "-q", "main")
        self.commit("M1", {"Dockerfile.api": "RUN go build -o server ./cmd/server\n"})
        self.git("checkout", "-q", "pr")
        self.git("merge", "-q", "--no-edit", "main")
        self.commit("edit", {"Dockerfile.api": "RUN go build -o srv ./cmd/server\n"}, trailers("T"))
        self.assertFailsWith(self.guard("refactor", "refactor:tooling"), "(2) protected path", "after M1")

    def test_e_needs_an_unchanged_characterization(self):
        self.commit("extract", {"internal/api/handlers.go": "package api\n\nfunc A() {}\nfunc C() {}\n"},
                    trailers("E", Refactor_Characterization="internal/api/handlers_test.go"))
        self.assertPasses(self.guard("refactor"))

    def test_e_failures(self):
        self.commit("extract", {"internal/api/handlers_test.go": "package api\n\n// edited\n"},
                    trailers("E", Refactor_Characterization=["internal/api/handlers_test.go",
                                                             "internal/api/new_test.go", "internal/api/handlers.go"]))
        g = self.guard("refactor")
        self.assertFailsWith(g, "class E", "is changed by this pull request")
        self.assertFailsWith(g, "class E", "is not on the base")
        self.assertFailsWith(g, "class E", "is not a test file")

    def test_e_without_trailer_and_mixed(self):
        self.commit("extract", {"internal/api/handlers.go": "package api\n\nfunc A() {}\nfunc C() {}\n"},
                    trailers("E"))
        self.commit("docs", {"docs/guide.md": "x\n"}, trailers("T"))
        g = self.guard("refactor", "refactor:tooling")
        self.assertFailsWith(g, "class E", "names no Refactor-Characterization")
        self.assertFailsWith(g, "R2 class E alone", "mixed with class T")

    def test_labels_are_compared_with_classes(self):
        self.commit("docs", {"docs/guide.md": "x\n"}, trailers("T"))
        g = self.guard("refactor", "refactor:move")
        self.assertPasses(g)
        self.assertTrue(any("'refactor:tooling'" in w for w in g.warnings))
        self.assertTrue(any("'refactor:move'" in w for w in g.warnings))

    def test_non_refactor_pull_requests_skip_class_checks(self):
        self.commit("feature", {"internal/api/handlers.go": "package api\n\nfunc Z() {}\n"})
        self.assertPasses(self.guard("no-release-notes"))


class BuildIdentityTest(RepoTest):
    def test_a_refactor_that_keeps_the_css_passes_and_js_may_change(self):
        # A class B extraction changes the JavaScript; only the CSS is held.
        self.commit("extract", {"frontend/src/views/App.tsx": "const one = () => 1;\nexport const App = one();\n"},
                    trailers("B"))
        g = self.guard(*REFACTOR)
        self.assertPasses(g)
        self.assertTrue(any("same 2 *.css file(s)" in n for n in g.notes), g.notes)

    def test_a_reordered_stylesheet_fails_in_the_entry(self):
        # ProjectList.css after index.css is the cascade I20 pins; a refactor
        # that moves it ahead (here: renamed so it sorts first) changes the
        # entry's CSS bytes.
        self.commit("move", {"frontend/src/views/ProjectList.css": None,
                             "frontend/src/a/ProjectList.css": ".button { color: blue; }\n"}, trailers("B"))
        g = self.guard(*REFACTOR)
        f = self.assertFailsWith(g, "S12b build identity", "index.css")
        self.assertIn("first difference at byte", f.message)
        self.assertIn("keep every stylesheet import in its module and in its order", f.fix)

    def test_a_reordered_stylesheet_fails_in_a_lazy_chunk(self):
        self.commit("lazy", {"frontend/src/views/lazy/Aa.css": ".aa { color: black; }\n"}, trailers("B"))
        g = self.guard(*REFACTOR)
        f = self.assertFailsWith(g, "S12b build identity", "Lazy.css")
        self.assertIn("Lazy-", f.message)
        self.assertFalse(any("index.css" in x.render() for x in g.failures), "the entry's CSS did not change")

    def test_a_shared_chunk_renamed_with_the_same_css_passes(self):
        # The bundler names a chunk several modules share after one of them,
        # so an extraction can rename it with every byte of its CSS kept.
        self.commit("extract", {"frontend/src/views/App.tsx": "const one = () => 1;\nexport const App = one();\n"},
                    trailers("B"))
        g = self.guard(*REFACTOR, builder=StubBuilder(rename={"head": {"Lazy": "Shared"}}))
        self.assertPasses(g)
        self.assertTrue(any("same 2 *.css file(s) (.map files left out, and chunk names aside)" in n for n in g.notes),
                        g.notes)
        self.assertTrue(any(re.search(r"1 file\(s\) keep every byte under another chunk's name \(the bundler names "
                                      r"a shared chunk after one of its modules\): Lazy-\S+\.css -> Shared-\S+\.css$", n)
                            for n in g.notes), g.notes)

    def test_a_renamed_chunk_whose_css_changed_is_named_on_each_side(self):
        self.commit("lazy", {"frontend/src/views/lazy/Aa.css": ".aa { color: black; }\n"}, trailers("B"))
        g = self.guard(*REFACTOR, builder=StubBuilder(rename={"head": {"Lazy": "Shared"}}))
        f = self.assertFailsWith(g, "S12b build identity", "build/assets/Lazy.css: Lazy-")
        self.assertIn("is emitted only by the base: no file of the head's pairs with it", f.message)
        self.assertIn("keep every stylesheet import in its module and in its order", f.fix)
        self.assertFailsWith(g, "S12b build identity", "build/assets/Shared.css: Shared-")
        self.assertFalse(any("not byte-identical" in x.message for x in g.failures), "nothing paired, so nothing differs")

    def test_an_added_postcss_config_that_drops_a_rule_fails(self):
        # The build reads more than frontend/src: a PostCSS config the pull
        # request adds (the protected-path rule judges only files the base
        # has) rewrites every stylesheet, I20's .button override among them.
        self.commit("config", {"frontend/postcss.config.js": "remove .button\n"}, trailers("B"))
        g = self.guard(*REFACTOR)
        self.assertEqual(self.builder.calls, ["base", "head"])
        f = self.assertFailsWith(g, "S12b build identity", "build/assets/index.css")
        self.assertIn("first difference at byte 0", f.message)
        self.assertFalse(any(x.rule == "(2) protected path" for x in g.failures), "an added file is not modified")

    def test_only_a_refactor_touching_shipped_frontend_files_builds(self):
        for labels, files, builds in (
                (("no-release-notes",), {"frontend/src/index.css": ".x{}\n"}, False),  # not a refactor
                (REFACTOR, {"frontend/src/views/App.test.tsx": "test('a', () => {});\n"}, False),
                (REFACTOR, {"frontend/src/arch/extra.ts": "export const e = 1;\n"}, False),
                (REFACTOR, {"docs/guide.md": "# Guide\n\nmore\n"}, False),
                (REFACTOR, {"frontend/tsconfig.json": "{}\n"}, False),
                (REFACTOR, {"frontend/.postcssrc.json": "{}\n"}, True),
                (REFACTOR, {"frontend/vite.config.js": "export default {};\n"}, True),
                (REFACTOR, {"frontend/src/views/App.tsx": "export const App = 3;\n"}, True)):
            with self.subTest(labels=labels, files=list(files)):
                self.setUp()
                self.commit("change", files, trailers("C" if "test" in next(iter(files)) or "arch" in
                                                      next(iter(files)) else "B"))
                self.guard(*labels)
                self.assertEqual(self.builder.calls, ["base", "head"] if builds else [])

    def test_a_build_that_fails_or_writes_nothing_fails(self):
        self.commit("extract", {"frontend/src/views/App.tsx": "export const App = 4;\n"}, trailers("B"))
        g = self.guard(*REFACTOR, builder=StubBuilder(fail="head"))
        self.assertIn("npm ERR!", self.assertFailsWith(g, "S12b build identity", "cannot build the head").message)
        self.assertEqual(self.builder.cleaned, 1)
        for side in ("base", "head"):
            with self.subTest(empty=side):
                g = rg.Guard(g.git, g.base, g.head, g.merge, list(REFACTOR), StubHashers(), StubBuilder(empty=side))
                g.run()
                self.assertFailsWith(g, "S12b build identity", f"the {side}'s build wrote no build/assets")


@unittest.skipUnless(shutil.which("git") and shutil.which("sh"), "git or sh is not installed")
class BuilderTest(RepoTest):
    """The real FrontendBuilder: git archive, the node_modules link and
    `npm run build`, with a stand-in npm on PATH that "builds" by
    concatenating the exported tree's stylesheets in path order."""

    NPM = """#!/bin/sh
[ "$1 $2" = "run build" ] || { echo "unexpected: npm $*" >&2; exit 64; }
[ -L node_modules ] && [ -d node_modules ] || { echo "node_modules is not linked" >&2; exit 65; }
[ -f src/views/App.tsx ] || { echo "not the exported frontend/" >&2; exit 66; }
mkdir -p build/assets
cat $(find src -name '*.css' | LC_ALL=C sort) > build/assets/index-AAAAAAAA.css
echo "built $(pwd)"
"""

    def setUp(self):
        super().setUp()
        self.bin = tempfile.mkdtemp(prefix="fake-npm-")
        self.addCleanup(shutil.rmtree, self.bin, True)
        npm = os.path.join(self.bin, "npm")
        with open(npm, "w") as f:
            f.write(self.NPM)
        os.chmod(npm, os.stat(npm).st_mode | stat.S_IEXEC)
        path = os.environ["PATH"]
        os.environ["PATH"] = self.bin + os.pathsep + path
        self.addCleanup(os.environ.__setitem__, "PATH", path)

    def real_guard(self, *labels):
        base, head, merge = self.merged()
        self.builder = rg.FrontendBuilder()
        g = rg.Guard(rg.Git(self.dir), base, head, merge, list(labels), StubHashers(), self.builder)
        g.run()
        return g

    def test_needs_node_modules(self):
        self.commit("extract", {"frontend/src/views/App.tsx": "export const App = 5;\n"}, trailers("B"))
        g = self.real_guard(*REFACTOR)
        self.assertIn("run `npm ci` in frontend/", self.assertFailsWith(g, "S12b build identity").message)

    def test_builds_base_and_head_and_cleans_up(self):
        os.makedirs(os.path.join(self.dir, "frontend", "node_modules", "vite"))  # untracked, as after npm ci
        self.commit("extract", {"frontend/src/views/App.tsx": "export const App = 6;\n"}, trailers("B"))
        g = self.real_guard(*REFACTOR)
        self.assertPasses(g)
        self.assertTrue(any("same 1 *.css file(s)" in n for n in g.notes), g.notes)
        self.assertEqual(self.builder.dirs, [])
        # A stylesheet moved so that it sorts, and so loads, earlier.
        self.git("checkout", "-q", "pr")
        self.git("branch", "-D", "merge")
        self.commit("move", {"frontend/src/views/ProjectList.css": None,
                             "frontend/src/a/ProjectList.css": ".button { color: blue; }\n"}, trailers("B"))
        g = self.real_guard(*REFACTOR)
        f = self.assertFailsWith(g, "S12b build identity", "index.css")
        self.assertIn("index-AAAAAAAA.css differs", f.message)


class RatchetTest(RepoTest):
    def ratchets(self, change):
        r = json.loads(json.dumps(RATCHETS))
        change(r)
        return {"internal/archtest/ratchets.json": json.dumps(r, indent=2) + "\n"}

    def test_lowering_passes_in_any_class(self):
        self.commit("lower", self.ratchets(lambda r: r["counts"].update(raw_json_encodes=200)), trailers("B"))
        self.assertPasses(self.guard(*REFACTOR))

    def test_raising_or_adding_fails(self):
        def change(r):
            r["counts"]["raw_json_encodes"] = 250
            r["side_effect_vars"].append("internal/x:y")
            r["file_lines"]["internal/api/new.go"] = 900
        self.commit("raise", self.ratchets(change), trailers("T"))
        g = self.guard("refactor", "refactor:tooling")
        self.assertFailsWith(g, "(2) ratchets", "raises ratchets.json entry counts.raw_json_encodes (249 -> 250)")
        self.assertFailsWith(g, "(2) ratchets", "side_effect_vars.internal/x:y")
        self.assertFailsWith(g, "(2) ratchets", "file_lines.internal/api/new.go")

    def test_class_d_new_package_entries(self):
        def change(r):
            r["import_edges"]["internal/domain/exports"].append("internal/domain/snapshot")
            r["import_edges"]["internal/domain/baselines"].append("internal/domain/snapshot")
            r["import_edges"]["internal/domain/snapshot"] = ["internal/domain/artifacts"]
            r["client_domain_deps"]["cmd/agentd"].append("internal/domain/snapshot")
        files = self.ratchets(change)
        files["internal/domain/snapshot/snapshot.go"] = "package snapshot\n"
        self.commit("P1", files, trailers("D"))
        self.assertPasses(self.guard("refactor"))

    def test_class_d_cannot_add_other_edges(self):
        def change(r):
            r["import_edges"]["internal/domain/exports"].append("internal/domain/snapshot")
            r["import_edges"]["internal/domain/snapshot"] = ["internal/api"]
            r["import_edges"]["internal/api"].append("internal/domain/links")
        files = self.ratchets(change)
        files["internal/domain/snapshot/snapshot.go"] = "package snapshot\n"
        self.commit("P1", files, trailers("D"))
        g = self.guard("refactor")
        self.assertFailsWith(g, "(2) ratchets", "import_edges.internal/domain/snapshot")
        self.assertFailsWith(g, "(2) ratchets", "import_edges.internal/api.internal/domain/links")
        self.assertEqual(len([f for f in g.failures if f.rule == "(2) ratchets"]), 2)

    def test_class_b_cannot_use_the_d_exception(self):
        def change(r):
            r["import_edges"]["internal/domain/exports"].append("internal/domain/snapshot")
        files = self.ratchets(change)
        files["internal/domain/snapshot/snapshot.go"] = "package snapshot\n"
        self.commit("sneaky", files, trailers("B"))
        self.assertFailsWith(self.guard("refactor"), "(2) ratchets", "internal/domain/snapshot")

    def test_restoring_the_base_value_within_the_pull_request(self):
        self.commit("lower", self.ratchets(lambda r: r["counts"].update(raw_json_encodes=200)), trailers("B"))
        self.commit("restore", self.ratchets(lambda r: None), trailers("B"))
        self.assertPasses(self.guard(*REFACTOR))

    def test_ratchets_created_by_the_pull_request(self):
        self.git("checkout", "-q", "main")
        self.git("rm", "-q", "internal/archtest/ratchets.json")
        self.commit("no ratchets yet")
        self.git("checkout", "-q", "pr")
        self.git("merge", "-q", "--no-edit", "main")
        self.commit("S1", self.ratchets(lambda r: None), trailers("T"))
        self.commit("S1 fix", self.ratchets(lambda r: r["counts"].update(raw_json_encodes=300)), trailers("T"))
        self.assertPasses(self.guard("refactor", "refactor:tooling"))

    def test_class_t_rule_key(self):
        files = self.ratchets(lambda r: r.update(helper_homes=["internal/api:respondJSON"]))
        files["internal/archtest/homes_test.go"] = "package archtest\n"
        self.commit("M5 rule", files, trailers("T"))
        self.assertPasses(self.guard("refactor", "refactor:tooling"))

    def test_class_t_rule_key_needs_the_rule(self):
        self.commit("key", self.ratchets(lambda r: r.update(helper_homes=[])), trailers("T"))
        self.assertFailsWith(self.guard("refactor", "refactor:tooling"), "(2) ratchets", "adds ratchets.json entry "
                             "helper_homes")


class AllowlistTest(RepoTest):
    def test_removing_an_entry_is_not_a_guard_code_edit(self):
        self.commit("X15b", {"frontend/eslint.config.js": ESLINT.replace("  'src/views/InterviewChat.tsx': 1,\n", "")},
                    trailers("E", Refactor_Characterization="frontend/src/arch/routeTree.test.ts"))
        self.assertPasses(self.guard("refactor"))

    def test_adding_or_raising_fails_in_any_class(self):
        grown = ESLINT.replace("'src/views/InterviewChat.tsx': 1", "'src/views/InterviewChat.tsx': 2").replace(
            "  'src/components/ProjectLayout.tsx': ['src/views/TodoList'],\n",
            "  'src/components/ProjectLayout.tsx': ['src/views/TodoList', 'src/views/X'],\n"
            "  'src/components/New.tsx': ['src/views/Y'],\n")
        self.commit("grow", {"frontend/eslint.config.js": grown}, trailers("T"))
        g = self.guard("refactor", "refactor:tooling")
        self.assertFailsWith(g, "(2) lint allowlist", "raises 'src/views/InterviewChat.tsx' from 1 to 2")
        self.assertFailsWith(g, "(2) lint allowlist", "adds 'src/views/X' to 'src/components/ProjectLayout.tsx'")
        self.assertFailsWith(g, "(2) lint allowlist", "adds 'src/components/New.tsx'")

    # The allowlists are carved out of guard code in every class, so the
    # guard reads every line there, as for the size ceilings: a quoted file
    # with a decimal count, or with a list of quoted targets.

    def allowlists_in_class_b(self, old, new):
        text = ESLINT.replace(old, new)
        self.assertNotEqual(text, ESLINT)
        self.commit("allow", {"frontend/eslint.config.js": text}, trailers("B"))
        return self.guard(*REFACTOR)

    def assertOnlyForm(self, g, name, form, stray):
        f = self.assertFailsWith(g, "(2) lint allowlist", f"{name} may hold only {form} entries")
        self.assertIn(f"`{stray}`", f.message)
        self.assertEqual(len(g.failures), 1, [f.render() for f in g.failures])

    def test_an_arithmetic_count_fails_in_class_b(self):
        g = self.allowlists_in_class_b("'src/views/InterviewChat.tsx': 1", "'src/views/InterviewChat.tsx': 1 + 1")
        self.assertOnlyForm(g, "EVENT_SOURCE_SITES", "quoted-key: integer", "'src/views/InterviewChat.tsx': 1 + 1,")

    def test_a_computed_key_fails_in_class_b(self):
        g = self.allowlists_in_class_b("  'src/views/InterviewChat.tsx': 1,\n",
                                       "  'src/views/InterviewChat.tsx': 1,\n  [EVENT_SOURCE_HOOK + 'x']: 2,\n")
        self.assertOnlyForm(g, "EVENT_SOURCE_SITES", "quoted-key: integer", "[EVENT_SOURCE_HOOK + 'x']: 2,")

    def test_a_spread_target_list_fails_in_class_b(self):
        g = self.allowlists_in_class_b("['src/views/TodoList'],\n};", "['src/views/TodoList', ...VIEWS],\n};")
        self.assertOnlyForm(g, "COMPONENTS_IMPORTING_VIEWS", "quoted-key: [quoted strings]",
                            "'src/components/ProjectLayout.tsx': ['src/views/TodoList', ...VIEWS],")

    def test_an_entry_after_a_lone_cr_adds_an_allowance_in_class_b(self):
        g = self.allowlists_in_class_b("  'src/views/InterviewChat.tsx': 1,\n",
                                       "  'src/views/InterviewChat.tsx': 1, // Q21\r  'src/views/Login.tsx': 1,\n")
        self.assertFailsWith(g, "(2) lint allowlist", "adds 'src/views/Login.tsx'")
        self.assertFailsWith(g, "(2) guard code", "eslint.config.js")

    def test_a_target_after_a_hidden_line_end_adds_an_import_in_class_b(self):
        g = self.allowlists_in_class_b("  'src/components/ProjectLayout.tsx': ['src/views/TodoList'],\n",
                                       "  'src/components/ProjectLayout.tsx': ['src/views/TodoList'], // F2\u2029"
                                       "  'src/components/New.tsx': ['src/views/Y'],\n")
        self.assertFailsWith(g, "(2) lint allowlist", "adds 'src/components/New.tsx'")
        self.assertFailsWith(g, "(2) guard code", "eslint.config.js")

    def test_a_target_list_over_several_lines_is_read(self):
        g = self.allowlists_in_class_b("'src/components/ProjectLayout.tsx': ['src/views/TodoList'],",
                                       "'src/components/ProjectLayout.tsx': [\n    'src/views/TodoList',\n"
                                       "    'src/views/X',\n  ],")
        self.assertFailsWith(g, "(2) lint allowlist", "adds 'src/views/X' to 'src/components/ProjectLayout.tsx'")
        self.assertEqual(len(g.failures), 1, [f.render() for f in g.failures])

    def test_allowlists_created_by_the_pull_request(self):
        self.git("checkout", "-q", "main")
        self.commit("no allowlists yet", {"frontend/eslint.config.js": "export default [];\n"})
        self.git("checkout", "-q", "pr")
        self.git("merge", "-q", "--no-edit", "main")
        self.commit("S12", {"frontend/eslint.config.js": ESLINT}, trailers("T"))
        self.commit("S12 more", {"frontend/eslint.config.js": ESLINT.replace("'src/views/InterviewChat.tsx': 1",
                                                                            "'src/views/InterviewChat.tsx': 2")},
                    trailers("T"))
        self.assertPasses(self.guard("refactor", "refactor:tooling"))

    def test_other_edits_are_guard_code(self):
        self.commit("rule", {"frontend/eslint.config.js": ESLINT.replace("export default [];", "export default [1];")},
                    trailers("B"))
        self.assertFailsWith(self.guard("refactor"), "(2) guard code", "eslint.config.js")

    def test_x2b_block_edit_in_class_e_only(self):
        filled = GUARD_PY.replace("X2B_CALL_SHAPE_CHANGES = [\n]",
                                  'X2B_CALL_SHAPE_CHANGES = [\n    ("GET /b: -", "GET /b: session"),\n]')
        self.commit("X2b list", {"scripts/refactor/refactor_guard.py": filled},
                    trailers("E", Refactor_Characterization="internal/api/handlers_test.go"))
        self.assertPasses(self.guard("refactor"))

    def test_python_after_a_lone_cr_beside_the_x2b_block_fails_in_class_e(self):
        # Python, too, ends a line at a lone CR: OTHER = 2 is code, not part
        # of the comment on the X2b list's closer.
        self.commit("X2b list", {"scripts/refactor/refactor_guard.py":
                                 GUARD_PY.replace("X2B_CALL_SHAPE_CHANGES = [\n]",
                                                  "X2B_CALL_SHAPE_CHANGES = [\n]  # X2b fills this\rOTHER = 2")},
                    trailers("E", Refactor_Characterization="internal/api/handlers_test.go"))
        self.assertFailsWith(self.guard("refactor"), "(2) guard code", "refactor_guard.py")

    def test_x2b_block_edit_in_class_b_fails(self):
        filled = GUARD_PY.replace("X2B_CALL_SHAPE_CHANGES = [\n]",
                                  'X2B_CALL_SHAPE_CHANGES = [\n    ("GET /b: -", "GET /b: session"),\n]')
        self.commit("X2b list", {"scripts/refactor/refactor_guard.py": filled}, trailers("B"))
        self.assertFailsWith(self.guard("refactor"), "(2) guard code", "refactor_guard.py")


class CeilingTest(RepoTest):
    def test_lowering_is_not_a_guard_code_edit(self):
        self.commit("X15b", {"frontend/src/arch/errorChains.test.ts": ERROR_CHAINS.replace("53", "49")},
                    trailers("E", Refactor_Characterization="frontend/src/arch/routeTree.test.ts"))
        self.assertPasses(self.guard("refactor"))

    def test_raising_fails_in_any_class(self):
        self.commit("raise", {"frontend/src/arch/errorChains.test.ts": ERROR_CHAINS.replace("53", "54")},
                    trailers("C"))
        self.assertFailsWith(self.guard("refactor", "refactor:test"), "(2) guard ceiling", "from 53 to 54")

    def test_other_edits_are_guard_code(self):
        self.commit("edit", {"frontend/src/arch/errorChains.test.ts": ERROR_CHAINS.replace("// ratchet", "// r")},
                    trailers("B"))
        self.assertFailsWith(self.guard("refactor"), "(2) guard code", "errorChains.test.ts")

    def test_the_real_ceiling_is_found(self):
        with open(os.path.join(REPO, "frontend/src/arch/errorChains.test.ts")) as f:
            self.assertIsNotNone(rg.find_constant(f.read(), "CEILING"))

    # S12b's K14 size budgets: numeric constants, and maps whose entries
    # are ceilings that only fall (R6).

    SIZES = "frontend/src/arch/sizeBudget.test.ts"

    def test_lowering_or_removing_a_size_ceiling_is_not_a_guard_code_edit(self):
        self.commit("F8", {self.SIZES: SIZE_BUDGET.replace("'views/Login.tsx': 778", "'views/Login.tsx': 700")
                           .replace("  'Login': 733,\n", "").replace("OVER_1000 = 5", "OVER_1000 = 4")},
                    trailers("B"))
        self.assertPasses(self.guard(*REFACTOR))

    def test_raising_or_adding_a_size_ceiling_fails_in_any_class(self):
        self.commit("raise", {self.SIZES: SIZE_BUDGET.replace("'views/Login.tsx': 778", "'views/Login.tsx': 800")
                              .replace("'Login': 733,", "'Login': 733,\n  'Wizard': 400,")
                              .replace("FILE_BUDGET = 600", "FILE_BUDGET = 700")}, trailers("C"))
        g = self.guard("refactor", "refactor:test")
        self.assertFailsWith(g, "(2) guard ceiling", "raises 'views/Login.tsx' in FILE_CEILINGS from 778 to 800")
        self.assertFailsWith(g, "(2) guard ceiling", "adds 'Wizard' to COMPONENT_CEILINGS")
        self.assertFailsWith(g, "(2) guard ceiling", "raises FILE_BUDGET from 600 to 700")
        self.assertEqual(len(g.failures), 3, [f.render() for f in g.failures])

    # The ceiling maps are carved out of guard code in every class, so the
    # guard reads every line there: anything but 'key': <decimal integer>,
    # comments aside, fails in any class, since a key or value the guard
    # cannot read would hide a ceiling raised or added.

    def size_ceilings_in_class_b(self, old, new):
        text = SIZE_BUDGET.replace(old, new)
        self.assertNotEqual(text, SIZE_BUDGET)
        self.commit("grandfather", {self.SIZES: text}, trailers("B"))
        return self.guard(*REFACTOR)

    def assertOnlyForm(self, g, name, stray):
        f = self.assertFailsWith(g, "(2) guard ceiling", f"{name} may hold only quoted-key: integer entries")
        self.assertIn(f"`{stray}`", f.message)
        self.assertEqual(len(g.failures), 1, [f.render() for f in g.failures])

    def test_an_unquoted_size_ceiling_key_fails_in_class_b(self):
        g = self.size_ceilings_in_class_b("  'Login': 733,\n", "  Login: 900,\n")
        self.assertOnlyForm(g, "COMPONENT_CEILINGS", "Login: 900,")

    def test_an_arithmetic_size_ceiling_fails_in_class_b(self):
        g = self.size_ceilings_in_class_b("'views/Login.tsx': 778", "'views/Login.tsx': 778 + 400")
        self.assertOnlyForm(g, "FILE_CEILINGS", "'views/Login.tsx': 778 + 400,")

    def test_a_hex_size_ceiling_fails_in_class_b(self):
        g = self.size_ceilings_in_class_b("'views/Login.tsx': 778", "'views/Login.tsx': 0xfff")
        self.assertOnlyForm(g, "FILE_CEILINGS", "'views/Login.tsx': 0xfff,")

    def test_a_spread_into_a_size_ceiling_map_fails_in_class_b(self):
        g = self.size_ceilings_in_class_b("  'Login': 733,\n", "  'Login': 733,\n  ...{ Wizard: 900 },\n")
        self.assertOnlyForm(g, "COMPONENT_CEILINGS", "...{ Wizard: 900 },")

    def test_a_size_ceiling_map_closed_otherwise_fails_in_class_b(self):
        # find_block then runs on to the next line holding only a brace, so
        # the carve-out would cover the code in between.
        g = self.size_ceilings_in_class_b("  'Login': 733,\n};",
                                          "  'Login': 733,\n} as const;\n\nfunction f() {\n  return 1;\n}")
        f = self.assertFailsWith(g, "(2) guard ceiling",
                                 "COMPONENT_CEILINGS may hold only quoted-key: integer entries")
        self.assertIn("`} as const;`", f.message)

    # A lone CR ends a line in JavaScript (and Python), and U+2028 and U+2029
    # end one in JavaScript: hidden in a // comment, one turns the rest of the
    # physical line into code. The guard reads such a line end as the line
    # end it is (HIDDEN_EOL), and never carves out a span holding one.

    def test_an_entry_after_a_hidden_line_end_raises_a_ceiling_in_class_b(self):
        g = self.size_ceilings_in_class_b("  'views/Login.tsx': 778,\n",
                                          "  'views/Login.tsx': 778, // F8 trims this next\u2028"
                                          "  'views/Login.tsx': 5000,\n")
        self.assertFailsWith(g, "(2) guard ceiling", "raises 'views/Login.tsx' in FILE_CEILINGS from 778 to 5000")
        self.assertFailsWith(g, "(2) guard code", self.SIZES)

    def test_an_entry_after_a_lone_cr_adds_a_ceiling_in_class_b(self):
        g = self.size_ceilings_in_class_b("  'Login': 733,\n", "  'Login': 733, // see F8\r  'Wizard': 900,\n")
        self.assertFailsWith(g, "(2) guard ceiling", "adds 'Wizard' to COMPONENT_CEILINGS")
        self.assertFailsWith(g, "(2) guard code", self.SIZES)

    def test_code_after_a_hidden_line_end_on_a_closer_fails_in_class_b(self):
        g = self.size_ceilings_in_class_b("  'views/Login.tsx': 778,\n};",
                                          "  'views/Login.tsx': 778,\n}; // end\u2029console.log('ran');")
        f = self.assertFailsWith(g, "(2) guard ceiling", "FILE_CEILINGS may hold only quoted-key: integer entries")
        self.assertIn("`console.log('ran');`", f.message)
        self.assertFailsWith(g, "(2) guard code", self.SIZES)

    def test_code_after_a_hidden_line_end_on_a_constant_fails_in_class_b(self):
        g = self.size_ceilings_in_class_b("const OVER_1000 = 5;", "const OVER_1000 = 5; // §10\u2028globalThis.ran = 1;")
        self.assertFailsWith(g, "(2) guard code", self.SIZES)

    def test_code_after_a_hidden_line_end_on_the_error_chains_ceiling_fails_in_class_b(self):
        self.commit("skip", {"frontend/src/arch/errorChains.test.ts":
                             ERROR_CHAINS.replace("const CEILING = 53;", "const CEILING = 53; //\u2028it.skip = it;")},
                    trailers("B"))
        self.assertFailsWith(self.guard(*REFACTOR), "(2) guard code", "errorChains.test.ts")

    def test_a_crlf_line_end_in_a_size_ceiling_map_passes(self):
        g = self.size_ceilings_in_class_b("  'views/Login.tsx': 778,\n", "  'views/Login.tsx': 700, // F8\r\n")
        self.assertPasses(g)

    def test_a_map_failed_for_its_form_is_not_also_read_for_growth(self):
        # FILE_CEILINGS closed with `} as const;` runs on into
        # COMPONENT_CEILINGS; its one failure is the form, not 'adds' lines
        # for the next map's entries.
        g = self.size_ceilings_in_class_b("  'views/Login.tsx': 778,\n};", "  'views/Login.tsx': 778,\n} as const;")
        ceilings = [f for f in g.failures if f.rule == "(2) guard ceiling"]
        self.assertEqual(len(ceilings), 1, [f.render() for f in g.failures])
        self.assertIn("FILE_CEILINGS may hold only quoted-key: integer entries", ceilings[0].message)

    def test_a_quoted_size_ceiling_raised_in_class_b_fails(self):
        g = self.size_ceilings_in_class_b("'Login': 733", "'Login': 900")
        self.assertFailsWith(g, "(2) guard ceiling", "raises 'Login' in COMPONENT_CEILINGS from 733 to 900")
        self.assertEqual(len(g.failures), 1, [f.render() for f in g.failures])

    def test_comments_and_blank_lines_in_a_size_ceiling_map_pass(self):
        g = self.size_ceilings_in_class_b("  'views/Login.tsx': 778,\n",
                                          "\n  // F8 split the form out.\n  \"views/Login.tsx\": 700, // was 778\n")
        self.assertPasses(g)

    def test_a_size_ceiling_map_left_unreadable_is_failed_once(self):
        # The commit that made it unreadable fails; a later one that leaves
        # the literal as its parent has it is not failed again.
        self.commit("grandfather", {self.SIZES: SIZE_BUDGET.replace("'Login': 733", "Login: 900")}, trailers("B"))
        self.commit("later", {self.SIZES: SIZE_BUDGET.replace("'Login': 733", "Login: 900")
                              .replace("'views/Login.tsx': 778", "'views/Login.tsx': 700")}, trailers("B"))
        g = self.guard(*REFACTOR)
        f = self.assertFailsWith(g, "(2) guard ceiling", "COMPONENT_CEILINGS may hold only")
        self.assertEqual(f.commit[1], "grandfather")
        self.assertEqual(len(g.failures), 1, [f.render() for f in g.failures])

    def test_a_size_ceiling_removed_then_re_added_higher_fails(self):
        entry = "  'views/Login.tsx': 778,\n"
        self.commit("drop", {self.SIZES: SIZE_BUDGET.replace(entry, "")}, trailers("B"))
        self.commit("back", {self.SIZES: SIZE_BUDGET.replace("778", "790")}, trailers("C"))
        f = self.assertFailsWith(self.guard(*REFACTOR, "refactor:test"), "(2) guard ceiling",
                                 "raises 'views/Login.tsx' in FILE_CEILINGS from 778 to 790")
        self.assertEqual(f.commit[1], "back")

    def test_other_edits_to_the_size_budgets_are_guard_code(self):
        self.commit("edit", {self.SIZES: SIZE_BUDGET.replace("// ratchet", "// r")}, trailers("B"))
        self.assertFailsWith(self.guard(*REFACTOR), "(2) guard code", "sizeBudget.test.ts")

    def test_the_real_size_budgets_are_found(self):
        with open(os.path.join(REPO, self.SIZES)) as f:
            text = f.read()
        for name in rg.GUARD_CODE_CEILINGS[self.SIZES]:
            with self.subTest(name=name):
                self.assertTrue(rg.find_constant(text, name) or rg.find_block(text, name), name)
        self.assertEqual(rg.find_constant(text, "FILE_BUDGET")[1], 600)
        self.assertEqual(rg.find_constant(text, "COMPONENT_BUDGET")[1], 300)
        self.assertEqual(rg.find_constant(text, "OVER_1000")[1], 4)
        for name in ("FILE_CEILINGS", "COMPONENT_CEILINGS"):
            with self.subTest(name=name):
                self.assertIsNone(rg.read_literal(text, name, "count")[1], f"{name} holds an entry the guard cannot read")
        files = rg.parse_allowlist(text, "FILE_CEILINGS", "count")
        self.assertEqual(len(files), 16)
        self.assertNotIn("api/client.ts", files)  # F1 made it a barrel within budget
        self.assertEqual(files["views/Login.tsx"], 708 + 70)
        components = rg.parse_allowlist(text, "COMPONENT_CEILINGS", "count")
        self.assertEqual(len(components), 41)
        self.assertEqual(components["ModuleView"], 2076 + 150)  # F4 moved the filter engine out


class ReAddTest(RepoTest):
    """A file deleted in one commit and re-added in the next is judged
    against the base, which still has it, not against the empty parent."""

    def test_ratchets_deleted_then_re_added_raised(self):
        self.commit("grow", {"internal/api/handlers.go": "package api\n\nfunc A() {}\n\nfunc B() {}\n",
                             "internal/archtest/ratchets.json": None}, trailers("B"))
        r = json.loads(json.dumps(RATCHETS))
        r["counts"]["raw_json_encodes"] = 300
        r["import_edges"]["internal/api"].append("internal/domain/links")
        self.commit("bootstrap", {"internal/archtest/ratchets.json": json.dumps(r, indent=2) + "\n"}, trailers("B"))
        g = self.guard(*REFACTOR)
        f = self.assertFailsWith(g, "(2) ratchets", "raises ratchets.json entry counts.raw_json_encodes (249 -> 300)")
        self.assertEqual(f.commit[1], "bootstrap")
        self.assertFailsWith(g, "(2) ratchets", "import_edges.internal/api.internal/domain/links")

    def test_guard_code_deleted_in_c_then_re_added_changed_in_b(self):
        self.commit("drop", {"internal/api/route_binding_test.go": None}, trailers("C"))
        self.commit("adapt", {"internal/api/route_binding_test.go": "package api\n\n// looser guard\n",
                              "internal/api/handlers.go": "package api\n\nfunc A() { _ = 1 }\n"}, trailers("B"))
        f = self.assertFailsWith(self.guard(*REFACTOR), "(2) guard code", "route_binding_test.go")
        self.assertIn("re-adds, changed from the base, guard code (I1, S2) in a class B commit", f.message)

    def test_guard_code_restored_byte_identical_passes(self):
        self.commit("drop", {"internal/api/route_binding_test.go": None}, trailers("C"))
        self.commit("restore", {"internal/api/route_binding_test.go": "package api\n\n// guard\n",
                                "internal/api/handlers.go": "package api\n\nfunc A() { _ = 1 }\n"}, trailers("B"))
        self.assertPasses(self.guard("refactor", "refactor:test", "no-release-notes"))

    def test_ceiling_deleted_then_re_added_raised(self):
        self.commit("drop", {"frontend/src/arch/errorChains.test.ts": None}, trailers("C"))
        self.commit("back", {"frontend/src/arch/errorChains.test.ts": ERROR_CHAINS.replace("53", "90")},
                    trailers("C"))
        f = self.assertFailsWith(self.guard("refactor", "refactor:test"), "(2) guard ceiling", "from 53 to 90")
        self.assertEqual(f.commit[1], "back")

    def test_allowlist_deleted_then_re_added_raised(self):
        self.commit("drop", {"frontend/eslint.config.js": None}, trailers("T"))
        self.commit("back", {"frontend/eslint.config.js": ESLINT.replace("'src/views/InterviewChat.tsx': 1",
                                                                         "'src/views/InterviewChat.tsx': 5")},
                    trailers("T"))
        f = self.assertFailsWith(self.guard("refactor", "refactor:tooling"), "(2) lint allowlist",
                                 "raises 'src/views/InterviewChat.tsx' from 1 to 5")
        self.assertEqual(f.commit[1], "back")

    # An entry removed in one commit and re-added higher in the next is
    # 'added' against the parent and 'raised' against the base: both count.

    def ratchets_with(self, edit):
        r = json.loads(json.dumps(RATCHETS))
        edit(r)
        return json.dumps(r, indent=2) + "\n"

    def test_ratchet_entry_removed_then_re_added_raised(self):
        self.commit("drop", {"internal/archtest/ratchets.json":
                             self.ratchets_with(lambda r: r["file_lines"].pop("internal/api/handlers.go"))},
                    trailers("B"))
        self.commit("back", {"internal/archtest/ratchets.json":
                             self.ratchets_with(lambda r: r["file_lines"].update({"internal/api/handlers.go": 3671}))},
                    trailers("B"))
        f = self.assertFailsWith(self.guard(*REFACTOR), "(2) ratchets",
                                 "raises ratchets.json entry file_lines.internal/api/handlers.go (3519 -> 3671)")
        self.assertEqual(f.commit[1], "back")

    def test_ratchet_count_removed_then_re_added_raised(self):
        self.commit("drop", {"internal/archtest/ratchets.json":
                             self.ratchets_with(lambda r: r["counts"].pop("raw_json_encodes"))}, trailers("B"))
        self.commit("back", {"internal/archtest/ratchets.json":
                             self.ratchets_with(lambda r: r["counts"].update({"raw_json_encodes": 400}))},
                    trailers("B"))
        self.assertFailsWith(self.guard(*REFACTOR), "(2) ratchets",
                             "raises ratchets.json entry counts.raw_json_encodes (249 -> 400)")

    def test_ratchet_entry_removed_then_restored_passes(self):
        self.commit("drop", {"internal/archtest/ratchets.json":
                             self.ratchets_with(lambda r: r["file_lines"].pop("internal/api/handlers.go"))},
                    trailers("B"))
        self.commit("back", {"internal/archtest/ratchets.json": json.dumps(RATCHETS, indent=2) + "\n"},
                    trailers("B"))
        self.assertPasses(self.guard(*REFACTOR))

    def test_allowlist_entry_removed_then_re_added_raised(self):
        entry = "  'src/views/InterviewChat.tsx': 1,\n"
        self.commit("drop", {"frontend/eslint.config.js": ESLINT.replace(entry, "")}, trailers("B"))
        self.commit("back", {"frontend/eslint.config.js": ESLINT.replace(entry, entry.replace("1", "5"))},
                    trailers("B"))
        f = self.assertFailsWith(self.guard(*REFACTOR), "(2) lint allowlist",
                                 "raises 'src/views/InterviewChat.tsx' from 1 to 5")
        self.assertEqual(f.commit[1], "back")

    def test_allowlist_entry_removed_then_restored_passes(self):
        entry = "  'src/views/InterviewChat.tsx': 1,\n"
        self.commit("drop", {"frontend/eslint.config.js": ESLINT.replace(entry, "")}, trailers("B"))
        self.commit("back", {"frontend/eslint.config.js": ESLINT}, trailers("B"))
        self.assertPasses(self.guard(*REFACTOR))

    def test_ceiling_line_removed_then_re_added_raised(self):
        dropped = ERROR_CHAINS.replace("const CEILING = 53;\n", "")
        self.commit("drop", {"frontend/src/arch/errorChains.test.ts": dropped}, trailers("C"))
        self.commit("back", {"frontend/src/arch/errorChains.test.ts": ERROR_CHAINS.replace("53", "90")},
                    trailers("C"))
        f = self.assertFailsWith(self.guard("refactor", "refactor:test"), "(2) guard ceiling", "from 53 to 90")
        self.assertEqual(f.commit[1], "back")


class ScriptTest(RepoTest):
    RENAME = ("import pathlib, sys\n"
              "old, new = sys.argv[1], sys.argv[2]\n"
              "for p in pathlib.Path('internal').rglob('*.go'):\n"
              "    p.write_text(p.read_text().replace(old, new))\n")

    def test_class_r_reproduces_the_commit(self):
        self.commit("script", {"scripts/refactor/rename.py": self.RENAME}, trailers("T"))
        self.write({"internal/api/handlers.go": "package api\n\nfunc Renamed() {}\n"})
        self.commit("rename", body=trailers("R", Refactor_Script="scripts/refactor/rename.py 'func A' 'func Renamed'"))
        g = self.guard("refactor", "refactor:tooling", "refactor:script")
        self.assertPasses(g)
        self.assertEqual(self.git("worktree", "list").count("\n"), 0)  # the scratch worktree is gone

    def test_class_r_edit_after_the_script_fails(self):
        self.commit("script", {"scripts/refactor/rename.py": self.RENAME}, trailers("T"))
        self.write({"internal/api/handlers.go": "package api\n\nfunc Renamed() { _ = 1 }\n"})
        self.commit("rename", body=trailers("R", Refactor_Script="scripts/refactor/rename.py 'func A' 'func Renamed'"))
        f = self.assertFailsWith(self.guard("refactor", "refactor:tooling", "refactor:script"), "class R",
                                 "does not reproduce")
        self.assertIn("internal/api/handlers.go", f.message)

    def test_class_r_needs_the_script_in_its_parent(self):
        self.write({"scripts/refactor/rename.py": self.RENAME,
                    "internal/api/handlers.go": "package api\n\nfunc Renamed() {}\n"})
        self.commit("rename", body=trailers("R", Refactor_Script="scripts/refactor/rename.py 'func A' 'func Renamed'"))
        self.assertFailsWith(self.guard("refactor", "refactor:script"), "class R", "not in the commit's parent")

    def test_class_r_needs_one_trailer(self):
        self.commit("rename", {"internal/api/handlers.go": "package api\n\nfunc R() {}\n"}, trailers("R"))
        self.assertFailsWith(self.guard("refactor", "refactor:script"), "class R", "exactly one Refactor-Script")


class MergeTest(RepoTest):
    def test_a_clean_merge_of_master_needs_no_trailer(self):
        self.commit("docs", {"docs/guide.md": "x\n"}, trailers("T"))
        self.git("checkout", "-q", "main")
        self.commit("master moves", {"docs/other.md": "y\n"})
        self.git("checkout", "-q", "pr")
        self.git("merge", "-q", "--no-edit", "main")
        g = self.guard("refactor", "refactor:tooling")
        self.assertPasses(g)
        self.assertIn("merge", [g.row(c)[1] for c in g.commits])

    def test_an_evil_merge_is_a_change_of_its_own(self):
        self.commit("docs", {"docs/guide.md": "x\n"}, trailers("T"))
        self.git("checkout", "-q", "main")
        self.commit("master moves", {"docs/other.md": "y\n"})
        self.git("checkout", "-q", "pr")
        self.git("merge", "-q", "--no-edit", "--no-commit", "main")
        self.write({"internal/api/handlers.go": "package api\n\nfunc Evil() {}\n"})
        self.git("add", "-A")
        self.git("commit", "-q", "--no-edit")
        f = self.assertFailsWith(self.guard("refactor", "refactor:tooling"), "R2 class trailer",
                                 "git's own merge of its parents")
        self.assertIn("Merge", f.commit[1])
        self.assertIn("internal/api/handlers.go", f.message)

    def conflict(self):
        """A class B commit on the branch and a master commit that both edit
        handlers.go, then a merge of master into the branch that stops on
        the conflict."""
        self.commit("extract", {"internal/api/handlers.go": "package api\n\nfunc A() { b() }\n\nfunc b() {}\n"},
                    trailers("B"))
        self.git("checkout", "-q", "main")
        self.commit("fix", {"internal/api/handlers.go": "package api\n\nfunc A() { fixed() }\n\nfunc fixed() {}\n",
                            "docs/other.md": "y\n"})
        self.git("checkout", "-q", "pr")
        self.git("merge", "-q", "--no-edit", "main", may_fail=True)
        self.assertIn("internal/api/handlers.go", self.git("diff", "--name-only", "--diff-filter=U"))

    def test_a_conflict_settled_with_the_branch_side_is_a_change(self):
        # --cc shows nothing when the result equals one parent, yet this
        # drops master's fix from the pull request's net change.
        self.conflict()
        self.git("checkout", "--ours", "internal/api/handlers.go")
        self.git("add", "-A")
        self.git("commit", "-q", "--no-edit")
        self.assertEqual(self.git("diff-tree", "-r", "--cc", "--no-commit-id", "HEAD"), "")
        f = self.assertFailsWith(self.guard(*REFACTOR), "R2 class trailer", "git's own merge of its parents")
        self.assertIn("Merge", f.commit[1])
        self.assertIn("internal/api/handlers.go", f.message)
        self.assertNotIn("docs/other.md", f.message)

    def test_a_merge_with_strategy_ours_is_a_change(self):
        # Reverts master's guard-code change in a pull request whose only
        # commit is class B.
        self.commit("extract", {"internal/api/handlers.go": "package api\n\nfunc A() { b() }\n\nfunc b() {}\n"},
                    trailers("B"))
        self.git("checkout", "-q", "main")
        self.commit("guard", {"internal/api/route_binding_test.go": "package api\n\n// stricter guard\n"})
        self.git("checkout", "-q", "pr")
        self.git("merge", "-q", "--no-edit", "-s", "ours", "main")
        f = self.assertFailsWith(self.guard(*REFACTOR), "R2 class trailer", "git's own merge of its parents")
        self.assertIn("internal/api/route_binding_test.go", f.message)

    def test_a_conflict_settled_with_masters_copy_needs_no_trailer(self):
        self.conflict()
        self.git("checkout", "--theirs", "internal/api/handlers.go")
        self.git("add", "-A")
        self.git("commit", "-q", "--no-edit")
        g = self.guard(*REFACTOR)
        self.assertPasses(g)
        self.assertEqual([g.row(c) for c in g.commits], [("B", "ok"), ("-", "merge")])


class CliTest(RepoTest):
    def test_main_on_a_merge_commit(self):
        self.commit("docs", {"docs/guide.md": "x\n"}, trailers("T"))
        self.merged()
        summary = os.path.join(self.dir, "summary.md")
        cwd = os.getcwd()
        os.chdir(self.dir)
        try:
            out = io.StringIO()
            with redirect_stdout(out):
                rc = rg.main(["--labels", "refactor\nrefactor:tooling\nno-release-notes", "--summary", summary])
            with redirect_stdout(io.StringIO()):
                bad = rg.main(["--label", "refactor", "--summary", ""])  # class T without its label: still passes
            self.commit("golden", {"internal/api/testdata/routes.txt": "GET /q\n"})
            with redirect_stdout(io.StringIO()) as fail_out:
                failed = rg.main(["--base", "main", "--head", "HEAD", "--label", "no-release-notes", "--summary", ""])
            with redirect_stdout(io.StringIO()), redirect_stderr_to(io.StringIO()):
                usage = rg.main(["--summary", ""])  # HEAD is no longer a merge commit
        finally:
            os.chdir(cwd)
        self.assertEqual(rc, 0, out.getvalue())
        self.assertIn("Refactor guard passed", out.getvalue())
        self.assertRegex(out.getvalue(), r"\n[0-9a-f]+ +T +ok +docs")
        with open(summary) as f:
            self.assertIn("| T | ok | docs |", f.read())
        self.assertEqual(bad, 0)
        self.assertEqual(failed, 1, fail_out.getvalue())
        self.assertIn("without a release note", fail_out.getvalue())
        self.assertEqual(usage, 2)


class redirect_stderr_to:
    def __init__(self, stream):
        self.stream = stream

    def __enter__(self):
        self.old, sys.stderr = sys.stderr, self.stream

    def __exit__(self, *exc):
        sys.stderr = self.old


if __name__ == "__main__":
    unittest.main()
