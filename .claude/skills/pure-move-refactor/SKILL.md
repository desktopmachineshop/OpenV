---
name: pure-move-refactor
description: Use when building a step of the OpenV refactor plan (docs/plans/codebase-refactor.md), or any pull request labelled refactor, which moves or extracts code and must change nothing a person or a program can observe.
---

# Refactor step

Read these first; they are the rules, and this skill only points at them.

- The plan, [docs/plans/codebase-refactor.md](../../../docs/plans/codebase-refactor.md):
  the step's row in [§6](../../../docs/plans/codebase-refactor.md#6-roadmap),
  [§3, the invariants](../../../docs/plans/codebase-refactor.md#3-invariants-what-must-not-change-and-what-pins-it),
  [§4.1, the rules](../../../docs/plans/codebase-refactor.md#41-rules-every-refactor-pr-follows),
  [§4.2, the verification classes](../../../docs/plans/codebase-refactor.md#42-verification-classes)
  and [§8, the process](../../../docs/plans/codebase-refactor.md#8-process-for-each-refactor-pr).
- [CONTRIBUTING.md, "Refactor PRs"](../../../CONTRIBUTING.md#refactor-prs):
  labels, the `Refactor-Class:` trailer, and what the Refactor guard checks.
- [internal/tools/README.md](../../../internal/tools/README.md): the tool
  that proves or generates each class (`declhash`, `movecheck`,
  `tsdeclhash`, `tsmovecheck`, the window generators).
- The Invariants and Guards sections of the README of each area you touch.

Before you push: "Before you finish" in the root [CLAUDE.md](../../../CLAUDE.md),
with the pull request's labels. The scaffolds write behaviour changes, so
a refactor never runs them.
