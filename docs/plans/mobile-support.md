# Mobile support plan

Status: approved and implemented through Phase 3, 2026-09-07 — see
section 8 for what shipped and what is deferred. Evidence base: the frontend
readiness review in `docs/assessments/2026-09-07-mobile-readiness.md`.
Requirements recorded in the OpenV Platform project under **Mobile**
(NEED-13, REQ-101 … REQ-110, DES-23, TC-50).

## 1. Where we are

OpenV's frontend is a desktop application. The facts that matter:

- **iOS cannot sign in to production.** The frontend and API live on two
  different `up.railway.app` hosts, which browsers treat as different *sites*
  (the suffix is on the Public Suffix List), so the session cookie is a
  third-party cookie (`SameSite=None`). Safari drops third-party cookies
  outright, and every browser on iOS is Safari underneath. Sign-in appears to
  succeed, the next request answers 401, and the app bounces back to the
  login page. Android Chrome still works but is on the same road. Self-hosted
  deployments on one domain are unaffected.
- **No responsive layer.** Six media queries in the whole codebase, all in
  leaf components (lightbox, gallery, project cards, help panel); no
  component knows the screen width; styling is inline pixel widths. The
  requirements workspace needs about 460 px before the document pane has any
  width at all, and the guided wizard needs 852 px with no wrapping. Both
  clip overflow rather than scroll, by design (REQ-53 fits the window).
- **No touch layer.** Every drag is HTML5 drag-and-drop, which touch
  browsers do not synthesise: reparenting an artifact and launching an agent
  from the board are drag-only. Seven artifact actions live behind a
  right-click menu. Figure actions appear on hover. Panels in auto-hide mode
  open on hover and are recovered through a 10 px edge strip.
- **Input details bite:** every field is 14 px, so iOS zooms on focus and
  stays zoomed; the login card is a fixed 380 px and overflows an iPhone SE;
  `100vh` is used where `100dvh` is needed, so bottom bars sit under the
  Safari toolbar.
- **What already works:** the app is installable (manifest with maskable
  icons, `display: standalone`, a deliberately non-caching service worker);
  routes are code-split so the heavy grid and graph libraries stay off the
  first load; the public interview chat is a genuine phone-first screen and
  works on iOS because it uses no cookie; the crew graph (Cytoscape) already
  handles touch; the design tokens support dark mode and system preference.

## 2. Who needs mobile, for what

| Persona | Jobs on a phone or tablet | Today |
|---|---|---|
| PER-4 External stakeholder | Answer an interview from a link | Works |
| PER-1 Solo founder, PER-2 Company admin | Approve or reject proposals and reviews; read a requirement someone linked; reply to a comment or ask the V&V Assistant; see a run fail and retry it; act on a budget alert; start a cloud runner and sign an agent in from the browser relay | Blocked on iOS; unusable layout elsewhere |
| PER-3 Team contributor | Move a board card, comment on it, hand work to an agent; read requirements on a tablet in a meeting or on the shop floor | Same |

Not mobile jobs, and explicitly out of scope: authoring long documents,
building crews, editing the traceability matrix, resizing columns, installing
the Agent Connector.

The transient runner with browser-relayed sign-in (REQ-57, REQ-58) is the
feature that makes a phone genuinely useful: a founder can lease a runner,
sign Claude in by pasting a code, and launch agents from anywhere with
nothing installed. The plan protects that path first.

## 3. Strategy: one responsive web app, installed as a PWA

Recommendation: make the existing React app responsive and touch-capable and
ship it through the PWA shell that already exists. Not a native app, not a
wrapper.

- The product is a live collaborative tool with no offline requirement; the
  service worker's no-cache stance stays.
- One codebase, one deployment, no store review, and every fix ships through
  the existing promote-to-release path.
- Web Push reaches installed PWAs on iOS 16.4 and later and on Android, so
  notifications do not require a native app.
- A Capacitor wrapper can be added later without redoing any of this work if
  store presence or native share sheets ever matter.

## 4. Phases

### Phase 0 — Unblock iOS (one PR, the prerequisite for everything else)

Serve the API and the app from **one origin**. Two ways; the first is
recommended because it needs no DNS and removes cross-site cookies entirely:

- **A. Proxy the API through the frontend's nginx.** Add
  `location /api/ { proxy_pass http://openv.railway.internal:8080; }` (the
  API's Railway private domain) with `proxy_buffering off` and
  `proxy_read_timeout` raised for SSE, `client_max_body_size 32m` to match
  the API's body cap, and forwarded headers. Build the frontend with
  `REACT_APP_API_URL` empty and make the client default to a relative
  `/api` on the same origin. Then the cookie is first-party,
  `CROSS_SITE_COOKIES` comes off, `SameSite=Lax` applies, CORS is unused, and
  the CSP `connect-src` collapses to `'self'`. Self-hosted compose can do the
  same or keep two ports.
- **B. A custom domain pair** (`app.example` and `api.example`) under one
  registrable domain, keeping CORS. Needs a domain and DNS; still cross-origin
  for CORS purposes but same-site for cookies.

Belt and braces in the same PR: set the `Partitioned` attribute (CHIPS) on
the session and OIDC cookies when `CROSS_SITE_COOKIES` is on, so any
remaining cross-site deployment works on Safari 18.4 and later. Add a WebKit
project and an iPhone project to Playwright so the sign-in journey is run on
the engine that failed.

Effort: one to two days. Risk: SSE through nginx (buffering), upload size
cap, and `/api/v1/public/connector/download` (27 MB) through the proxy;
keep that one on the API origin or raise the cap for it.

### Phase 1 — Layout foundation

Make the shell responsive without redesigning any view.

- A `useViewport` hook and two breakpoints: phone (≤ 640 px) and narrow
  tablet (≤ 900 px), exposed as a context so views can branch.
- `ProjectLayout`: below the phone breakpoint the side navigation becomes an
  off-canvas drawer behind a 44 px menu button; the auto-hide mode is treated
  as hidden on touch; edge strips grow to 44 px.
- The requirements workspace: below the tablet breakpoint the three columns
  become one stacked view with a segmented control (Tree, Document, Notes),
  each scrolling within its own bounds, so REQ-53's fit-the-window contract
  holds on every width instead of clipping.
- The guided wizard: rail collapses to a top progress bar; the assistant
  panel becomes a bottom sheet.
- Global fixes: `100dvh` and safe-area insets; 16 px inputs; every table in
  an `overflow-x: auto` wrapper; login and the unclamped modals capped at
  the viewport; `viewport-fit=cover` and the Apple web-app meta tags;
  `autoComplete` on the login form; `enterKeyHint` on chat composers.

Effort: three to five days.

### Phase 2 — The mobile jobs

Make the review-and-act flows first class on a phone, in this order:

1. **Review queue and proposals**: card list with approve, reject and open
   actions; the proposal diff readable at 360 px.
2. **Notifications**: the bell becomes a full-screen list; deep links land
   on mobile views.
3. **Reading requirements**: the stacked workspace from Phase 1 plus a
   reference search that jumps to a ref; comments and the V&V Assistant on
   the same chat skeleton the interview page uses.
4. **Board**: one column at a time with a pager; a card's action sheet
   offers *Move to…* and *Assign to agent*, so launching an agent no longer
   depends on drag.
5. **Runner card**: start, extend and end a cloud runner; the relayed
   sign-in screen sized for a phone (this is where a paste from a password
   manager has to work).
6. **Runs**: list, detail, retry and cancel.

Effort: one to two weeks.

### Phase 3 — Touch parity for editing

- A kebab button on every artifact row opens the same menu as right-click.
- *Move to…* dialog (choose parent and position) as the touch alternative to
  drag, using the existing `planMove` logic.
- Convert drags to pointer events with `touch-action: none` on the handle,
  so tree and board drag also work with a finger where the screen allows it.
- Figure actions always visible on touch devices; the figure reference
  autocomplete gets a tap-to-insert menu.

Effort: about a week.

### Phase 4 — Installability and polish

- Manifest screenshots and shortcuts (Review queue, Board, Notifications).
- Web Push for the high-signal notification types: a VAPID key pair, a
  push-subscription table, a subscribe endpoint, and the service worker's
  `push` handler; opt-in per device.
- `prefers-reduced-motion`; raise the muted-text contrast token to 4.5:1;
  a server-side thumbnail size for figures and a right-sized logo.

Effort: about a week, of which push is most of it.

## 5. Testing

- Playwright projects: Desktop Chrome (existing), WebKit, iPhone 13,
  Pixel 5, iPad portrait. The smoke journey runs on all of them; a mobile
  journey covers sign-in, review queue approve, board move via action sheet,
  and starting a cloud runner up to the relay screen.
- A viewport contract test: at 360 px wide no view scrolls horizontally and
  every primary action is at least 44 px.
- Lighthouse mobile in CI as an informational score, not a gate.

## 6. Requirements recorded

NEED-13 *Act on my product from my phone* and, deriving from it, REQ-101
same-site session, REQ-102 responsive shell, REQ-103 stacked requirements
workspace, REQ-104 touch alternatives for every drag, hover and right-click
action, REQ-105 tap targets and input sizing, REQ-106 mobile review and
approval, REQ-107 board actions without drag, REQ-108 runner control from a
phone, REQ-109 installable app metadata and push, REQ-110 mobile test
coverage; DES-23 same-origin API proxy; TC-50 mobile journey suite.

## 7. Decisions for the maintainer

1. Phase 0 option A (nginx proxy, recommended) or B (custom domain pair).
2. Whether Web Push is worth its backend surface now or waits.
3. Whether a store presence will ever matter (it changes nothing in Phases
   0 to 3; it decides whether Phase 4 grows a Capacitor wrapper).

## 8. Implementation status (2026-09-07)

Decisions taken: Phase 0 option A (nginx proxy); Web Push deferred; no
store presence planned.

Shipped, in one PR on top of this plan:

- **Phase 0** — `frontend/nginx.conf` proxies `/api/` to `API_UPSTREAM`
  (re-resolved through the container's DNS by
  `frontend/docker-entrypoint.d/40-openv-api-proxy.sh`); the client defaults
  to a relative `/api` in production builds; the CSP collapses to `'self'`;
  `Partitioned` on cross-site cookies; a WebKit Playwright project. The
  connector download goes through the proxy unbuffered, so no special case
  was needed. Production needs the variable changes in `docs/railway.md`
  (`API_UPSTREAM` on the frontend; `CROSS_SITE_COOKIES` off and `PUBLIC_URL`
  on the frontend origin for the API) at the next promotion.
- **Phase 1** — `hooks/useViewport`; drawer navigation with a top bar below
  900 px; the stacked Tree / Document / Notes requirements module; the
  wizard's progress strip and assistant bottom sheet; 16 px fields on touch,
  `100dvh`, table wrappers, safe-area helpers, reduced motion, clamped login
  and modals, viewport and installed-app meta tags.
- **Phase 2** — notifications panel full-width on phones; suspect links as
  cards with 44 px controls; the board shows one column at a time with a
  pager and a card actions sheet offering *Open card* and *Move to <column>*
  (the same move as a drop, so it launches an agent from To Do). Runs,
  proposals and the requirements page reuse the stacked layout and table
  wrappers rather than new mobile views.
- **Phase 3** — every artifact row has a visible ⋯ actions button opening
  the right-click menu; the menu gains *Move to…* (target artifact and
  before / after / inside, validated by `planMove`). Rows keep the HTML5
  drag: mobile browsers start it from a long press on the row, so the grip
  handle first shipped for touch was redundant and was removed after the
  maintainer's first phone session. Row controls sit in one column in the
  order they act: Move up, ⋯, Move down.
- **Manual** — below the tablet breakpoint `/manual` shows a top bar
  (Contents button, chapter title, App link); the table of contents is a
  drawer that closes on selection; tables, code and images fit the card.
- **Phase 4 (part)** — manifest shortcuts and categories; muted text raised
  to 4.6:1; reduced-motion media query.
- **Testing** — `e2e/tests/mobile.spec.ts` runs in iPhone 13 (WebKit) and
  Pixel 5 (Chromium) projects: drawer navigation, the stacked module, and
  the no-horizontal-scroll contract on every page it visits.

### Phone polish pass (2026-09-08)

A run-through of every screen at 390 px, prompted by the personal settings
modal clipping its System / Light / Dark control after "System". The
findings and the fixes, by class:

- **Clipped or off-screen controls** — dialog shells (`Modal`,
  `ConfirmDialog`, `PromptDialog`, the runner dialogs) share
  `ui/dialogCard.ts`: on a phone the card fills the width with 16 px
  padding. Personal settings is a full-screen sheet whose rows wrap.
  Segmented controls wrap. The workspace switcher's menu is fixed to the
  screen on phones. The top bar is one row (logo, workspace, bell, account)
  with the centred title dropped.
- **Two-pane layouts** — `ui/Sheet.tsx` opens the side pane as a
  full-screen sheet below 900 px: the agent editor, a run's detail, a crew
  node's settings.
- **Wide tables and grids** — the runs, automations and V&V tables show
  their identifying columns on a phone (the rest are in the detail a tap
  away); the ag-grid views pin a 150 px first column and hide the
  lowest-value columns.
- **Tap targets** — a touch-screen floor in `index.css`: 40 px for
  `.button`, `.button-secondary`, `.icon-btn`, selects and text fields,
  36 px for every other button, 18 px ticks. Inline-styled buttons that
  set their own padding carry `minHeight` explicitly.
- **Text** — nothing under 12 px: chips, refs, timestamps, tree glyphs,
  upload hints.
- **Grids that never collapsed** — Product Overview personas / needs, the
  agent editor form, the version compare, the workspace details all go to
  one column on a phone.

The regression tool is `e2e/tools/phone-audit.js` (`npm run audit:phone`
in `e2e/`): it renders a served production build on a Pixel 5 against a
mocked API, opens the sheets and dialogs reachable from 48 screens, and
reports elements past the viewport, clipped containers, tap targets under
32 px, text under 12 px and page errors, with a screenshot per screen.
`mobile.spec.ts` covers the four worst cases end to end: the theme
options in personal settings, the workspace switcher menu, the agent
editor sheet, and the runs and automations tables.

### Desktop pass (2026-09-08)

The same run-through for desktop displays, from a 1024 px laptop to 4K.
The audit tool gained ten desktop profiles (`npm run audit:desktop` in
`e2e/`): 1024×768, 1280×800, 1366×768, 1440×900, 1536×864 (Windows at
125 %), 1920×1080, 2560×1440, 3840×2160 at a device pixel ratio of 2, and
HiDPI variants of 1440 and 1920 at 2×. On a desktop profile the tool
clicks instead of tapping, skips the phone-only drawer screens, applies a
24 px click-target floor instead of the 32 px tap floor, and adds three
wide-screen checks: text blocks wider than 900 px with over 200
characters (unbounded line length), single-line fields wider than 720 px,
and raster images drawn larger than their pixels (blur on HiDPI). Nothing
overflowed, nothing was blurry, and no text block ran too long; the
findings and the fixes, by class:

- **A squeezed document at 1024 px** — the requirements module drew the
  tree at its saved 400 px and the notes column at 320 px, which left the
  document a 50 px sliver with 22 px-wide form fields. The tree column is
  now drawn at its saved width clamped so the document keeps at least
  420 px (`ModuleView`), and a first visit under 1200 px starts the notes
  column auto-hidden; a saved choice still wins.
- **Reading surfaces on a wide display** — one token, `--measure: 1100px`
  in `theme.css`, and a `.measure` utility in `index.css`. The artifact
  document and its editor, and the agent editor, stop growing there and
  stay left-aligned with their pane; inside a measured surface a
  single-line field or a hint under one stops at `--measure-field`
  (720 px). Tables keep their full width and scroll.
- **Stretched fields** — the baseline selector took the toolbar's whole
  width (a 2320 px `<select>` at 2560); it is now `width: auto`. Workspace
  name fields, run review notes and the bulk review note carry a maximum
  width.
- **Click targets and text** — the "Menu: Pinned" and "Notes: Pinned"
  mode buttons were 22 and 18 px tall at 11 px; the tree's move up and
  down arrows 18 px; the test-run status select 22 px; the manual's
  heading links 22 px; the collapsed-panel strip glyph 10 px. All are at
  or above 24 px and 12 px now.

`desktop.spec.ts` (chromium only, viewports from `test.use`) keeps the two
broken contracts under CI: at 1024×768 every page fits without sideways
scrolling and the document keeps its 400 px; at 2560×1440 the document
and the editor's title field stop at the measures.

Deferred, each a follow-up of its own:

- Web Push (VAPID keys, a subscription table, a subscribe endpoint, the
  worker's `push` handler) — REQ-109 remains partially met (installable,
  no push).
- The runner card and the relayed sign-in screen sized for a phone
  (REQ-108); they work through the responsive shell but were not reworked.
- Figure actions on touch and the tap-to-insert reference menu; server-side
  figure thumbnails; manifest screenshots; an iPad project and Lighthouse in
  CI.
