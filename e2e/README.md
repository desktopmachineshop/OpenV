# e2e: the Playwright journeys

End-to-end tests that drive a real browser against an already running
OpenV stack; they never boot one. CI's `e2e` job (`.github/workflows/ci.yml`)
starts the compose stack and runs them on every pull request. A separate
tool, the phone audit, checks every screen's layout against a mocked API.
`go run ./internal/tools/areas which <path>` names a file's area.

## Areas

| Area (`docs/areas.json`) | Files here |
|---|---|
| frontend-shell | `*` (the config, the package files and this guide), `tests/smoke.spec.ts`, `tests/desktop.spec.ts`, `tests/landing.spec.ts`, `tests/mobile.spec.ts`, `tests/helpers.ts`, `tools/**` |
| requirements-core | `tests/review-queue.spec.ts` |
| documents | `tests/baseline-diff.spec.ts` |
| agent-suite | `tests/interviews.spec.ts`, `tests/proposals.spec.ts` |

Everything under `tests/` is a test file, which needs no area; these specs
are claimed so each journey has an owner. A new spec needs no glob.

## Map

| Glob | What it holds |
|---|---|
| `playwright.config.ts` | `BASE_URL` (default `http://localhost:3000`), one worker, serial; the projects `chromium` (every spec but the phone one), `webkit` (smoke and landing), `iphone` and `android` (`mobile.spec.ts` only) |
| `package.json`, `package-lock.json` | Playwright pinned to one version; the scripts `test`, `report`, `audit:phone`, `audit:desktop` |
| `tests/*.spec.ts` | the journeys; `smoke.spec.ts` is the core one: register, project, artifacts, link, baseline, status, search, export |
| `tests/helpers.ts` | shared steps: `makeRunId`, `makeUser`, `registerUser`, `createProject`, `openModule`, `openRequirements`, `createRequirement` |
| `tools/phone-audit.js` | the layout audit: each entry of `SCREENS` rendered from a production build on phone and desktop profiles, against a mocked API |

## Invariants (plan §3) that bind here

- **I21.** Screens, copy, ARIA roles, and the ids and classes these specs
  select on (`#type`, `#title`, `#body`, `.measure`, `.card`, the
  `Search...` placeholder, tablist roles) are frozen for a refactor; the
  S16 snapshots pin the DOM of the views being split.
- **Additive on any stack.** Every run registers its own users with a
  run-unique id, so the suite never touches data it did not make. The
  API throttles registrations per address, so the stack under test needs
  `OPENV_REGISTER_IP_BURST` raised (CI sets 100).
- **Serial.** A journey shares one page across its steps, so the suite
  runs with one worker and no shuffling.

## Recipes

**Run the suite.**
1. Start the stack: `OPENV_REGISTER_IP_BURST=100 docker compose up -d --build`
   from the repository root, and wait for the frontend on port 3000.
2. `cd e2e && npm ci && npx playwright install --with-deps chromium webkit && npx playwright test`
   (set `BASE_URL` for another stack; `npm run report` opens the HTML
   report).

**Add a journey.** A new `tests/<name>.spec.ts` that registers its own user
(`makeRunId`, `makeUser`, `registerUser`) and selects by role, label,
placeholder, title or visible text, as the others do. It runs on the
`chromium` project; add it to another project's `testMatch` only if the
engine matters. `npx playwright test --list` shows where each test runs.

**Add a screen to the phone audit.** An entry in `SCREENS`
(`tools/phone-audit.js`) with the mocked API answers it needs; serve a
production build, then `cd e2e && npm run audit:phone` and
`cd e2e && npm run audit:desktop`, and compare the summary with the
previous run (usage in the script's header).

## Guards

Each line: the guard, what it pins, and the command that runs it.

- **CI `e2e` job**: every journey on the composed stack, on every pull
  request (I21). Locally: the recipe above.
- **Spec loading**, with no stack: every spec parses and is assigned to its
  projects. `cd e2e && npm ci && npx playwright test --list`
- **S16** the frontend's DOM snapshots of the views the refactor splits:
  `cd frontend && npx vitest run src/views` (`frontend/src/README.md`).
- **The phone audit** is a tool, not a gate: it exits 1 only when a screen
  fails to render or throws, and reports layout findings.
