#!/usr/bin/env python3
"""Customer-facing release notes: the rules CI and the release pipeline apply.

RELEASE_NOTES.md at the repository root has one ``## Unreleased`` section and
any number of release sections, newest first. A release section is headed by
a semantic version and the day it shipped -- ``## 0.2.0 - 2026-09-13`` -- and
groups its bullets under ``### New features``, ``### Maintenance updates``
and ``### Bug fixes``, which is the shape the app serves and announces.

Releases from before OpenV had version numbers are headed by their date
instead and have no groups. They are read, never written: they were
announced under those names, and renaming them would announce them again.

The next version is derived from the notes themselves. Anything under New
features is a minor bump; a release of only maintenance and fixes is a patch;
a major bump is deliberate (``cut --major``). The first release is 0.1.0.

Stdlib only, so it runs on any runner or machine with Python 3.

Subcommands:

  check [FILE]                 the file parses: every heading is Unreleased,
                               a version or a known group, nothing repeats,
                               and no bullet sits outside a group
  check-pr --base BASE [FILE]  the pull request added at least one bullet
                               under Unreleased compared with BASE, the file
                               as it is on the base branch (empty when the
                               base has no notes file yet)
  cut [--date D] [--major] [FILE]
                               move the Unreleased bullets into a new release
                               section at the top and rewrite FILE; prints the
                               new version. Fails when Unreleased is empty.
  check-release --released REL [FILE]
                               the file names a release newer than REL, the
                               file as deployed (release branch); used by the
                               promotion after a cut, or instead of one when
                               the notes were cut by hand
  version [FILE]               print the current (top) version
  next [--major] [FILE]        print the version the next cut would produce
"""

import argparse
import datetime
import re
import sys

UNRELEASED = "Unreleased"
HEADING = re.compile(r"^##\s+(.+?)\s*$")
GROUP_HEADING = re.compile(r"^###\s+(.+?)\s*$")
# A release heading: a semantic version, optionally followed by the day it
# shipped. Both an em dash and a hyphen are accepted, because one of them is
# what a person types.
VERSION = re.compile(r"^(\d+\.\d+\.\d+)(?:\s+[—-]\s+(\d{4}-\d{2}-\d{2}))?$")
LEGACY_VERSION = re.compile(r"^(\d{4}-\d{2}-\d{2})(?:\.(\d+))?$")
BULLET = re.compile(r"^\s*[-*]\s+(.+?)\s*$")

FEATURES = "New features"
MAINTENANCE = "Maintenance updates"
FIXES = "Bug fixes"
GROUPS = (FEATURES, MAINTENANCE, FIXES)

# Where a bullet written before any group heading is filed. Legacy sections
# are entirely ungrouped; a new bullet may not be, and check() says so.
UNGROUPED = "Changes"

FIRST_VERSION = "0.1.0"


class NotesError(Exception):
    pass


def parse(text, strict=True):
    """Return (unreleased, releases, sections).

    ``unreleased`` and each release's notes are [(group, bullet)] in file
    order; ``releases`` is [(version, date, notes)], newest first; and
    ``sections`` keeps every line so cut() can rewrite the file without
    disturbing prose.

    strict=False skips the "every bullet is in a group" rule, which is how a
    base-branch file written before groups existed is still readable.
    """
    releases = []
    unreleased = []
    sections = []  # (heading or None for preamble, [lines])
    current = (None, [])
    seen = set()
    for line in text.split("\n"):
        m = HEADING.match(line)
        if m:
            sections.append(current)
            heading = m.group(1)
            if heading in seen:
                raise NotesError(f"heading {heading!r} appears twice")
            if heading != UNRELEASED and not VERSION.match(heading) and not LEGACY_VERSION.match(heading):
                raise NotesError(
                    f"heading {heading!r} is neither Unreleased nor a version "
                    "(0.2.0, optionally followed by the release date)"
                )
            seen.add(heading)
            current = (heading, [])
            continue
        current[1].append(line)
    sections.append(current)
    if UNRELEASED not in seen and not any(is_version(h) for h in seen):
        raise NotesError("no Unreleased or release section found")
    for heading, lines in sections:
        if heading is None:
            continue
        notes = notes_of(lines, heading)
        if heading == UNRELEASED:
            unreleased = notes
            if strict:
                refuse_ungrouped(notes, "Unreleased")
            continue
        m = VERSION.match(heading)
        if m:
            if strict:
                refuse_ungrouped(notes, m.group(1))
            releases.append((m.group(1), m.group(2) or "", notes))
        else:
            # Legacy: the heading is the date, and there are no groups.
            releases.append((heading, heading.partition(".")[0], notes))
    return unreleased, releases, sections


def is_version(heading):
    return bool(VERSION.match(heading) or LEGACY_VERSION.match(heading))


def refuse_ungrouped(notes, where):
    stray = [b for g, b in notes if g == UNGROUPED]
    if stray:
        raise NotesError(
            f"{where}: {stray[0]!r} sits under no group. Put every bullet under one of "
            + ", ".join(f"'### {g}'" for g in GROUPS)
        )


def notes_of(lines, heading):
    """[(group, bullet)] for a section, joining an indented line onto the
    bullet above it."""
    notes = []
    group = UNGROUPED
    for line in lines:
        m = GROUP_HEADING.match(line)
        if m:
            if m.group(1) not in GROUPS:
                raise NotesError(
                    f"{heading}: {m.group(1)!r} is not one of " + ", ".join(GROUPS)
                )
            group = m.group(1)
            continue
        m = BULLET.match(line)
        if m:
            notes.append((group, m.group(1)))
        elif notes and line.strip() and line[:1] in (" ", "\t"):
            notes[-1] = (notes[-1][0], notes[-1][1] + " " + line.strip())
    return notes


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def version_key(version):
    """A sort key that orders every version, old shape and new.

    Legacy date versions sort below every semantic version, because that is
    the order they shipped in and the promotion gate compares the two.
    """
    m = VERSION.match(version)
    if m:
        return (1,) + tuple(int(p) for p in m.group(1).split("."))
    date, _, n = version.partition(".")
    return (0, date, int(n) if n else 1)


def bump_for(notes, major=False):
    """The part of the version this set of notes moves: a new capability is a
    minor release, tidying and fixes are a patch, a major is always asked
    for."""
    if major:
        return "major"
    if any(group == FEATURES for group, _ in notes):
        return "minor"
    return "patch"


def next_version(existing, notes, major=False):
    """The version a cut of ``notes`` would produce, given the versions the
    file already carries."""
    semvers = [v for v in existing if VERSION.match(v)]
    if not semvers:
        # The first numbered release is 0.1.0 — OpenV's history up to here is
        # the dated sections — unless a major one is asked for outright, in
        # which case that is a declaration that this is 1.0.0.
        return "1.0.0" if major else FIRST_VERSION
    latest = max(semvers, key=version_key)
    parts = [int(p) for p in latest.split(".")]
    bump = bump_for(notes, major)
    if bump == "major":
        return f"{parts[0] + 1}.0.0"
    if bump == "minor":
        return f"{parts[0]}.{parts[1] + 1}.0"
    return f"{parts[0]}.{parts[1]}.{parts[2] + 1}"


def cut(text, date, major=False):
    unreleased, releases, sections = parse(text)
    if not unreleased:
        raise NotesError("Unreleased is empty: every release must say what changed")
    version = next_version([v for v, _, _ in releases], unreleased, major)
    out = []
    for heading, lines in sections:
        if heading is None:
            out.extend(lines)
            continue
        if heading == UNRELEASED:
            out.append(f"## {UNRELEASED}")
            out.append("")
            out.append(f"## {version} — {date}")
            body = "\n".join(lines).strip("\n")
            out.append("")
            out.extend(body.split("\n"))
            out.append("")
            continue
        out.append(f"## {heading}")
        out.extend(lines)
    result = "\n".join(out)
    if not result.endswith("\n"):
        result += "\n"
    # Collapse runs of blank lines the rewrite may have introduced.
    result = re.sub(r"\n{3,}", "\n\n", result)
    return version, result


def current_version(text, strict=True):
    _, releases, _ = parse(text, strict=strict)
    return releases[0][0] if releases else ""


def cmd_check(args):
    parse(read(args.file))
    print("release notes OK")


def cmd_check_pr(args):
    base_unreleased = []
    if args.base:
        base_text = read(args.base)
        if base_text.strip():
            # The base branch is read leniently: a rule this branch introduces
            # must not turn into a failure about somebody else's file.
            base_unreleased, _, _ = parse(base_text, strict=False)
    head_unreleased, _, _ = parse(read(args.file))
    added = [n for n in head_unreleased if n not in base_unreleased]
    if not added:
        raise NotesError(
            "this pull request adds no release note. Add a bullet under '## Unreleased' in "
            "RELEASE_NOTES.md describing the change for the people who use OpenV — under "
            + ", ".join(f"'### {g}'" for g in GROUPS)
            + " — or label the pull request 'no-release-notes' if nothing they can see changed."
        )
    print("release note added:")
    for group, bullet in added:
        print(f"  {group}: {bullet}")


def cmd_cut(args):
    date = args.date or datetime.date.today().isoformat()
    if not re.match(r"^\d{4}-\d{2}-\d{2}$", date):
        raise NotesError(f"--date must be YYYY-MM-DD, got {date!r}")
    version, result = cut(read(args.file), date, args.major)
    with open(args.file, "w", encoding="utf-8") as f:
        f.write(result)
    print(version)


def cmd_check_release(args):
    head = current_version(read(args.file))
    if not head:
        raise NotesError("the notes name no release yet; cut one first")
    released = ""
    if args.released:
        text = read(args.released)
        if text.strip():
            released = current_version(text, strict=False)
    if released and version_key(head) <= version_key(released):
        raise NotesError(
            f"nothing to release: the notes still name {head}, which is what is deployed. "
            "Add a bullet under Unreleased and cut a release."
        )
    print(head)


def cmd_version(args):
    print(current_version(read(args.file)))


def cmd_next(args):
    unreleased, releases, _ = parse(read(args.file))
    print(next_version([v for v, _, _ in releases], unreleased, args.major))


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="command", required=True)

    def add(name, fn, **kw):
        sp = sub.add_parser(name)
        sp.add_argument("file", nargs="?", default="RELEASE_NOTES.md")
        for flag, opts in kw.items():
            sp.add_argument(flag, **opts)
        sp.set_defaults(fn=fn)

    major = {"action": "store_true", "help": "cut a major release"}
    add("check", cmd_check)
    add("check-pr", cmd_check_pr, **{"--base": {"default": ""}})
    add("cut", cmd_cut, **{"--date": {"default": ""}, "--major": major})
    add("check-release", cmd_check_release, **{"--released": {"default": ""}})
    add("version", cmd_version)
    add("next", cmd_next, **{"--major": major})
    args = p.parse_args(argv)
    try:
        args.fn(args)
    except NotesError as e:
        print(f"release notes: {e}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
