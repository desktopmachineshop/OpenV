#!/usr/bin/env python3
"""Customer-facing release notes: the rules CI and the release pipeline apply.

RELEASE_NOTES.md at the repository root has one ``## Unreleased`` section and
any number of dated ``## YYYY-MM-DD`` (or ``YYYY-MM-DD.N``) sections, newest
first. Stdlib only, so it runs on any runner or machine with Python 3.

Subcommands:

  check [FILE]                 the file parses (headings are Unreleased or a
                               release date, no duplicates)
  check-pr --base BASE [FILE]  the pull request added at least one bullet
                               under Unreleased compared with BASE, the file
                               as it is on the base branch (empty when the
                               base has no notes file yet)
  cut [--date D] [FILE]        move the Unreleased bullets into a new dated
                               section at the top (the promotion date, with
                               .2, .3 … when that date already exists) and
                               rewrite FILE; prints the new version. Fails
                               when Unreleased is empty.
  check-release --released REL [FILE]
                               the file names a release newer than REL, the
                               file as deployed (release branch); used by the
                               promotion after a cut, or instead of one when
                               the notes were cut by hand
  version [FILE]               print the current (top) version
"""

import argparse
import datetime
import re
import sys

UNRELEASED = "Unreleased"
HEADING = re.compile(r"^##\s+(.+?)\s*$")
VERSION = re.compile(r"^(\d{4}-\d{2}-\d{2})(?:\.(\d+))?$")
BULLET = re.compile(r"^\s*[-*]\s+(.+?)\s*$")


class NotesError(Exception):
    pass


def parse(text):
    """Return (unreleased_bullets, [(version, bullets)], preamble_lines,
    sections) where sections keeps every line so cut() can rewrite the file
    without disturbing prose."""
    unreleased = []
    releases = []
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
            if heading != UNRELEASED and not VERSION.match(heading):
                raise NotesError(
                    f"heading {heading!r} is neither Unreleased nor a release date (YYYY-MM-DD or YYYY-MM-DD.N)"
                )
            seen.add(heading)
            current = (heading, [])
            continue
        current[1].append(line)
    sections.append(current)
    if UNRELEASED not in seen and not any(VERSION.match(h) for h in seen):
        raise NotesError("no Unreleased or release section found")
    for heading, lines in sections:
        bullets = bullets_of(lines)
        if heading == UNRELEASED:
            unreleased = bullets
        elif heading is not None:
            releases.append((heading, bullets))
    return unreleased, releases, sections


def bullets_of(lines):
    """Bullet texts, with an indented line joined onto the bullet above it."""
    bullets = []
    for line in lines:
        m = BULLET.match(line)
        if m:
            bullets.append(m.group(1))
        elif bullets and line.strip() and line[:1] in (" ", "\t"):
            bullets[-1] += " " + line.strip()
    return bullets


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def version_key(version):
    date, _, n = version.partition(".")
    return (date, int(n) if n else 1)


def next_version(existing, date):
    """The version for a release cut on date: the date itself, or date.N."""
    taken = {version_key(v) for v in existing}
    n = 1
    while (date, n) in taken:
        n += 1
    return date if n == 1 else f"{date}.{n}"


def cut(text, date):
    unreleased, releases, sections = parse(text)
    if not unreleased:
        raise NotesError("Unreleased is empty: every release must say what changed")
    version = next_version([v for v, _ in releases], date)
    out = []
    for heading, lines in sections:
        if heading is None:
            out.extend(lines)
            continue
        if heading == UNRELEASED:
            out.append(f"## {UNRELEASED}")
            out.append("")
            out.append(f"## {version}")
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


def current_version(text):
    _, releases, _ = parse(text)
    return releases[0][0] if releases else ""


def cmd_check(args):
    parse(read(args.file))
    print("release notes OK")


def cmd_check_pr(args):
    base_unreleased = []
    if args.base:
        base_text = read(args.base)
        if base_text.strip():
            base_unreleased, _, _ = parse(base_text)
    head_unreleased, _, _ = parse(read(args.file))
    added = [b for b in head_unreleased if b not in base_unreleased]
    if not added:
        raise NotesError(
            "this pull request adds no release note. Add a bullet under '## Unreleased' in "
            "RELEASE_NOTES.md describing the change for the people who use OpenV, or label "
            "the pull request 'no-release-notes' if nothing they can see changed."
        )
    print("release note added:")
    for b in added:
        print(f"  - {b}")


def cmd_cut(args):
    date = args.date or datetime.date.today().isoformat()
    if not re.match(r"^\d{4}-\d{2}-\d{2}$", date):
        raise NotesError(f"--date must be YYYY-MM-DD, got {date!r}")
    version, result = cut(read(args.file), date)
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
            released = current_version(text)
    if released and version_key(head) <= version_key(released):
        raise NotesError(
            f"nothing to release: the notes still name {head}, which is what is deployed. "
            "Add a bullet under Unreleased and cut a release."
        )
    print(head)


def cmd_version(args):
    print(current_version(read(args.file)))


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="command", required=True)

    def add(name, fn, **kw):
        sp = sub.add_parser(name)
        sp.add_argument("file", nargs="?", default="RELEASE_NOTES.md")
        for flag, opts in kw.items():
            sp.add_argument(flag, **opts)
        sp.set_defaults(fn=fn)

    add("check", cmd_check)
    add("check-pr", cmd_check_pr, **{"--base": {"default": ""}})
    add("cut", cmd_cut, **{"--date": {"default": ""}})
    add("check-release", cmd_check_release, **{"--released": {"default": ""}})
    add("version", cmd_version)
    args = p.parse_args(argv)
    try:
        args.fn(args)
    except NotesError as e:
        print(f"release notes: {e}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
