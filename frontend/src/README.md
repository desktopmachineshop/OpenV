# frontend/src: the single-page app

The React app Vite builds into the frontend image. Everything it imports
lives under `frontend/`, since `frontend/Dockerfile.prod` copies only that
directory. Commands below run from `frontend/` after `npm ci`. Plan §7.7
and §7.8 (`docs/plans/codebase-refactor.md`) have the history.
`go run ./internal/tools/areas which <path>` names a file's area.

## Areas

Every area has files here, matched by these `docs/areas.json` globs
(relative to this directory; `api/` has its own guide, `api/README.md`):

| Area | Globs here |
|---|---|
| frontend-shell | `*` (the files directly here), `components/ExpandableText*`, `components/HelpSidebar*`, `components/Navbar*`, `components/ProjectLayout*`, `components/ThemeSwitcher*`, `components/helpTopics*`, `components/markdownSoftBreaks*`, `components/navSections*`, `components/panelMode*`, `components/ui/**`, `hooks/**`, `landing/**`, `manual/**`, `site/**`, `state/**`, `test/**`, `utils/publicPaths*`, `views/Landing*`, `views/ManualView*` |
| requirements-core | `components/Artifact*`, `components/ChatterPanel*`, `components/GlobalSearch*`, `components/LinkPanel*`, `components/Note*`, `components/ProjectList*`, `components/Quality*`, `components/artifact*`, `components/kanban/**`, `components/note*`, `config/**`, `utils/artifact*`, `utils/pendingLinks*`, `views/KanbanBoard*`, `views/ModuleView*`, `views/ProductOverview*`, `views/ProjectSettings*`, `views/ReviewQueue*`, `views/TodoList*`, `views/projectSettings/**`, `views/moduleView/**`, `views/review*` |
| verification | `components/EvidencePicker*`, `utils/evidence*`, `views/EvidenceView*`, `views/ImpactView*`, `views/TestRunView*`, `views/TraceabilityMatrix*`, `views/VVDashboard*`, `views/testRunCitations*`, `views/vvGapLabels*` |
| documents | `components/Attachment*`, `components/DownloadWizard*`, `components/Image*`, `components/StlPreview*`, `components/attachment*`, `utils/baselines*`, `utils/downloadSelection*`, `views/BaselineCompare*` |
| tenancy-identity | `components/Avatar*`, `components/CreateOrgModal*`, `components/OrgSwitcher*`, `components/User*`, `components/org/OrgLimits*`, `components/org/OrgMembers*`, `components/org/OrgTeams*`, `components/org/OrgUsage*`, `utils/activeOrg*`, `views/Login*`, `views/OrgSettings*`, `views/ResetPassword*`, `views/VerifyEmail*` |
| agent-suite | `components/ChatMarkdown*`, `components/agents/**`, `components/crews/**`, `components/org/OrgProviders*`, `components/wizard/**`, `views/Agent*`, `views/AutomationsPage*`, `views/CrewBuilder*`, `views/GuidedWizard*`, `views/Interview*`, `views/WorkspaceRuns*` |
| runner-fleet | `components/RunnerConnectPrompt*`, `components/org/*Runner*`, `components/org/WorkerKeys*` |
| events-notifications | `components/NotificationBell*`, `push/**`, `views/ActivityLog*` |
| community | `components/SharedProductVotes*`, `utils/randomProduct*`, `views/SharedProjectView*` |
| billing | `components/org/OrgBilling*` |
| platform-http | `components/ReleaseUpdateBanner*`, `views/PlatformAdmin*`, `views/WhatsNew*` |
| tooling | `arch/**`, with `frontend/scripts/**` and `frontend/eslint.config.js` |

## Map

| Glob | What it holds |
|---|---|
| `index.tsx`, `App.tsx` | the entry with the eager stylesheet imports in order; the hand-written `<Route>` table (K13), each view eager or `lazy()` |
| `index.css`, `theme.css`, `theme.ts`, `appShortcuts.ts` | global styles, the theme and its storage key, keyboard shortcuts |
| `features.ts` | the feature gates with no module of their own to sit beside |
| `views/*.tsx` | the pages; a split view keeps its path and export, with its panes in `views/moduleView/` or `views/projectSettings/` |
| `components/**` | shared components; `components/ui/**` is the UI kit, and `components/agents/`, `components/crews/`, `components/kanban/`, `components/org/` and `components/wizard/` hold one feature each |
| `components/ProjectLayout.tsx`, `components/navSections.ts`, `components/helpTopics.ts` | the project shell: its `navSections` list, the menu state, the help topic per path |
| `hooks/**`, `state/**`, `utils/**`, `config/**` | `useFeature` and other hooks, the store and the active-workspace storage, helpers, the link-rule UI config |
| `landing/**`, `site/**`, `manual/**`, `push/**` | the front page, the public site, the user manual's chapters, web push |
| `test/mockApi.ts` | `mockApi`: stubs every method of `api/client` for a `vi.mock` (F2) |
| `arch/**` | the architecture tests of S12, S12b and S13, their helpers and `arch/__snapshots__/*.txt` |
| `**/*.test.ts`, `**/*.test.tsx`, `**/__snapshots__/**` | vitest tests, beside what they test; snapshots are files, never inline |
| `frontend/scripts/*.mjs` | `bundle-check.mjs` (S12b), `tsdeclhash.mjs` and `tsmovecheck.mjs` (S14a), `tsdeclmove.mjs` (S14f), `scaffold.mjs` (N3), with their `frontend/scripts/*.test.mjs` |

## Invariants (plan §3) that bind here

- **I19 routes.** The `<Route>` elements of `App.tsx`, eager or lazy, the
  relative redirects, the query params the app reads, the deep links the
  backend builds, and the public paths (`utils/publicPaths.ts`).
- **I20 CSS cascade and bundle.** The eager stylesheet order from
  `index.tsx`; `ProjectList.css` redefines `.button` and must stay eager;
  the emitted CSS and the lazy chunk boundaries.
- **I21** screens, copy, ARIA roles and the ids and classes e2e relies on.
- **I22 storage keys.** `openv_active_org` (sessionStorage, then
  localStorage), `openv_last_project`, `openv-theme`.
- **I23, K12** every call is a route the server registers; code outside
  `api/` imports only `api/client`, `api/errors`, `api/baseURL` and
  `api/contentDisposition`.
- **I24** each vocabulary copied from Go, today's drift included (S13).
- **K14** a new file at most 600 lines and a component at most 300.
- **Q20, Q21.** No new inline `err.response?.data?.error` read; no new
  `EventSource` outside the shared hook.

## Recipes

**Add a page.**
1. The view in `views/`, then its `<Route>` in `App.tsx`, through `lazy()`
   unless it must be eager.
2. Keep the parallel lists in step by hand: for a project page, its
   `navSections` entry in `components/ProjectLayout.tsx`; the topic in
   `components/helpTopics.ts`; a chapter in `manual/index.ts` if it has
   one; `utils/publicPaths.ts` if it needs no session; and its screen in
   `e2e/tools/phone-audit.js`.
3. Claim its file in `docs/areas.json` if no glob does, then regenerate the
   route tree and the bundle shape:
   `cd frontend && npx vitest run src/arch -u` and
   `cd frontend && npm run build && UPDATE_BUNDLE_SHAPE=1 node scripts/bundle-check.mjs`.
Scaffold: `node frontend/scripts/scaffold.mjs page <name>` (X17, in N3's tool)

**Add an API call.** In `api/`; see `api/README.md`.

**Read a feature flag.** `useFeature(X_FEATURE)` from `hooks/useFeature.ts`,
where `X_FEATURE` is a const typed `FeatureKey` (`generated/contract.ts`), so
its key must be one `internal/domain/release/features.go` registers. The
const sits beside the module that owns the gate, or in `features.ts` when
none does.

**Change a vocabulary the Go side owns.** Change Go first
(`internal/domain/README.md`), then the copy here; `arch/vocabParity.test.ts`
names a copy that disagrees with `contracts/vocab.json`.

## Guards

Each line: the guard, what it pins, and the command that runs it.

- **S12** `arch/routeTree.test.ts`, `arch/publicAccess.test.ts`,
  `arch/deepLinks.test.ts`, `arch/inventories.test.ts`: the route tree, public
  paths, backend deep links, storage keys and query params (I19, I22).
- **S12** `arch/clientRoutes.test.ts`, `arch/clientSurface.test.ts`,
  `arch/interceptors.test.ts`: every call a server route, the barrel's
  export surface, the axios interceptors (I22, I23).
- **S12b** `arch/cssOrder.test.ts` and `arch/sizeBudget.test.ts`: the
  cascade (I20) and K14; `frontend/scripts/bundle-check.mjs`, the bundle
  shape in `frontend/scripts/testdata/bundle-shape.json`.
- **S6, S13** `arch/sseListeners.test.ts` (I9) and `arch/vocabParity.test.ts`
  (I24); `arch/errorChains.test.ts` (Q20); `arch/areas.test.ts` (K15).
  All of `arch/`: `cd frontend && npx vitest run src/arch`
- **ESLint** `openv/import-boundaries` (K12, components importing views) and
  `openv/event-source-sites` (Q21). `cd frontend && npm run lint`
- **S14a, S14f** the TypeScript move proofs and F1's generator.
  `cd frontend && node --test 'scripts/*.test.mjs'`
- **S16** `views/*.snapshot.test.tsx`: DOM snapshots of the views being
  split, in `views/__snapshots__/` (I21).
  `cd frontend && npx vitest run src/views`; the Playwright journeys are in
  `e2e/README.md`.

A snapshot changes only with `-u` in a pull request that changes behavior
(R3); the bundle shape only with `UPDATE_BUNDLE_SHAPE=1`.
