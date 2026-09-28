# Vendored: Leonxlnx/taste-skill

- Source: https://github.com/Leonxlnx/taste-skill
- Pinned commit: `ce26fc25c0e5e8cab638f883de62d9a86ee5e45b` (2026-09-26; upstream has no tags or releases)
- Contents: the full tree at that commit, exported with `git archive`, unmodified.
- `SKILL.md` at this folder's root is a copy of `skills/taste-skill/SKILL.md`, so Claude Code discovers the
  main skill (`design-taste-frontend`). Every other skill under `skills/` is exposed by a symlink in
  `.claude/skills/` named after its folder (e.g. `.claude/skills/brandkit -> taste-skill/skills/brandkit/`).

## Updating

    git clone https://github.com/Leonxlnx/taste-skill /tmp/ts
    rm -rf .claude/skills/taste-skill/* && git -C /tmp/ts archive <commit> | tar -x -C .claude/skills/taste-skill
    cp .claude/skills/taste-skill/skills/taste-skill/SKILL.md .claude/skills/taste-skill/SKILL.md

Re-run a security scan of the new tree before committing, then update the commit above.

## Security scan (at the pinned commit)

No prompt injection, hidden Unicode, base64 payloads, credential or env access, or settings/hook/permission
changes were found. No instructions fetch or post to remote URLs at runtime. `scripts/*.mjs` use only `fs`,
`path` and `sharp` (local image processing). Notes:
- `skills/redesign-skill` tells the agent to invent realistic-looking data ("randomize dates to appear real").
  Treat that as mockup-only.
- `skills/taste-skill` suggests unpinned `npm install` / `npx shadcn@latest` for design-system packages.
- `skills/output-skill` has an over-broad trigger description ("any task").
