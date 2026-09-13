"""Tests for scripts/release_notes.py: python3 -m unittest scripts/release_notes_test.py"""

import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import release_notes as rn  # noqa: E402

# The file as it is written today: grouped bullets waiting for a release, a
# numbered release, and one of the dated releases from before OpenV had
# version numbers.
SAMPLE = """# Notes

Preamble.

## Unreleased

### New features

- Pending one
  wrapped

### Bug fixes

- Pending two

## 0.1.0 — 2026-09-13

### Maintenance updates

- Tidied

## 2026-09-12

- Shipped
"""

# A file from before groups and version numbers existed: what the base branch
# of a pull request opened against an older master still looks like.
LEGACY = """# Notes

## Unreleased

- Pending

## 2026-09-12

- Shipped
"""


def without_unreleased(text):
    """text with nothing pending, for the cases that add their own."""
    _, _, rest = text.partition("## 0.1.0")
    return "## Unreleased\n\n## 0.1.0" + rest


def pending(group, bullet, text=None):
    """text with one bullet waiting under group."""
    base = without_unreleased(SAMPLE) if text is None else text
    return base.replace("## Unreleased\n", f"## Unreleased\n\n### {group}\n\n- {bullet}\n", 1)


class ParseTests(unittest.TestCase):
    def test_bullets_carry_the_group_they_sit_under(self):
        unreleased, releases, _ = rn.parse(SAMPLE)
        self.assertEqual(
            unreleased,
            [(rn.FEATURES, "Pending one wrapped"), (rn.FIXES, "Pending two")],
        )
        self.assertEqual(releases[0], ("0.1.0", "2026-09-13", [(rn.MAINTENANCE, "Tidied")]))

    # The dated releases shipped before groups existed. They are read as they
    # were written rather than refused, because renaming them would announce
    # them to every account a second time.
    def test_a_legacy_release_is_read_ungrouped(self):
        _, releases, _ = rn.parse(SAMPLE)
        self.assertEqual(releases[1], ("2026-09-12", "2026-09-12", [(rn.UNGROUPED, "Shipped")]))

    def test_rejects_bad_heading_and_duplicates(self):
        for text in ("## Unreleased\n## v1\n", "## 0.1.0\n## 0.1.0\n", "# title only\n"):
            with self.assertRaises(rn.NotesError):
                rn.parse(text)

    # A bullet under no group would not be shown under any heading in the app,
    # so it is refused where it is cheap to fix: in the pull request.
    def test_rejects_an_ungrouped_bullet_and_an_invented_group(self):
        with self.assertRaises(rn.NotesError):
            rn.parse("## Unreleased\n\n- Loose\n")
        with self.assertRaises(rn.NotesError):
            rn.parse(pending("Improvements", "x"))

    # The base branch of a pull request may predate every rule this file
    # introduces; a new rule must not fail a check by complaining about
    # somebody else's file.
    def test_an_older_file_still_parses_leniently(self):
        with self.assertRaises(rn.NotesError):
            rn.parse(LEGACY)
        unreleased, releases, _ = rn.parse(LEGACY, strict=False)
        self.assertEqual(unreleased, [(rn.UNGROUPED, "Pending")])
        self.assertEqual(releases[0][0], "2026-09-12")


class VersionTests(unittest.TestCase):
    # Every numbered release is newer than every dated one, because that is
    # the order they shipped in and the promotion gate compares the two.
    def test_a_numbered_release_outranks_every_dated_one(self):
        self.assertGreater(rn.version_key("0.1.0"), rn.version_key("2026-09-12.3"))
        self.assertGreater(rn.version_key("1.0.0"), rn.version_key("0.9.9"))
        self.assertGreater(rn.version_key("0.10.0"), rn.version_key("0.9.0"))

    def test_the_bullets_decide_the_bump(self):
        self.assertEqual(rn.bump_for([(rn.FEATURES, "x"), (rn.FIXES, "y")]), "minor")
        self.assertEqual(rn.bump_for([(rn.MAINTENANCE, "x"), (rn.FIXES, "y")]), "patch")
        self.assertEqual(rn.bump_for([(rn.FIXES, "y")], major=True), "major")

    def test_the_first_numbered_release_is_0_1_0(self):
        self.assertEqual(rn.next_version(["2026-09-12"], [(rn.FEATURES, "x")]), "0.1.0")
        # Unless a major one is asked for outright, which is a declaration.
        self.assertEqual(rn.next_version(["2026-09-12"], [(rn.FIXES, "x")], major=True), "1.0.0")


class CutTests(unittest.TestCase):
    def test_cut_moves_unreleased_into_a_numbered_section(self):
        version, result = rn.cut(SAMPLE, "2026-09-14")
        self.assertEqual(version, "0.2.0")  # SAMPLE has a New features bullet
        self.assertIn("## 0.2.0 — 2026-09-14", result)
        unreleased, releases, _ = rn.parse(result)
        self.assertEqual(unreleased, [])
        self.assertEqual(
            releases[0],
            ("0.2.0", "2026-09-14", [(rn.FEATURES, "Pending one wrapped"), (rn.FIXES, "Pending two")]),
        )
        self.assertEqual(releases[1][0], "0.1.0")
        self.assertIn("Preamble.", result)
        self.assertNotIn("\n\n\n", result)

    def test_successive_cuts_walk_the_version(self):
        fix, out = rn.cut(pending(rn.FIXES, "Nothing crashes."), "2026-09-14")
        self.assertEqual(fix, "0.1.1")
        feature, out = rn.cut(pending(rn.FEATURES, "A new thing.", out), "2026-09-15")
        self.assertEqual(feature, "0.2.0")
        major, out = rn.cut(pending(rn.MAINTENANCE, "Tidier.", out), "2026-09-16", major=True)
        self.assertEqual(major, "1.0.0")
        after, _ = rn.cut(pending(rn.MAINTENANCE, "Tidier still.", out), "2026-09-17")
        self.assertEqual(after, "1.0.1")

    def test_cut_refuses_empty_unreleased(self):
        _, cut_once = rn.cut(SAMPLE, "2026-09-14")
        with self.assertRaises(rn.NotesError):
            rn.cut(cut_once, "2026-09-15")


class StableTests(unittest.TestCase):
    # Two numbered releases a week apart, the older one already stable.
    TWO = SAMPLE.replace(
        "## 0.1.0 — 2026-09-13\n",
        "## 0.2.0 — 2026-09-20\n\n### New features\n\n- Newer\n\n## 0.1.0 — 2026-09-13\n\nStable channel release since 2026-09-20.\n",
        1,
    )

    def test_the_marker_designates_a_release_and_is_not_a_note(self):
        self.assertEqual(rn.stables(self.TWO), [("0.1.0", "2026-09-20")])
        self.assertEqual(rn.current_stable(self.TWO), "0.1.0")
        self.assertEqual(rn.current_stable(SAMPLE), "")
        _, releases, _ = rn.parse(self.TWO)
        self.assertEqual(releases[1][2], [(rn.MAINTENANCE, "Tidied")])

    def test_cut_stable_takes_the_newest_release_that_has_soaked(self):
        # 0.2.0 shipped on the 20th: on the 26th it has not soaked, on the 27th it has.
        with self.assertRaises(rn.NotesError):
            rn.cut_stable(self.TWO, "2026-09-26")
        version, result = rn.cut_stable(self.TWO, "2026-09-27")
        self.assertEqual(version, "0.2.0")
        self.assertEqual(rn.stables(result), [("0.2.0", "2026-09-27"), ("0.1.0", "2026-09-20")])
        self.assertIn("## 0.2.0 — 2026-09-20\n\nStable channel release since 2026-09-27.\n\n### New features", result)
        self.assertNotIn("\n\n\n", result)
        # The rest of the file is untouched: the same releases, the same notes.
        self.assertEqual(rn.parse(result)[1], rn.parse(self.TWO)[1])

    def test_a_fix_takes_the_newest_release_at_once(self):
        version, _ = rn.cut_stable(self.TWO, "2026-09-21", fix=True)
        self.assertEqual(version, "0.2.0")

    def test_cut_stable_refuses_when_nothing_newer_has_soaked(self):
        _, once = rn.cut_stable(self.TWO, "2026-09-27")
        with self.assertRaises(rn.NotesError):
            rn.cut_stable(once, "2026-10-05")
        with self.assertRaises(rn.NotesError):
            rn.cut_stable(LEGACY.replace("- Pending", "### Bug fixes\n\n- Pending"), "2026-10-05")

    def test_cut_stable_command_honours_the_schedule(self):
        fd, path = tempfile.mkstemp(suffix=".md")
        with os.fdopen(fd, "w") as f:
            f.write(self.TWO)
        self.addCleanup(os.remove, path)
        # Sunday 2026-09-27: a scheduled run does nothing and exits 0.
        self.assertEqual(rn.main(["cut-stable", "--date", "2026-09-27", "--if-scheduled", path]), 0)
        self.assertEqual(rn.current_stable(rn.read(path)), "0.1.0")
        # Monday 2026-09-28, but September already has a stable release.
        self.assertEqual(rn.main(["cut-stable", "--date", "2026-09-28", "--if-scheduled", path]), 0)
        self.assertEqual(rn.current_stable(rn.read(path)), "0.1.0")
        # Thursday 2026-10-01: October has none, so 0.2.0 is designated.
        self.assertEqual(rn.main(["cut-stable", "--date", "2026-10-01", "--if-scheduled", path]), 0)
        self.assertEqual(rn.current_stable(rn.read(path)), "0.2.0")
        self.assertEqual(rn.main(["stable-version", path]), 0)
        self.assertEqual(rn.main(["check", path]), 0)
        # By hand, with nothing newer, the cut is refused.
        self.assertEqual(rn.main(["cut-stable", "--date", "2026-10-02", path]), 1)


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
        added = self.write(pending(rn.FIXES, "New in this PR", SAMPLE))
        self.assertEqual(rn.main(["check-pr", "--base", base, added]), 0)
        # A base branch with no notes file yet: any bullet counts.
        empty = self.write("")
        self.assertEqual(rn.main(["check-pr", "--base", empty, same]), 0)
        # A base branch written before groups existed is read leniently, so
        # the bullets this branch adds are still what is compared.
        self.assertEqual(rn.main(["check-pr", "--base", self.write(LEGACY), same]), 0)

    def test_check_pr_refuses_an_ungrouped_bullet(self):
        base = self.write(SAMPLE)
        loose = self.write(SAMPLE.replace("## Unreleased\n", "## Unreleased\n\n- Loose\n", 1))
        self.assertEqual(rn.main(["check-pr", "--base", base, loose]), 1)

    def test_check_release_compares_with_deployed(self):
        head = self.write(SAMPLE)
        deployed = self.write(SAMPLE)
        self.assertEqual(rn.main(["check-release", "--released", deployed, head]), 1)
        _, newer = rn.cut(SAMPLE, "2026-09-14")
        head2 = self.write(newer)
        self.assertEqual(rn.main(["check-release", "--released", deployed, head2]), 0)
        self.assertEqual(rn.main(["check-release", "--released", self.write(""), head]), 0)
        # What is deployed may still be a file written before groups existed.
        self.assertEqual(rn.main(["check-release", "--released", self.write(LEGACY), head]), 0)

    def test_cut_version_and_next_commands(self):
        path = self.write(SAMPLE)
        self.assertEqual(rn.main(["next", path]), 0)
        self.assertEqual(rn.main(["cut", "--date", "2026-09-14", path]), 0)
        self.assertEqual(rn.current_version(rn.read(path)), "0.2.0")
        self.assertEqual(rn.main(["version", path]), 0)
        self.assertEqual(rn.main(["check", path]), 0)


if __name__ == "__main__":
    unittest.main()
