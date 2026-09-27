## What and why

<!-- What changes, for whom, and why. Link the issue if there is one. -->

## Release notes

<!-- Every pull request does one of these (CONTRIBUTING.md, "Release notes"). -->

- [ ] A bullet under `## Unreleased` in `RELEASE_NOTES.md`, in its group
  (*New features*, *Maintenance updates* or *Bug fixes*), written for a
  workspace member.
- [ ] Nothing a user can see changes: the pull request carries the
  `no-release-notes` label.

A golden on the Refactor guard's list (`scripts/refactor/refactor_guard.py`)
changed on purpose? It needs the bullet, or the maintainer's
`behavior-change` label for a change nobody sees.

## Refactor pull requests

<!--
Delete this section unless the pull request carries `refactor` or a
`refactor:*` label (CONTRIBUTING.md, "Refactor PRs";
docs/plans/codebase-refactor.md §8). Title: refactor(<area>): <step-id> <summary>.
Labels: `refactor`, `no-release-notes`, and one per class below (A
`refactor:move`, C `refactor:test`, T `refactor:tooling`, R
`refactor:script`; B, D and E add none). Never `behavior-change`.
-->

Step: <!-- the plan step or sub-ID, e.g. M6 -->

| Commit | Class (`Refactor-Class:`) | Guarding step (R1) | Proof |
|---|---|---|---|
| <!-- sha subject --> | <!-- A B C D E R T --> | <!-- the merged guard that catches a change, e.g. S2, S5a --> | <!-- declhash, tsmovecheck, goldens unchanged, script re-run, characterization --> |

<details><summary><code>movecheck</code> / <code>declhash</code> / <code>tsmovecheck</code> output</summary>

```text
paste the function-to-file map (movecheck -base) and the proof's last line
for each class A commit
```

</details>

- [ ] Each commit declares one class; class E is alone in its pull request,
  names its `Refactor-Characterization:` tests and has two reviewers, one of
  them the maintainer.
- [ ] A class R commit names its `Refactor-Script:`, committed in an earlier
  commit, and holds only what the script writes.
- [ ] `make check LABELS="…"` passes with this pull request's labels; no
  golden, protected path or ratchet was loosened.
- [ ] OpenV (plan §8.5): for a guard step, the test case, its `verifies`
  links and a test run are recorded, and `get_vv_gaps` shows no new orphan;
  or the step maps to no requirement.
- [ ] Nothing here asks for *Promote to release*.
