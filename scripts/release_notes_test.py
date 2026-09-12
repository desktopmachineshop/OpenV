"""Tests for scripts/release_notes.py: python3 -m unittest scripts/release_notes_test.py"""

import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import release_notes as rn  # noqa: E402

SAMPLE = """# Notes

Preamble.

## Unreleased

- Pending one
  wrapped
- Pending two

## 2026-09-12

- Shipped
"""


class ParseTests(unittest.TestCase):
    def test_sections(self):
        unreleased, releases, _ = rn.parse(SAMPLE)
        self.assertEqual(unreleased, ["Pending one wrapped", "Pending two"])
        self.assertEqual(releases, [("2026-09-12", ["Shipped"])])

    def test_rejects_bad_heading_and_duplicates(self):
        for text in ("## Unreleased\n## v1\n", "## 2026-09-12\n## 2026-09-12\n", "# title only\n"):
            with self.assertRaises(rn.NotesError):
                rn.parse(text)


class CutTests(unittest.TestCase):
    def test_cut_moves_unreleased_into_dated_section(self):
        version, result = rn.cut(SAMPLE, "2026-09-13")
        self.assertEqual(version, "2026-09-13")
        unreleased, releases, _ = rn.parse(result)
        self.assertEqual(unreleased, [])
        self.assertEqual(releases[0], ("2026-09-13", ["Pending one wrapped", "Pending two"]))
        self.assertEqual(releases[1], ("2026-09-12", ["Shipped"]))
        self.assertIn("Preamble.", result)
        self.assertNotIn("\n\n\n", result)

    def test_same_day_gets_a_suffix(self):
        version, result = rn.cut(SAMPLE, "2026-09-12")
        self.assertEqual(version, "2026-09-12.2")
        version, _ = rn.cut(result.replace("## Unreleased\n", "## Unreleased\n\n- again\n"), "2026-09-12")
        self.assertEqual(version, "2026-09-12.3")

    def test_cut_refuses_empty_unreleased(self):
        _, cut_once = rn.cut(SAMPLE, "2026-09-13")
        with self.assertRaises(rn.NotesError):
            rn.cut(cut_once, "2026-09-14")


class CliTests(unittest.TestCase):
    def write(self, text):
        fd, path = tempfile.mkstemp(suffix=".md")
        with os.fdopen(fd, "w") as f:
            f.write(text)
        self.addCleanup(os.remove, path)
        return path

    def test_check_pr_needs_a_new_bullet(self):
        base = self.write(SAMPLE)
        same = self.write(SAMPLE)
        self.assertEqual(rn.main(["check-pr", "--base", base, same]), 1)
        added = self.write(SAMPLE.replace("- Pending two\n", "- Pending two\n- New in this PR\n"))
        self.assertEqual(rn.main(["check-pr", "--base", base, added]), 0)
        # A base branch with no notes file yet: any bullet counts.
        empty = self.write("")
        self.assertEqual(rn.main(["check-pr", "--base", empty, same]), 0)

    def test_check_release_compares_with_deployed(self):
        head = self.write(SAMPLE)
        deployed = self.write(SAMPLE)
        self.assertEqual(rn.main(["check-release", "--released", deployed, head]), 1)
        _, newer = rn.cut(SAMPLE, "2026-09-13")
        head2 = self.write(newer)
        self.assertEqual(rn.main(["check-release", "--released", deployed, head2]), 0)
        self.assertEqual(rn.main(["check-release", "--released", self.write(""), head]), 0)

    def test_cut_and_version_commands(self):
        path = self.write(SAMPLE)
        self.assertEqual(rn.main(["cut", "--date", "2026-09-13", path]), 0)
        self.assertEqual(rn.current_version(rn.read(path)), "2026-09-13")
        self.assertEqual(rn.main(["version", path]), 0)
        self.assertEqual(rn.main(["check", path]), 0)


if __name__ == "__main__":
    unittest.main()
