---
applyTo: "frontend/src/**/*.ts,frontend/src/**/*.tsx"
---

# Frontend code: how to work

How to change the React app under `frontend/src`, as its type check, lint
and architecture tests (`frontend/src/arch`) hold every pull request to it.
What the platform must do is not here: it is the live OpenV project (see
`CLAUDE.md`). The area guides `frontend/src/README.md` and
`frontend/src/api/README.md`, each with a `CLAUDE.md`, have the map, the
recipes and the commands; `docs/architecture.md` has the overview. Commands
run from `frontend/` after `npm ci`.

## Language and components

- TypeScript only, under `strict` (`tsconfig.json`); `npx tsc --noEmit` and
  `npm run lint` are CI gates.
- Function components and hooks; the app has no class components.
  `react-hooks/rules-of-hooks` and `react-hooks/exhaustive-deps` are lint
  errors.
- State the whole app shares lives in the Zustand store `useAppStore`
  (`state/store.ts`) and changes only through its setters, which replace
  values rather than mutate them; state one component needs stays in its own
  hooks.
- **K14.** A new file has at most 600 lines and a component at most 300;
  the grandfathered ceilings only fall (`arch/sizeBudget.test.ts`). Split a
  large view into panes in a folder beside it, as `views/moduleView/` and
  `views/projectSettings/` do.
- Components never import views (`openv/import-boundaries`; the two
  allowlisted imports in `eslint.config.js` only shrink), and nothing
  imports from outside `frontend/`, since the image builds from that
  directory alone.

## The API layer (K12)

- Every server call goes through `src/api`: an endpoint is a method of its
  area's `xxxAPI` object in `api/<area>.ts`, with its types in
  `api/types/<area>.ts`. No component calls `fetch` or `axios` itself.
- Outside `src/api`, import only `api/client` (the barrel), `api/errors`,
  `api/baseURL` and `api/contentDisposition`, never an area module,
  `api/types/*` or `api/http` (`openv/import-boundaries`).
- Every call is a route the server registers (`arch/clientRoutes.test.ts`,
  against `internal/api/testdata/routes.txt`), and the barrel's exports are
  pinned (`arch/clientSurface.test.ts`).
- Read an error answer through `apiErrorMessage` (`api/errors.ts`); the
  inline `err.response?.data?.error` reads may only fall
  (`arch/errorChains.test.ts`).
- Open no new `EventSource`: the four existing streams are allowlisted
  (`openv/event-source-sites`), and a new one belongs in the shared hook the
  lint already allows, `hooks/useEventStream.ts` (refactor step X15a).

## Routes, styles and vocabularies

- The `<Route>` JSX of `App.tsx` stays hand-written (K13), and the route
  tree is pinned (`arch/routeTree.test.ts`). A lazy view stays lazy, and the
  stylesheet imports keep their order (`arch/cssOrder.test.ts` and the
  bundle check).
- Go is the source of each vocabulary the app copies (link rules, statuses,
  plans, error codes): change Go first, then the copy;
  `arch/vocabParity.test.ts` names a copy that disagrees.
- The e2e specs select on ids, classes, labels and roles; change one only
  together with the spec that uses it.

## Tests

- Mock the API through the barrel with `mockApi` (`test/mockApi.ts`):
  `vi.mock('../api/client', async (orig) => mockApi(await orig(), overrides))`,
  overriding only what the test reads.
- Snapshots are files under `__snapshots__/`, never inline: the Refactor
  guard fails a pull request that adds `toMatchInlineSnapshot`. A snapshot
  is regenerated with `-u` only in a pull request that changes behavior.
- A new file needs an area: a glob in `docs/areas.json` (K15,
  `arch/areas.test.ts`).

Before you finish: `npx tsc --noEmit && npm run lint && npm test`; after a
route, stylesheet import or lazy view change, also
`npm run build && node scripts/bundle-check.mjs`; and `make check-fast` from
the repository root.
