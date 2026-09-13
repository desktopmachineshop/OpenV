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
  version [FILE]               print the current (newest nightly) version
  stable-version [FILE]        print the newest stable version ("" when none)
  cut-stable [--date D] [--fix] [--if-scheduled] [FILE]
                               cut a stable release (YYYY.MM) from the newest
                               nightly that is at least 7 days old, merging
                               the notes of every nightly since the previous
                               stable, grouped as changes and fixes; --fix
                               cuts YYYY.MM.P from the newest nightly with no
                               soak; --if-scheduled exits 0 without cutting
                               on a weekend or when the month already has a
                               stable (for the scheduled workflow)

Bullets that start with "fix:" are fixes, delivered to every channel at the
next nightly; every other bullet is a change that stable-channel workspaces
wait for. A stable section starts with "Cut on YYYY-MM-DD from <nightly>."
"""

import argparse
import datetime
import re
import sys

UNRELEASED = "Unreleased"
HEADING = re.compile(r"^##\s+(.+?)\s*$")
VERSION = re.compile(r"^(\d{4}-\d{2}-\d{2})(?:\.(\d+))?$")
STABLE = re.compile(r"^(\d{4})\.(\d{2})(?:\.(\d+))?$")
CUT_LINE = re.compile(r"^Cut on (\d{4}-\d{2}-\d{2}) from (\d{4}-\d{2}-\d{2}(?:\.\d+)?)\.?\s*$")
BULLET = re.compile(r"^\s*[-*]\s+(.+?)\s*$")
FIX_PREFIX = "fix:"
SOAK_DAYS = 7


class NotesError(Exception):
    pass


def parse(text):
    """Return (unreleased_bullets, [(nightly_version, bullets)],
    sections, stables) where sections keeps every line so the cut commands
    can rewrite the file without disturbing prose, and stables is
    [(version, cut_on, cut_from, bullets)]. Nightlies and stables are in
    file order (newest first by convention)."""
    unreleased = []
    releases = []
    stables = []
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
            if heading != UNRELEASED and not VERSION.match(heading) and not STABLE.match(heading):
                raise NotesError(
                    f"heading {heading!r} is neither Unreleased, a nightly date (YYYY-MM-DD or YYYY-MM-DD.N) "
                    "nor a stable version (YYYY.MM or YYYY.MM.P)"
                )
            seen.add(heading)
            current = (heading, [])
            continue
        current[1].append(line)
    sections.append(current)
    if not seen:
        raise NotesError("no Unreleased or release section found")
    for heading, lines in sections:
        if heading is None:
            continue
        if heading == UNRELEASED:
            unreleased = bullets_of(lines)
        elif VERSION.match(heading):
            releases.append((heading, bullets_of(lines)))
        else:
            cut_on = cut_from = ""
            for line in lines:
                c = CUT_LINE.match(line.strip())
                if c:
                    cut_on, cut_from = c.group(1), c.group(2)
                    break
            if not cut_from:
                raise NotesError(f"stable {heading} has no 'Cut on YYYY-MM-DD from <nightly>.' line")
            stables.append((heading, cut_on, cut_from, bullets_of(lines)))
    return unreleased, releases, sections, stables


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
    unreleased, releases, sections, _ = parse(text)
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


def is_fix(bullet):
    return bullet[: len(FIX_PREFIX)].lower() == FIX_PREFIX


def fix_text(bullet):
    return bullet[len(FIX_PREFIX):].strip() if is_fix(bullet) else bullet


def newest_nightly(releases):
    """The newest nightly by version, whatever the file order."""
    return max((v for v, _ in releases), key=version_key, default="")


def stable_key(version):
    m = STABLE.match(version)
    return (int(m.group(1)), int(m.group(2)), int(m.group(3) or 0))


def current_version(text):
    _, releases, _, _ = parse(text)
    return newest_nightly(releases)


def current_stable(text):
    _, _, _, stables = parse(text)
    return max((s[0] for s in stables), key=stable_key, default="")


def cut_stable(text, date, fix=False):
    """Cut a stable release on date. Returns (version, new_text)."""
    unreleased, releases, sections, stables = parse(text)
    if not releases:
        raise NotesError("no nightly release to cut a stable from")
    prev = max(stables, key=lambda s: stable_key(s[0]), default=None)
    prev_cut_from = prev[2] if prev else ""
    year, month = date.split("-")[0], date.split("-")[1]
    same_month = [s for s in stables if stable_key(s[0])[:2] == (int(year), int(month))]
    if fix:
        if prev is None:
            raise NotesError("no stable release to cut a fix release of")
        base = stable_key(prev[0])
        version = f"{base[0]:04d}.{base[1]:02d}.{base[2] + 1}"
        candidate = newest_nightly(releases)
    else:
        if same_month:
            raise NotesError(f"a stable release for {year}.{month} already exists ({same_month[0][0]})")
        version = f"{year}.{month}"
        cutoff = datetime.date.fromisoformat(date) - datetime.timedelta(days=SOAK_DAYS)
        eligible = [v for v, _ in releases if datetime.date.fromisoformat(v.split(".")[0]) <= cutoff]
        if not eligible:
            raise NotesError(f"no nightly is {SOAK_DAYS} days old yet; nothing has soaked long enough to be stable")
        candidate = max(eligible, key=version_key)
    if prev_cut_from and version_key(candidate) <= version_key(prev_cut_from):
        raise NotesError(f"nothing to release: {prev[0]} was already cut from {prev_cut_from}")
    covered = [
        (v, b) for v, b in releases
        if version_key(v) <= version_key(candidate) and (not prev_cut_from or version_key(v) > version_key(prev_cut_from))
    ]
    covered.sort(key=lambda r: version_key(r[0]), reverse=True)
    changes = [fix_text(b) for _, bullets in covered for b in bullets if not is_fix(b)]
    fixes = [fix_text(b) for _, bullets in covered for b in bullets if is_fix(b)]
    body = [f"Cut on {date} from {candidate}.", ""]
    if changes:
        body += ["### Changes", ""] + [f"- {c}" for c in changes] + [""]
    if fixes:
        body += ["### Fixes", ""] + [f"- fix: {f}" for f in fixes] + [""]
    if not changes and not fixes:
        body += ["No customer-facing changes since the previous stable release.", ""]
    out = []
    for heading, lines in sections:
        if heading is None:
            out.extend(lines)
            continue
        if heading == UNRELEASED:
            out.append(f"## {UNRELEASED}")
            out.extend(lines)
            if out and out[-1].strip():
                out.append("")
            out.append(f"## {version}")
            out.append("")
            out.extend(body)
            continue
        out.append(f"## {heading}")
        out.extend(lines)
    result = "\n".join(out)
    if not result.endswith("\n"):
        result += "\n"
    result = re.sub(r"\n{3,}", "\n\n", result)
    return version, result


def cmd_check(args):
    parse(read(args.file))
    print("release notes OK")


def cmd_check_pr(args):
    base_unreleased = []
    if args.base:
        base_text = read(args.base)
        if base_text.strip():
            base_unreleased, _, _, _ = parse(base_text)
    head_unreleased, _, _, _ = parse(read(args.file))
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


def cmd_stable_version(args):
    print(current_stable(read(args.file)))


def cmd_cut_stable(args):
    date = args.date or datetime.date.today().isoformat()
    if not re.match(r"^\d{4}-\d{2}-\d{2}$", date):
        raise NotesError(f"--date must be YYYY-MM-DD, got {date!r}")
    if args.if_scheduled and not args.fix:
        day = datetime.date.fromisoformat(date)
        if day.weekday() >= 5:
            print(f"{date} is a weekend; not cutting a stable release")
            return
        _, _, _, stables = parse(read(args.file))
        if any(stable_key(v)[:2] == (day.year, day.month) for v, _, _, _ in stables):
            print(f"{day.year:04d}.{day.month:02d} already exists; nothing to do")
            return
    version, result = cut_stable(read(args.file), date, fix=args.fix)
    with open(args.file, "w", encoding="utf-8") as f:
        f.write(result)
    print(version)


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
    add("stable-version", cmd_stable_version)
    add(
        "cut-stable",
        cmd_cut_stable,
        **{
            "--date": {"default": ""},
            "--fix": {"action": "store_true"},
            "--if-scheduled": {"action": "store_true", "dest": "if_scheduled"},
        },
    )
    args = p.parse_args(argv)
    try:
        args.fn(args)
    except NotesError as e:
        print(f"release notes: {e}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
