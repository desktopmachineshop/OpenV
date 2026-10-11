---
name: add-page
description: Use when adding a page, a routed view, to the OpenV frontend.
---

# Add a page

The recipe is in the area guide; follow it there.

- [frontend/src/README.md, Recipes](../../../frontend/src/README.md#recipes),
  "Add a page".
- The calls the page makes: the `add-endpoint` skill.

Start from the scaffold, then do what it prints:
`node frontend/scripts/scaffold.mjs page <name>` writes a stub view, adds its
lazy `<Route>` to `App.tsx`, and records the page in `frontend/src/pages.ts`,
the page registry. `-n` previews. The registry, not `ProjectLayout.tsx`, holds
the project menu: a page joins it by a `nav` on its `pages.ts` entry.
`src/arch/pagesParity.test.ts` holds the registry to the routes, the
eager/lazy split, the public paths and the help topics.

Before you finish: [frontend/src/CLAUDE.md](../../../frontend/src/CLAUDE.md),
then "Before you finish" in the root [CLAUDE.md](../../../CLAUDE.md).
