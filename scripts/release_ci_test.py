"""The commit the release workflows judge by its CI (release_ci.py)."""
import os
import subprocess
import tempfile
import unittest

import release_ci as rc


class JudgedCommitTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = self.tmp.name
        self.git("init", "-q", "-b", "master")
        self.git("config", "user.email", "dev@example.com")
        self.git("config", "user.name", "Dev")
        self.code = self.commit("Feature (#711)", {"frontend/src/app.ts": "one"})

    def tearDown(self):
        self.tmp.cleanup()

    def git(self, *args, author=None):
        env = dict(os.environ)
        if author:
            env.update(GIT_AUTHOR_NAME=author, GIT_AUTHOR_EMAIL="bot@example.com")
        return subprocess.run(["git", *args], cwd=self.dir, check=True, capture_output=True, text=True, env=env).stdout.strip()

    def commit(self, message, files, author=None):
        for path, text in files.items():
            full = os.path.join(self.dir, path)
            os.makedirs(os.path.dirname(full) or self.dir, exist_ok=True)
            with open(full, "w") as f:
                f.write(text)
            self.git("add", path)
        self.git("commit", "-q", "-m", message, author=author)
        return self.git("rev-parse", "HEAD")

    def test_a_code_commit_is_judged_itself(self):
        self.assertEqual(rc.judged_commit(cwd=self.dir), self.code)

    def test_the_pipeline_commits_after_a_cut_are_skipped(self):
        # Cut stable release: the notes and the changelog, then the stable marker.
        self.commit("Release 0.27.2", {"RELEASE_NOTES.md": "a", "frontend/src/site/changelog/releases.json": "[]"}, author=rc.BOT)
        self.commit("Stable release 0.27.2", {"RELEASE_NOTES.md": "b"}, author=rc.BOT)
        self.commit("Manual documents 0.27", {"manual/opend.config.yaml": "line: 0.27"}, author=rc.BOT)
        self.assertEqual(rc.judged_commit(cwd=self.dir), self.code)

    def test_a_bot_commit_that_touches_code_is_judged(self):
        bot_code = self.commit("Bump", {"RELEASE_NOTES.md": "a", "go.mod": "module x"}, author=rc.BOT)
        self.assertEqual(rc.judged_commit(cwd=self.dir), bot_code)

    def test_a_person_editing_only_the_notes_is_judged(self):
        notes = self.commit("Fix a typo in the notes", {"RELEASE_NOTES.md": "typo"})
        self.assertEqual(rc.judged_commit(cwd=self.dir), notes)


if __name__ == "__main__":
    unittest.main()
