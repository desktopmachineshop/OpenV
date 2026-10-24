# Plan: OpenV help on OpenD, one click from every screen

Move the user manual and the in-app help onto OpenD-hosted docs, served for
the release each workspace is on, and retire the manual compiled into the
frontend. Status: **in progress**. Decided: the docs source lives in this
repository under `manual/`; docs.openv.app is a public site. Open: how the
help is drawn (the native drawer or OpenD's iframe panel), and which
release the public site shows by default.

## Where we start

- **The manual** is 16 markdown chapters compiled into the frontend
  (`frontend/src/manual`). `ManualView` serves them at `/manual` with
  substring search in the browser. openv-hosted pre-renders them for the
  sitemap and `llms.txt`.
- **Context help** is `HelpSidebar`, a summary card per page.
  - It shows 13 topics from `helpTopics.ts`; "Learn more" opens a whole
    chapter in a new tab.
  - These project pages have no topic: evidence, impact, review, todos
    and team.
  - These Navbar pages have no "?" button: org settings, runs, admin and
    What's new.
- **OpenD** already provides what we need:
  - context keys on topics (`resourceid`), each falling back to its parent
    and then to the app default, and never to a topic the reader can't see;
  - per-reader filtering by products, features and version;
  - a JSON help API (`/api/v1/help/context`, `/topics`, `/search`) behind
    short-lived HS256 embed tokens signed by the host app's backend.

## Decisions

1. **Draw help natively, through OpenV's backend.** *(Open.)* The browser
   calls only OpenV; OpenV's API calls OpenD's JSON help API and returns
   sanitized topics that render in OpenV's own drawer.
   - What it buys: OpenV's CSP and `X-Frame-Options` are untouched, no
     third-party cookie is needed, and the embed secret never reaches the
     browser.
   - The alternative is OpenD's iframe panel. It is quicker to ship, but
     needs `frame-src`/`script-src` for the docs origin and is styled by
     the docs theme, not by the app.
2. **Service readers per profile.** OpenD only exchanges tokens for
   registered readers, and what a reader may see is set per organization.
   So OpenV signs tokens for one reader per *profile*, not per user.
   - A profile is the docs version × the plan family.
   - Each profile is one OpenD organization with entitlements
     `{version, products: [plan]}`, upserted through OpenD's
     entitlement-sync API on first use.
   - There is no per-user sync, and responses are cacheable per profile.
3. **Source in this repository, `manual/`.** *(Decided.)* The docs change
   in the same pull request as the code. CI holds the two together.
   Releases publish the docs.
4. **Keys come from `pages.ts`.** Each page sends its context key. In-page
   help links send child keys (`vv.coverage`), which fall back to the
   page's key until their topic exists.
5. **Public site.** *(Decided.)* docs.openv.app serves anonymous visitors
   the public entitlements: the free plan on the latest line. That keeps the
   manual's search traffic and serves self-hosters.

## Versions

- **The docs version is the version the workspace is on:**
  - nightly: the latest release;
  - stable: the stable release;
  - a member with preview on: the next stable release;
  - dedicated and self-hosted instances: their `OPENV_VERSION`.
- **One docs line per minor version.** Promote to release stamps
  `release:` and pushes the shipped commit to `docs/<major.minor>`, which
  OpenD publishes (`releaseRefs: ['docs/*']`). A patch release moves its
  line on. Master is never published.
- **Release gating needs no conditions:** the 0.21 line only describes
  0.21. If a line is not live yet, the backend falls back to the newest
  older live line and says which it shows.
- **The public site** gets a version menu. It is a `<details>` menu,
  because the docs pages run no script. It lists the public lines labelled
  *latest* and *stable* and links to the same topic under `/v/<line>/`.
- **Inside the app** there is no menu: the drawer reads *"Docs for OpenV
  0.21 · stable channel"*, with a link to other versions on the public site.

## The experience: one click from every screen

| Surface | One click opens |
|---|---|
| "?" in every shell, and the `?` / F1 key | the current page's topic, in the drawer |
| `HelpLink` beside section headings, empty states, dialogs, wizard steps | that element's key (`vv.coverage`, `guided.step.scope`) |
| Quality findings, `QualityBadge` | the rule's topic (`quality.rule.<id>`) |
| Error toasts, run failure classes | `errors.<code>` |
| Disabled and plan-gated controls | why it is off and how to get it (`plans.*`) |

- **The drawer** shows the topic, scrolled to its section, with "In this
  section" and search over what this workspace may see. It follows the
  route while it is open.
- **"Open full manual"** goes to an in-app `/manual/<topicId>` reader with
  the navigation tree, never a new tab.

## Workstreams

| | Work | Repo | State |
|---|---|---|---|
| A | OpenD-hosted tenant `openv`:<br>• domain docs.openv.app, embed origins openv.app and staging<br>• repo `desktopmachineshop/openv`, `docsPath: manual`, `releaseRefs: ['docs/*']`<br>• public, minor release lines | openv-hosted ops | to do (needs the control plane's admin token) |
| B | The manual as DITA: one topic per section, context keys, `help-contexts.yaml`, `redirects.json` for the old links | openv | **done**, generated from the chapters until D ships |
| C | Help proxy:<br>• `GET /api/v1/help/{context,topics/{id},search,nav}`<br>• profile and version, embed token, bluemonday sanitize, links rewritten to `/manual/…`, image proxy<br>• per-profile cache, graceful "help unavailable" | openv | to do |
| D | Frontend:<br>• `HelpSidebar` on the proxy<br>• `HelpLink` and `useHelp()`, "?" on every shell<br>• `help.key` in `pages.ts`<br>• native `/manual` reader<br>• behind the `opend-help` feature key | openv | to do |
| E1 | `GET /api/v1/help/nav` for embed tokens | opend | to do |
| E2 | Public context redirect, per version: `/v/<line>/context/<app>/<key>`, for self-hosted OpenV | opend | to do |
| E3 | Release lines by `minor` as well as `major`, per repository | opend | to do |
| E4 | Line labels (`latest`, `stable`), public lines, retention, the public version menu | opend | to do |
| F | Guards: the *Docs* job, `make check`/`check-fast`, the promote steps (`docs/docs-maintenance.md`) | openv | **done**; key resolution for `pages.ts` and `HelpLink` comes with D |
| G | Retire:<br>• delete `frontend/src/manual`, `helpTopics.ts` and the site pre-render of the manual, here and in openv-hosted<br>• 301 `/manual/*` to docs.openv.app; sitemap and `llms.txt` there<br>• flip the source to `manual/` and drop `docs.py sync` | openv, openv-hosted | after stable carries `opend-help` |
| H | Requirements:<br>• help for the current screen in one action from every signed-in page<br>• help served for the workspace's release<br>• public version menu<br>• docs gate on promotion<br>• a design item for the proxy, a Playwright help journey<br>• a baseline | OpenV, OpenD projects | with C/D and E |

## Order

1. A and E3, then the site goes live from `docs/0.x` with B's content.
2. C and D behind `opend-help`, with E1, on nightly and staging.
3. `HelpLink`s across the UI, with authors writing the child topics.
4. E2 and E4.
5. G, once stable carries `opend-help`.

## Risks

- **Gated images** need a docs session. The proxy holds one embed session
  per profile, or the manual keeps gated images out.
- **Profile count** is lines × plan families. Lines retire as stable moves
  on.
- **The embed secret** signs in as any reader of the tenant. The tenant
  only hosts this manual; rotate the secret through the ops API.
