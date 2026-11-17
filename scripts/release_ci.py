#!/usr/bin/env python3
"""Which master commit the release workflows judge by its CI.

The release pipeline commits to master itself: Promote to release and Cut
stable release cut the notes ("Release 0.27.2"), mark the stable release
("Stable release 0.27.2") and stamp the manual's release line ("Manual
documents 0.27"), as github-actions[bot], with the workflow's own token. A
push made with that token starts no workflows, so those commits never carry
CI of their own, and a gate that judged master's head refused every release
right after one (the stable cut that found "No check runs" on a release
commit, and Promote after a stable cut).

Those commits change only the notes, the changelog the site renders from
them and the manual's release line, none of which CI builds differently. So
the gate judges the newest commit at or below HEAD that is not one of them:
the code that is actually being released.

    python3 scripts/release_ci.py judged-commit        # prints a sha
"""
import argparse
import subprocess
import sys

BOT = "github-actions[bot]"
# What the pipeline's own commits may touch (promote-release.yml and
# cut-stable.yml: the notes, `release_notes.py site`, `docs.py stamp`).
PIPELINE_FILES = {
    "RELEASE_NOTES.md",
    "frontend/src/site/changelog/releases.json",
    "manual/opend.config.yaml",
}


def git(*args, cwd=None):
    return subprocess.run(["git", *args], cwd=cwd, check=True, capture_output=True, text=True).stdout


def is_pipeline_commit(sha, cwd=None):
    """A commit the release workflows made: by the bot, touching only the
    notes, the changelog and the manual's release line, with one parent."""
    author, parents = git("show", "-s", "--format=%an%x00%P", sha, cwd=cwd).strip().split("\x00")
    if author != BOT or len(parents.split()) != 1:
        return False
    files = {f for f in git("show", "--format=", "--name-only", sha, cwd=cwd).splitlines() if f}
    return bool(files) and files <= PIPELINE_FILES


def judged_commit(ref="HEAD", cwd=None):
    sha = git("rev-parse", ref, cwd=cwd).strip()
    while is_pipeline_commit(sha, cwd=cwd):
        sha = git("rev-parse", f"{sha}^", cwd=cwd).strip()
    return sha


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="cmd", required=True)
    j = sub.add_parser("judged-commit", help="the commit whose CI gates a release")
    j.add_argument("ref", nargs="?", default="HEAD")
    args = p.parse_args(argv)
    if args.cmd == "judged-commit":
        print(judged_commit(args.ref))
    return 0


if __name__ == "__main__":
    sys.exit(main())
