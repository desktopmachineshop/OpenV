"""Tests for refactor_guard.py.

Run: python3 -m unittest scripts/refactor/refactor_guard_test.py

Each test builds a throwaway repository shaped like a pull request: a base
branch, a pull request branch, and the merge commit GitHub checks out
(HEAD^1 the base, HEAD^2 the branch). The class A hashers are stubbed, so no
Go or Node toolchain runs here; the job itself runs the real ones.
"""

import io
import json
import os
import re
import shutil
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
            "frontend/src/arch/routeTree.test.ts": "test('x', () => {});\n",
            "frontend/src/arch/errorChains.test.ts": ERROR_CHAINS,
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

    def guard(self, *labels, hashers=None):
        base, head, merge = self.merged()
        g = rg.Guard(rg.Git(self.dir), base, head, merge, list(labels), hashers or StubHashers())
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
    def test_the_golden_list_has_20_entries(self):
        self.assertEqual(len(rg.GOLDEN_LIST), 20)

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
        merged = {"I1, pre-S2", "S2", "S3", "S4", "S5", "S6", "S6, S13", "S7", "S12, S12b, S16"}
        files = subprocess.run(["git", "ls-files"], cwd=REPO, capture_output=True, text=True,
                               check=True).stdout.split("\n")
        files = [f for f in files if f and not f.endswith(".gitattributes")]
        entries = [(step, what, patterns) for step, what, patterns in rg.GOLDEN_LIST if step in merged]
        self.assertEqual(len(entries), 15)
        for step, what, patterns in entries:
            with self.subTest(step=step, golden=what):
                self.assertTrue(any(rg.matches(f, patterns) for f in files),
                                f"no tracked file matches {patterns}: rename the entry with its golden")

    def test_merged_guard_code_exists(self):
        # A literal guard-code path of a merged step that no longer exists
        # would protect nothing; rename it here in the same commit.
        merged = {"S1", "I1, S2", "S3", "S4a", "S4b", "S5a-S5e", "S6", "S7", "S12", "S14a", "S14b"}
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

    def test_class_t_rejects_frontend_src_until_s12b(self):
        self.commit("types", {"frontend/src/api/wireCompat.ts": "export type X = 1;\n",
                              "internal/api/handlers.go": "package api\n\nfunc B() {}\n"}, trailers("T"))
        g = self.guard("refactor", "refactor:tooling")
        self.assertIn("S12b", self.assertFailsWith(g, "class T", "wireCompat").render())
        self.assertFailsWith(g, "class T", "handlers.go")

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
