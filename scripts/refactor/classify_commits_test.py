"""Tests for classify_commits.py.

Run: python3 -m unittest scripts/refactor/classify_commits_test.py
"""

import io
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stdout

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import classify_commits as cc  # noqa: E402


class ClassifyPathTest(unittest.TestCase):
    def test_layers(self):
        cases = {
            "RELEASE_NOTES.md": "release notes",
            "docs/railway.md": "docs",
            "internal/api/README.md": "docs",
            "internal/api/handlers_test.go": "tests",
            "frontend/src/views/ModuleView.test.tsx": "tests",
            "e2e/tests/journeys.spec.ts": "tests",
            "internal/api/testdata/routes.txt": "tests",
            "scripts/release_notes_test.py": "tests",
            "internal/persistence/postgres/migrations.go": "migration/schema",
            "internal/persistence/postgres/migration_0048_billing.go": "migration/schema",
            "internal/persistence/postgres/schema_core.go": "migration/schema",
            "internal/persistence/postgres/org_repository.go": "repository",
            "internal/api/routes.go": "route registration",
            "internal/api/handlers.go": "handler",
            "internal/domain/orgs/orgs.go": "domain",
            "cmd/server/main.go": "main wiring",
            "cmd/server/wire_http.go": "main wiring",
            "internal/runner/worker.go": "services",
            "release_notes.go": "services",
            "go.mod": "tooling",
            ".github/workflows/ci.yml": "tooling",
            "Makefile": "tooling",
            "Dockerfile.api": "tooling",
            "scripts/refactor/classify_commits.py": "tooling",
            "frontend/scripts/tsmovecheck.mjs": "tooling",
            "frontend/package.json": "tooling",
            "frontend/src/api/client.ts": "frontend client",
            "frontend/src/api/types/artifacts.ts": "types",
            "frontend/src/components/crews/cytoscape-shim.d.ts": "types",
            "frontend/src/index.css": "CSS",
            "frontend/src/views/ModuleView.tsx": "views/components",
            "frontend/src/components/helpTopics.ts": "views/components",
            "frontend/src/App.tsx": "views/components",
            "frontend/src/utils/baselines.ts": "frontend other",
            "frontend/public/favicon.ico": "frontend other",
            "node_modules/x/y.json": "other",
        }
        for path, want in cases.items():
            with self.subTest(path=path):
                self.assertEqual(cc.classify_path(path), want)

    def test_every_rule_names_a_known_layer(self):
        for layer, _ in cc.RULES:
            self.assertIn(layer, cc.LAYERS)
        for layer, _, _ in cc.CONTENT_RULES:
            self.assertIn(layer, cc.LAYERS)


LOG = (
    "\x00aaa\x1fAda\x1f2026-09-01\x1fAdd billing routes\n\n"
    "internal/api/handlers.go\ninternal/api/billing_handlers.go\nfrontend/src/api/client.ts\n"
    "RELEASE_NOTES.md\n"
    "\x00bbb\x1fBo\x1f2026-09-02\x1fdocs: plan\n\ndocs/plans/x.md\n"
    "\x00ccc\x1fCy\x1f2026-09-03\x1fTypes only\n\nfrontend/src/views/X.tsx\n"
)


class SummaryTest(unittest.TestCase):
    def test_parse_classify_and_summarize(self):
        commits = cc.classify(cc.parse_log(LOG), {"route registration": {"aaa"}, "types": {"ccc"}})
        self.assertEqual([c["sha"] for c in commits], ["aaa", "bbb", "ccc"])
        self.assertEqual(commits[0]["layers"], ["release notes", "route registration", "handler", "frontend client"])
        self.assertEqual(commits[0]["hubs"], ["internal/api/handlers.go", "frontend/src/api/client.ts"])
        self.assertEqual(commits[2]["layers"], ["types", "views/components"])
        self.assertFalse(commits[1]["source"])
        s = cc.summarize(commits)
        self.assertEqual(s["commits"], 3)
        self.assertEqual(s["source_commits"], 2)
        self.assertEqual(s["any_hub"], 1)
        self.assertEqual(s["any_hub_source"], 1)
        self.assertEqual(s["hubs"]["frontend/src/App.tsx"], 0)
        self.assertEqual(s["files_per_commit"], {"mean": 2.0, "median": 1, "max": 4})
        text = cc.render(commits, s, "test")
        self.assertIn("any hub, of commits that change source", text)
        self.assertIn("50%", text)
        self.assertIn("hubs: handlers.go, client.ts", text)

    def test_empty_range(self):
        s = cc.summarize([])
        self.assertEqual(s["commits"], 0)
        self.assertIn("Commits: 0", cc.render([], s, "nothing"))


@unittest.skipUnless(shutil.which("git"), "git is not installed")
class GitTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.dir)

    def git(self, *args):
        subprocess.run(["git", "-c", "user.name=t", "-c", "user.email=t@example.com"] + list(args),
                       cwd=self.dir, check=True, capture_output=True)

    def commit(self, files, message):
        for path, text in files.items():
            full = os.path.join(self.dir, path)
            os.makedirs(os.path.dirname(full), exist_ok=True)
            with open(full, "w") as f:
                f.write(text)
        self.git("add", "-A")
        self.git("commit", "-q", "-m", message)

    def test_end_to_end(self):
        self.git("init", "-q")
        self.commit({"README.md": "hi\n"}, "readme")
        self.commit({"internal/api/handlers.go": 'package api\n\nfunc r() { m.HandleFunc("/x", h) }\n'}, "route")
        self.commit({"frontend/src/types.ts": "export interface A {\n  a: number;\n}\n"}, "types")
        cwd = os.getcwd()
        os.chdir(self.dir)
        try:
            out = io.StringIO()
            with redirect_stdout(out):
                self.assertEqual(cc.main(["-n", "2"]), 0)
            text = out.getvalue()
            with redirect_stdout(io.StringIO()) as js:
                self.assertEqual(cc.main(["--json", "HEAD~2..HEAD"]), 0)
        finally:
            os.chdir(cwd)
        self.assertIn("Commits: 2 (-n 2 HEAD, merges skipped)", text)
        self.assertIn("[route registration, handler]  hubs: handlers.go  route", text)
        self.assertIn("[types, frontend other]", text)
        self.assertIn('"any_hub": 1', js.getvalue())


if __name__ == "__main__":
    unittest.main()
