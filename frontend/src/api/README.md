# frontend/src/api: the API client

Every request the app makes goes through this directory. Since refactor
step F1, `client.ts` is a barrel over one axios instance (`http.ts`) and
one module per area, with each area's types in `types/`. Code outside
`api/` imports only the barrel and three small modules (K12), so a test's
`vi.mock('../api/client')` still stubs every call. Plan §7.7
(`docs/plans/codebase-refactor.md`) has the history; `frontend/src/README.md`
covers the rest of the app.

## Areas

| Area (`docs/areas.json`) | Files here |
|---|---|
| frontend-shell | `client.ts`, `http.ts`, `errors.ts`, `baseURL.ts`, `contentDisposition.ts`, `*.md` |
| requirements-core | `artifacts.ts`, `projects.ts`, `types/artifacts.ts`, `types/projects.ts` |
| verification | `vv.ts`, `types/vv.ts` |
| documents | `attachments.ts`, `types/attachments.ts` |
| tenancy-identity | `account.ts`, `orgs.ts`, `types/account.ts`, `types/orgs.ts` |
| agent-suite | `agents.ts`, `guided.ts`, `types/agents.ts`, `types/guided.ts` |
| runner-fleet | `runners.ts`, `types/runners.ts` |
| events-notifications | `notifications.ts`, `types/notifications.ts` |
| community | `community.ts`, `types/community.ts` |
| billing | `billing.ts`, `types/billing.ts` |
| platform-http | `platform.ts`, `types/platform.ts` |

The globs are `frontend/src/api/**/<area>*` per area, so an area module
and its types module share a name.

## Map

| Glob | What it holds |
|---|---|
| `client.ts` | the barrel: `export * from` each area module, `export type * from` each types module, and the axios instance as its default export |
| `http.ts` | the one axios instance `client`, its two interceptors (the `X-Org-ID` header out; the sign-in and verify-email redirects back), `API_BASE_URL`, `uploadConfig`, `downloadBlob`, `saveBlob` |
| `*.ts` named after an area | that area's `xxxAPI` objects (`vvAPI`, `evidenceAPI`) and its helpers and constants |
| `types/*.ts` | that area's request and response types; the build erases them |
| `errors.ts` | reading an error answer: `apiErrorMessage`, `apiErrorCode`, `limitRefusal`, `isPlanReadOnlyError`, `retryAfterSeconds` |
| `baseURL.ts`, `contentDisposition.ts` | `getAPIBaseURL` and `resolveAvatarUrl`; `filenameFromContentDisposition` |
| `*.test.ts` | unit tests of the helpers and of uploads; the barrel's surface is pinned in `frontend/src/arch/` |

## Invariants (plan §3) that bind here

- **I23.** Every `METHOD PATH` a module sends is a route of
  `internal/api/testdata/routes.txt`, and the barrel's export surface (every
  name, its kind, and the members of each `xxxAPI` object) changes only on
  purpose.
- **K12.** Outside `api/`, imports name `api/client`, `api/errors`,
  `api/baseURL` or `api/contentDisposition` only. Tests mock the barrel,
  through `mockApi` (`frontend/src/test/mockApi.ts`) or a whole-module
  `vi.mock`.
- **I22.** The `X-Org-ID` header comes from `openv_active_org` in
  sessionStorage, then localStorage; the 401 redirect skips public pages
  and the auth calls.
- **Load order.** `client.ts` loads `http.ts` before any area module, so the
  instance and its interceptors exist before a module uses them.
- **Q20.** Error text is read through `apiErrorMessage`; no new inline
  `err.response?.data?.error`.

## Recipes

**Add an endpoint call.**
1. Add the method to its area's `xxxAPI` object in `<area>.ts`, calling
   `client` from `http.ts` with the path exactly as the server registers
   it; put new request and response types in `types/<area>.ts`.
2. Nothing else changes: the barrel re-exports the module. A new `xxxAPI`
   object or export changes the barrel's surface; regenerate the S12
   snapshots: `cd frontend && npx vitest run src/arch -u`.
3. `frontend/src/arch/clientRoutes.test.ts` fails until the route exists on
   the server (`internal/api/README.md`); add both in the same pull request.

**Add an area module.** `<area>.ts` and `types/<area>.ts`, one
`export * from` and one `export type * from` line in `client.ts`, after the
existing ones, and a glob for the area in `docs/areas.json`.
Scaffold: `node frontend/scripts/scaffold.mjs api-module <area>`

**Mock the client in a test.** Call `mockApi`
(`frontend/src/test/mockApi.ts`) in the factory of
`vi.mock('../api/client', ...)`, overriding only the methods the test
reads; every other method is stubbed, and its header shows the form.

## Guards

Each line: the guard, what it pins, and the command that runs it.

- **S12** `frontend/src/arch/clientRoutes.test.ts`: every call is a server
  route (I23).
- **S12** `frontend/src/arch/clientSurface.test.ts`: the barrel's export
  surface (I23).
- **S12** `frontend/src/arch/interceptors.test.ts`: the `X-Org-ID` header and
  the redirects around every request (I22, I23).
- **S12** `frontend/src/arch/errorChains.test.ts`: no new inline error reads
  (Q20).
  All four: `cd frontend && npx vitest run src/arch`
- **ESLint** `openv/import-boundaries`: imports from outside `api/` name an
  entry point (K12). `cd frontend && npm run lint`
- **F1's proofs** for a move between these modules: `tsdeclhash` and
  `tsmovecheck`. `cd frontend && node --test 'scripts/*.test.mjs'`
