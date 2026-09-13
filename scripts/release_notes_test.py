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
        unreleased, releases, _, stables = rn.parse(SAMPLE)
        self.assertEqual(stables, [])
        self.assertEqual(unreleased, ["Pending one wrapped", "Pending two"])
        self.assertEqual(releases, [("2026-09-12", ["Shipped"])])

    def test_rejects_bad_heading_and_duplicates(self):
        for text in ("## Unreleased\n## v1\n", "## 2026-09-12\n## 2026-09-12\n", "# title only\n", "## 2026.09\n- no cut line\n"):
            with self.assertRaises(rn.NotesError):
                rn.parse(text)

    def test_stable_sections(self):
        text = "## Unreleased\n\n## 2026.09\n\nCut on 2026-10-01 from 2026-09-12.\n\n### Changes\n\n- a\n\n### Fixes\n\n- fix: b\n\n## 2026-09-12\n\n- a\n- fix: b\n"
        _, releases, _, stables = rn.parse(text)
        self.assertEqual(stables, [("2026.09", "2026-10-01", "2026-09-12", ["a", "fix: b"])])
        self.assertEqual(rn.current_version(text), "2026-09-12")
        self.assertEqual(rn.current_stable(text), "2026.09")


class CutTests(unittest.TestCase):
    def test_cut_moves_unreleased_into_dated_section(self):
        version, result = rn.cut(SAMPLE, "2026-09-13")
        self.assertEqual(version, "2026-09-13")
        unreleased, releases, _, _ = rn.parse(result)
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


class CutStableTests(unittest.TestCase):
    NIGHTLIES = """# Notes

## Unreleased

## 2026-09-26

- Too new to soak
- fix: a recent fix

## 2026-09-20.2

- Second on the twentieth

## 2026-09-20

- fix: Repaired the thing
- Twentieth change

## 2026-09-10

- Old change
"""

    def test_cut_stable_takes_the_soaked_nightly_and_merges_notes(self):
        version, result = rn.cut_stable(self.NIGHTLIES, "2026-10-01")
        self.assertEqual(version, "2026.10")
        _, releases, _, stables = rn.parse(result)
        self.assertEqual(len(stables), 1)
        v, cut_on, cut_from, bullets = stables[0]
        self.assertEqual((v, cut_on, cut_from), ("2026.10", "2026-10-01", "2026-09-20.2"))
        self.assertEqual(bullets, ["Second on the twentieth", "Twentieth change", "Old change", "fix: Repaired the thing"])
        self.assertIn("### Changes", result)
        self.assertIn("### Fixes", result)
        # The nightlies stay in the file; the stable sits under Unreleased.
        self.assertEqual([v for v, _ in releases], ["2026-09-26", "2026-09-20.2", "2026-09-20", "2026-09-10"])
        self.assertLess(result.index("## 2026.10"), result.index("## 2026-09-26"))
        self.assertNotIn("\n\n\n", result)

    def test_second_stable_covers_only_newer_nightlies(self):
        _, once = rn.cut_stable(self.NIGHTLIES, "2026-10-01")
        with self.assertRaises(rn.NotesError):
            rn.cut_stable(once, "2026-10-02")  # same month
        newer = once.replace("## 2026-09-26\n", "## 2026-10-20\n\n- November change\n\n## 2026-09-26\n")
        version, twice = rn.cut_stable(newer, "2026-11-02")
        self.assertEqual(version, "2026.11")
        _, _, _, stables = rn.parse(twice)
        newest = max(stables, key=lambda s: rn.stable_key(s[0]))
        self.assertEqual(newest[2], "2026-10-20")
        self.assertEqual(newest[3], ["November change", "Too new to soak", "fix: a recent fix"])

    def test_cut_stable_refuses_when_nothing_soaked_or_nothing_new(self):
        with self.assertRaises(rn.NotesError):
            rn.cut_stable(self.NIGHTLIES, "2026-09-12")  # nothing 7 days old
        _, once = rn.cut_stable(self.NIGHTLIES, "2026-10-01")
        with self.assertRaises(rn.NotesError):
            rn.cut_stable(once.replace("## 2026-09-26\n", "## 2026-09-19\n"), "2026-11-02")  # nothing newer than the cut

    def test_fix_release_takes_the_newest_nightly_now(self):
        _, once = rn.cut_stable(self.NIGHTLIES, "2026-10-01")
        version, result = rn.cut_stable(once, "2026-10-03", fix=True)
        self.assertEqual(version, "2026.10.1")
        _, _, _, stables = rn.parse(result)
        newest = max(stables, key=lambda s: rn.stable_key(s[0]))
        self.assertEqual(newest[2], "2026-09-26")
        self.assertEqual(newest[3], ["Too new to soak", "fix: a recent fix"])
        self.assertEqual(rn.current_stable(result), "2026.10.1")


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
        self.assertEqual(rn.main(["stable-version", path]), 0)

    def test_cut_stable_command_and_schedule_guard(self):
        path = self.write(CutStableTests.NIGHTLIES)
        # A weekend, scheduled: nothing happens and the command succeeds.
        self.assertEqual(rn.main(["cut-stable", "--date", "2026-10-03", "--if-scheduled", path]), 0)
        self.assertEqual(rn.current_stable(rn.read(path)), "")
        self.assertEqual(rn.main(["cut-stable", "--date", "2026-10-01", "--if-scheduled", path]), 0)
        self.assertEqual(rn.current_stable(rn.read(path)), "2026.10")
        # Already cut this month, scheduled: a no-op, not an error.
        self.assertEqual(rn.main(["cut-stable", "--date", "2026-10-02", "--if-scheduled", path]), 0)
        # By hand, the same refusal is an error.
        self.assertEqual(rn.main(["cut-stable", "--date", "2026-10-02", path]), 1)
        self.assertEqual(rn.main(["cut-stable", "--date", "2026-10-05", "--fix", path]), 0)
        self.assertEqual(rn.current_stable(rn.read(path)), "2026.10.1")


if __name__ == "__main__":
    unittest.main()
