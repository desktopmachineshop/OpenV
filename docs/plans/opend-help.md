# Plan: OpenV help on OpenD, one click from every screen

Move the user manual and the in-app help onto OpenD-hosted docs, served for
the release each workspace is on, and retire the manual compiled into the
frontend. Status: **in progress**. Decided:
- the docs source lives in this repository under `manual/`;
- docs.openv.app is a public site that shows the latest line by default,
  with a version menu to switch lines;
- help is drawn natively in OpenV's own drawer, through OpenV's backend;
- the docs site is a **shared** OpenD-hosted site (plan `oss`, OpenD-hosted
  ADR 0004) at `https://openv.opend.app`: shared plans take no custom
  domain, which the native drawer does not need, since OpenV's server is
  the only caller;
- **no secrets cross between the two:** OpenV reads the site's public view,
  anonymously, asking for the workspace's line. The only setting is
  `OPENV_DOCS_URL`. Per-plan content (an embed secret and a reader per
  profile, decision 2) stays available for when the manual needs it.

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

1. **Draw help natively, through OpenV's backend.** *(Decided.)* The browser
   calls only OpenV; OpenV's API calls OpenD's JSON help API and returns
   sanitized topics that render in OpenV's own drawer.
   - What it buys: OpenV's CSP and `X-Frame-Options` are untouched, no
     third-party cookie is needed, and the embed secret never reaches the
     browser.
   - The alternative is OpenD's iframe panel. It is quicker to ship, but
     needs `frame-src`/`script-src` for the docs origin and is styled by
     the docs theme, not by the app.
2. **Service readers per profile** *(optional; off without
   `OPENV_DOCS_EMBED_SECRET`)*. OpenD only exchanges tokens for
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
   the public entitlements: the free plan, on the line labelled *latest* by
   default, with the version menu to read any other public line. That keeps
   the manual's search traffic and serves self-hosters.

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
- **The public site** uses OpenD's version picker (OpenD ADR 0016), a
  `<details>` menu that works without script. The repository is on `minor`
  release lines (OpenD PR #39), so a chosen version serves that minor's
  own build. The picker is in `live` mode and the public version is
  `latest`, so the site follows releases by itself: each `docs/<minor>`
  build joins the menu and becomes the default once live. Nothing in the
  release workflow calls the docs site.
- **The in-app help** asks the public help API for `version=<line>`; the
  site answers from that line, or from its default when the line is not
  live yet, and names the line it served in `OpenD-Release-Line`.
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
  the navigation tree, never a new tab. Until that reader exists, **Full
  manual** opens the docs site's front page at the workspace's line in a
  new tab, beside **Open in the docs** for the topic itself.

## Workstreams

| | Work | Repo | State |
|---|---|---|---|
| A | OpenD-hosted **shared** tenant `openv` (plan `oss`) at `https://openv.opend.app`:<br>• repo `desktopmachineshop/OpenV` through the GitHub app, `docsPath: manual`, `releaseRefs: ['docs/*']`<br>• public; minor release lines; picker `live`, public version `latest`, all declared in `manual/opend.config.yaml`<br>• `OPENV_DOCS_URL` on OpenV's Railway service, nothing else | openv-hosted ops | tenant **created**, GitHub app installed; the site connects itself and takes its settings from the `site:` block of `manual/opend.config.yaml` (OpenD ADR 0017) |
| B | The manual as DITA: one topic per section, context keys, `help-contexts.yaml`, `redirects.json` for the old links | openv | **done**, generated from the chapters until D ships |
| C | Help proxy (`internal/domain/help`, the `help-docs` capability, `OPENV_DOCS_*`):<br>• `GET /api/v1/orgs/{id}/help/{context,topic,search}` (nav to come)<br>• profile and version, embed token, allowlist sanitize, links rewritten to `/manual/…`, image proxy<br>• per-profile cache, graceful "help unavailable" | openv | **done**, without the image proxy and nav; off until `OPENV_DOCS_URL` is set (public mode without the embed secret) |
| D' | Turn it on: the `manual-help` feature key on the routes and the panel, its release note, the manual's help section, and public mode (no secret) for the help service | openv | **done** in the switch-on PR |
| D | Frontend:<br>• `HelpSidebar` on the proxy<br>• `HelpLink` and `useHelp()`, "?" on every shell<br>• `help.key` in `pages.ts`<br>• native `/manual` reader<br>• behind the `opend-help` feature key | openv | **done but the reader**, behind `manual-help` (the key D' shipped; this rides it as a fix to the drawer):<br>• `DocsHelp` in `HelpSidebar` sends a key, never a bare path: `help.key` on every signed-in page of `pages.ts`<br>• `hooks/useHelp.tsx`: the panel's shared state, `useHelpContext(key)` for tabs, steps and (with `dialog`) dialogs, the innermost and latest winning, and the panel following it live; `Modal`'s `help` prop<br>• `HelpLink` (`components/ui`) beside headings, empty states and dialog titles; `?` and F1<br>• "?" on every shell: `Navbar` mounts the panel on workspace settings, runs, admin and What's new<br>• "Full manual" and "Open in the docs" open the docs site at the workspace's line, in a new tab, through the site's `/_version` route<br>The native `/manual` reader is still to do |
| E1 | `GET /api/v1/help/nav` for embed tokens | opend | **done** (OpenD #39) |
| E2 | Public context redirect `/context/<app>/<key>`, at the version the request is served, for self-hosted OpenV | opend | **done** (OpenD #39) |
| E3 | Release lines by `minor` as well as `major`, per repository | opend | **done** (OpenD #39) |
| E4 | The public version menu | opend | **done** by OpenD's own version picker (#34); OpenV's release workflow sets its list and the public version once the tenant (A) exists |
| F | Guards: the *Docs* job, `make check`/`check-fast`, the promote steps (`docs/docs-maintenance.md`) | openv | **done**, with key resolution: `docs.py sync --check` fails a key that `pages.ts`, a `useHelpContext`, a `HelpLink`, a dialog or `help-contexts.yaml` sends and no topic holds, directly or through a parent; `pagesHelp.test.ts` fails a signed-in page with no key |
| G | Retire:<br>• delete `frontend/src/manual`, `helpTopics.ts` and the site pre-render of the manual, here and in openv-hosted<br>• 301 `/manual/*` to docs.openv.app; sitemap and `llms.txt` there<br>• flip the source to `manual/` and drop `docs.py sync` | openv, openv-hosted | after stable carries `opend-help` |
| H | Requirements:<br>• help for the current screen in one action from every signed-in page<br>• help served for the workspace's release<br>• public version menu<br>• docs gate on promotion<br>• a design item for the proxy, a Playwright help journey<br>• a baseline | OpenV, OpenD projects | with C/D and E |

## Order

1. A and E3, then the site goes live from `docs/0.x` with B's content.
2. C and D behind `opend-help`, with E1, on nightly and staging.
3. `HelpLink`s across the UI, with authors writing the child topics.
4. The site's picker on `live` and public version `latest` (OpenD), so releases need no calls to it.
5. G, once stable carries `opend-help`.

## Risks

- **Gated images** need a docs session. The proxy holds one embed session
  per profile, or the manual keeps gated images out.
- **Profile count** is lines × plan families. Lines retire as stable moves
  on.
- **Public mode serves the public view:** content gated by plan stays
  hidden from every workspace until the embed secret is set.
- **The embed secret**, when used, signs in as any reader of the tenant. The tenant
  only hosts this manual; rotate the secret through the ops API.
