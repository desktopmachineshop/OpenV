# Search and AI visibility plan

Status: approved 2026-10-13 (decision 1: source-available, reversed
2026-10-16: relicensed under MIT). Phase 1 (4.1
to 4.3) and the wording change shipped in 0.17.3; 4.4 (Search Console, Bing
Webmaster Tools) is the maintainer's. Phase 2's first batch (section 5.1) is
built; `/capabilities` and the comparison pages are open, as are Phases 3
and 4. Evidence base:
`docs/assessments/2026-10-13-ai-visibility.md` (Conviora scan, baseline
**0 / 100**, OpenV in 0 of 60 unbranded AI answers, plus direct checks of the
site and repository).

## 1. Where we are

- **Crawlers see an empty page.** Every URL on openv.app, `/robots.txt`,
  `/sitemap.xml` and `/llms.txt` included, returns the same SPA shell with
  an empty `<div id="root">`. The words that would win the buyer questions
  (traceability, V&V evidence, ReqIF, baselines, self-hosted) exist only in
  meta tags and in JavaScript.
- **Nobody else talks about us.** AI answers are built from comparison
  listicles (ones.com alone had 67 citations), vendor docs and open-source
  docs and repositories. OpenV is on none of them. The GitHub repository has
  no description, homepage or topics.
- **The name is ambiguous.** Engines that cannot ground "OpenV" fill in a
  formal-verification tool, a CAD/geometry project, or OVM/OpenVera.
- **Our claims read as unverifiable.** ChatGPT and Claude called the
  self-hosting, ReqIF and audit claims "unverified". Some of that is the
  empty site; some is that we say "open-source" under an Elastic License 2.0.

The good news: the product fits all 12 buyer questions, the copy is already
written (`frontend/src/site/content.ts`, `frontend/src/landing/content.ts`,
`frontend/src/manual/chapters/`), and where an engine did ground its answer
in openv.app (Gemini and Google AI Mode, on the comparison question) it
described OpenV accurately and favourably. Most of
this is a plumbing problem, then a content problem, then a distribution
problem, in that order.

## 2. How the two channels work, and what that means for the order

- **Search engines** (Google, Bing) need fetchable HTML, a sitemap, one
  canonical host, and inbound links.
- **AI assistants** answer in two ways. *Retrieval* (ChatGPT search,
  Perplexity, Google AI Mode, Claude with search, Copilot) runs a web search
  at question time, mostly against the Bing and Google indexes, and quotes
  what it fetches. This can change within weeks of pages being indexed.
  *Model memory* (an answer with no search) reflects training data and moves
  only with the next model release, months later, and only if OpenV is
  widely written about by then.

So: fix crawlability first (nothing else counts without it), then publish
pages that answer the exact buyer questions, then get OpenV onto the third-party
sources the engines already trust. Model memory follows the third.

## 3. Decisions needed first (maintainer)

1. **"Open source" or "source-available"?** `LICENSE` is the Elastic
   License 2.0. It is not an OSI licence, GitHub reports it as
   "NOASSERTION", and a reviewer or an engine that checks will call the
   claim inaccurate, which is exactly the "unverified" verdict we got.
   Options: (a) say **"source-available, free to self-host"** everywhere
   (index.html, README, site, listings); or (b) relicense (e.g. Apache-2.0
   or AGPL-3.0 for the core) and keep "open source". (b) gets OpenV into
   the "best open-source requirements tools" lists, where StrictDoc,
   Doorstop and Tuleap currently win on that label alone. (a) is honest and
   costs nothing. **Decided 2026-10-13: (a), source-available.** The site,
   `index.html`, `llms.txt` and the README say so; every listing and page
   from here on uses the same words.
   **Reversed 2026-10-16.** OpenV is relicensed under the MIT License, an
   OSI licence, so the site, `index.html`, `llms.txt` and the README now
   say "open source" and name it.
2. **One-line descriptor, used verbatim everywhere.** Proposed: *"OpenV is
   a requirements management and V&V platform: traceability from need to
   test evidence, baselines, ReqIF exchange and AI agents, hosted or
   self-hosted."* The same sentence goes in the site's first paragraph,
   JSON-LD, GitHub, every listing and every bio. Repetition is how
   engines resolve the entity and stop confusing it with OpenVera and the
   others.
3. **Canonical host.** openv.app. The Railway frontend host should stop
   being indexable (see 4.1).

## 4. Phase 1: make the site crawlable (repo work, about a week)

Everything here is in `frontend/`. It changes what the platform serves, so
per `CLAUDE.md` it needs a `### Maintenance updates` release-notes bullet
and requirements in the OpenV Platform project (a **Discoverability**
need, REQs for robots/sitemap/pre-rendered public pages/structured data,
a test case each).

### 4.1 robots.txt, sitemap.xml, llms.txt

Built as follows.

- The build writes all three from `frontend/src/site/crawl.ts` into
  `build/.openv-crawl/` (which nginx refuses to serve), with the address left
  as a placeholder. `frontend/docker-entrypoint.d/46-openv-crawl.sh` fills it
  in at container start from a new **`OPENV_SITE_URL`** (`https://openv.app`
  on production) and copies them to the web root, so a self-hosted or
  staging instance never advertises openv.app's sitemap.
  - `OPENV_NOINDEX` set → `robots.txt` allows everything (so the existing
    `X-Robots-Tag: noindex` is still seen, per that script's own reasoning)
    and there is no sitemap or `llms.txt`.
  - `OPENV_SITE_URL` set → one group naming Googlebot, Bingbot,
    OAI-SearchBot, ChatGPT-User, GPTBot, Claude-SearchBot, Claude-User,
    ClaudeBot, PerplexityBot, Perplexity-User and Google-Extended alongside
    `*`, so all of them get the same rules; `Disallow` for `/api/`,
    `/projects`, `/org/`, `/admin`, `/whats-new`, `/interview/`,
    `/verify-email` and `/reset-password`; and a `Sitemap:` line. Share links
    are not disallowed: their page says noindex itself, and a crawler barred
    from fetching it would never read that.
  - Neither → the same `robots.txt` without the `Sitemap:` line, and no
    sitemap or `llms.txt`.
- `sitemap.xml` lists the public site routes from `frontend/src/pages.ts`
  (`public: true`, scope `site`, no `:param`, less the sign-in and token
  pages) plus each manual chapter. `crawl.test.ts` fails when a new public
  page has no summary or a private one is not disallowed. Published
  open-source project pages come from the API at run time and are not yet
  listed; they join when pages are pre-rendered (4.2).
- `llms.txt`: the descriptor, the licence in one paragraph, then one line
  and link per public page and manual chapter.
- nginx: exact-match `location = /robots.txt` etc., so a file a deployment
  does not publish is a 404 and never the app's `index.html`.

### 4.2 Pre-render the public pages

Built as follows, by server rendering rather than the headless browser
first proposed: the site pages turned out to render in Node unchanged
(`useViewport` already falls back to a desktop width without `window`, and
the pages fetch their data after mounting), and server rendering keeps
Chromium out of the Railway image build.

- `frontend/src/site/prerender.tsx` renders every page `crawlPages()` lists
  with `react-dom/server` through a `MemoryRouter` whose table
  (`PRERENDER_ROUTES`) gives each public path the element `App.tsx` gives
  it. A Vite plugin (`openv-prerender-pages` in `vite.config.ts`) runs it
  after the bundle is written and saves one copy of the built `index.html`
  per page under `build/.openv-prerender/`: the page's markup in a
  `#prerender` element beside the empty `#root`, in place of the
  `<noscript>` notice, and its own `<title>`, description, `og:*`,
  `<link rel="canonical">` and `og:url`.
- `46-openv-crawl.sh` finishes them at start into `prerendered/` (address
  filled in, or the address lines dropped without `OPENV_SITE_URL` or with
  `OPENV_NOINDEX`), and nginx serves `prerendered/index.html` for `/` and
  `prerendered$uri.html` for any other path before the app shell. Direct
  requests for `/prerendered/…` are 404.
- The hand-off: `index.css` hides `#root` while `#prerender` is present.
  `SiteShell` and `ManualView` remove the snapshot as they mount, `App`
  removes it as soon as it knows the visitor is signed in (the front page
  then redirects to `/projects`), and `index.tsx` removes it after ten
  seconds whatever happens. Measured in Chromium on `/`, `/pricing` and a
  manual chapter at desktop and phone widths: the snapshot is on screen at
  first paint, the live page replaces it 100 to 200 ms later, no frame
  shows the app's "Loading…" or a blank page, and `/pricing` still scrolls
  to the plans.
- `/pricing` (the front page scrolled to its plans) and `/manual` (the
  first chapter) name `/` and `/manual/getting-started` as canonical and
  are left out of the sitemap (`CANONICAL_PATHS` in `crawl.ts`).
- What a page fetches after mounting is not in its snapshot: the
  open-source page's project list and the live plan prices read from the
  API. Both pages still carry their full static copy.
- Guards: `site/prerender.test.tsx` fails if a listed page renders fewer
  than 150 words, if two canonical pages render the same, or if
  `index.html` drifts from what `pageDocument` edits; the build fails on
  the last too. `scripts/crawl-entrypoint.test.mjs` covers the start-up
  step.

### 4.3 Structured data and host hygiene

- Done: JSON-LD in the pre-rendered `/`: `Organization` (Desktop Machine
  Shop, logo, `sameAs` GitHub) and `SoftwareApplication`
  (`applicationCategory: BusinessApplication`, web and self-hosted Docker,
  `isAccessibleForFree`, licence URL, the descriptor). No `offers` price is
  given, since the site states none in a currency. `FAQPage` on `/faq`,
  generated from the same `content.ts` entries the page renders, so it
  cannot drift. Add LinkedIn to `sameAs` once the page exists.
- Done: `og:image` / `twitter:image` → `https://openv.app/Images/og.png`,
  not the Railway host.
- Done: with `OPENV_SITE_URL` set, every host name but the site's own gets
  `X-Robots-Tag: noindex, nofollow` (a `map` on `$host` the entrypoint
  writes). The Railway host keeps serving rather than redirecting, because
  health checks and the API proxy may use it.

### 4.4 Get indexed

- Google Search Console and **Bing Webmaster Tools** (Bing's index feeds
  ChatGPT search and Copilot) for openv.app; submit the sitemap; request
  indexing of the main pages.
- IndexNow ping (Bing, Yandex, Seznam) from the Promote to release workflow
  for changed public pages. Optional, one curl.

**Exit check:** `curl https://openv.app/faq | grep -c "ReqIF"` > 0;
robots/sitemap return text/plain and XML; Search Console shows the pages
indexed; re-run the Conviora technical audit and it should report
structured data and canonical present.

## 5. Phase 2: pages that answer the buyer questions (2–4 weeks)

### 5.1 Built: the first batch

Seven topic pages, each data in `frontend/src/site/topics/` rendered by one
layout (`site/TopicPage.tsx`): `/requirements-traceability`,
`/verification-evidence`, `/baselines-and-audit-trail`, `/reqif`,
`/self-hosted-requirements-management`, `/safety-critical` and
`/glossary`, plus an "OpenV in brief" FAQ group carrying the scan's three
branded questions word for word. Each page opens with a two-to-four
sentence answer, ends with "What OpenV does not do", links into the manual,
carries a dated "Updated" line and a `TechArticle` (or, for the glossary,
`DefinedTermSet`) JSON-LD, and is pre-rendered, in the sitemap and in
llms.txt through the existing guards. `site/topics/topics.test.ts` holds
every page to those rules, and to plain text that never calls OpenV open
source.

The copy was written from a fact sheet of the code and docs, with file
references, and checked claim by claim. Writing it found statements on the
site the product does not back, now corrected: agents "never" writing
directly (they can, in `direct` mode), a baseline holding attachments (it
holds their list), the quality linter checking EARS (it does not), and the
figure cap in `docs/api-spec.md`. It also found the app's import button
accepted only JSON although the API takes ReqIF; the next PR let it take
`.reqif` and `.xml` files and updated the ReqIF page and the manual.

Rules the pages keep, and the comparison pages will too: state limits
as plainly as capabilities (no e-signatures, no separate approver role, no
MFA or SAML of its own, ReqIF untested against DOORS or Polarion, no
certification or qualification kit); never claim a standard; date every
page.

### 5.2 Planned

One page per question cluster from the scan, each server-rendered (4.2),
written to the buyer's own words, and **every capability claim traceable to
a requirement in the OpenV Platform project with its verification status**.
That last part is our edge: Jama and Polarion assert, we can show.

| Page | Answers (scan intent) | Must contain |
|---|---|---|
| `/requirements-traceability` | Recommendation, use case: end-to-end traceability, gap finding | need → requirement → design → hazard → test chain; trace matrix and impact view screenshots; how gaps are found (V&V gaps view) |
| `/verification-evidence` | Use case: test runs, verification status, audit-ready reports | test runs, results, evidence attachments, coverage, report export; a sample exported report |
| `/baselines-and-audit-trail` | Recommendation: regulated orgs, baselines, history | baselines, compare, audit log, sign-off/review, export of a baseline |
| `/reqif` | Alternative, comparison: ReqIF exchange | what imports and exports; round-trip with DOORS/Polarion; known limits |
| `/self-hosted-requirements-management` | Recommendation: self-hosted / "open source" | Docker deployment, prerequisites, data ownership, licence in plain words (per decision 1), what self-hosted gets vs hosted |
| `/compare` hub plus `/compare/doors`, `/compare/jama`, `/compare/polarion`, `/compare/codebeamer`, `/compare/reqview`, `/compare/strictdoc-doorstop` | Comparison, alternative | a neutral capability table; "choose them if…" next to "choose OpenV if…"; migration path via ReqIF; dated |
| `/safety-critical` (or a FAQ section) | Category discovery, branded diagnostics | honest scope: what evidence OpenV stores and traces; **what it does not claim** (no tool qualification kit, no DO-178C/ISO 26262/IEC 62304 certification); alpha status; how to validate it yourself |
| `/capabilities` | All, and the "unverified" verdicts | a table generated from the OpenV Platform project: requirement, status (shipped / beta / planned), verification status, date. Rebuilt on release. |
| `/glossary` | Category discovery | V&V, trace matrix, baseline, ReqIF, hazard, verification evidence, needs vs requirements, each with a link into the product page |

Rules for this content:

- **Honest limits on every page.** The scan's own warning: unsupported
  comparison claims make branded answers *worse*. Engines repeat a
  "limitations" section verbatim, and it makes the rest of the page
  credible.
- **Lead with the answer.** The first two sentences of each page should be
  quotable on their own; that is what retrieval engines lift.
- **Name the competitors on the page.** "Alternative to DOORS" pages are
  how a page enters "alternatives to DOORS" answers.
- **Dates and authorship.** A visible "updated" date and a named author or
  company on each page.
- Expand the FAQ with the three branded-diagnostic questions verbatim: "Is
  OpenV a good fit for a regulated systems-engineering program?", "How does
  OpenV compare with enterprise tools?", "What are OpenV's features,
  deployment options and limitations?"
- Publish the manual as pre-rendered pages (already routed at
  `/manual/:chapterSlug`). Docs sites are a source type the engines cite
  (strictdoc.readthedocs.io and docs.tuleap.com both appear).

## 6. Phase 3: be where the engines already look (ongoing, maintainer-led)

Mapped to the source types in the scan's citation table:

**Repositories** (github.com: 7 citations)
- GitHub: description = the descriptor, homepage = https://openv.app,
  topics (`requirements-management`, `traceability`,
  `verification-and-validation`, `reqif`, `systems-engineering`,
  `safety-critical`, `alm`, `self-hosted`, `mbse`), a README whose first
  paragraph matches the descriptor, screenshots, and a licence line that
  matches decision 1. Ten minutes; the cheapest high-value item here.
- Submit to curated GitHub "awesome" lists for requirements engineering,
  systems engineering and self-hosted software (awesome-selfhosted requires
  an OSI or similar licence, which depends on decision 1).

**Listicles and review sites** (ones.com 67, matrixone.health 20,
thedigitalprojectmanager.com 10, thectoclub.com 5, titanapps.io 7, gitnux,
worldmetrics)
- Email the authors of the specific "best requirements management tools"
  articles that were cited, with a two-line pitch, a free account, and the
  `/compare` page. These lists are updated yearly and are the single
  largest input to the answers.
- Create listings on G2, Capterra / GetApp / Software Advice, SourceForge,
  AlternativeTo (as an alternative to DOORS, Jama, Polarion, ReqView) and
  Product Hunt. Ask the first real users for reviews.

**Community and press** (reddit.com, hackernoon.com, infoq.com,
thenextweb.com)
- Show HN post when Phase 2 is live.
- Genuine, disclosed answers in r/systems_engineering, r/embedded and
  r/QualityAssurance threads asking for DOORS alternatives, and in the
  INCOSE community. Answering questions, not dropping links.
- One technical article on a cited publication (e.g. "Requirements
  traceability with AI agents inside the audit trail" on HackerNoon or
  InfoQ).

**Standards and interoperability**
- Get listed wherever ReqIF tool support is catalogued (the ReqIF
  implementor community, Wikipedia's ReqIF article tool list, where
  supported by a citable source).
- White papers (the `/white-papers` page exists): one on verification
  evidence for regulated work, citing the NASA/FDA guidance the engines
  already use.

**Entity identity**
- LinkedIn company page and a Wikidata item (software, developer, website,
  licence, GitHub repo), both using the descriptor. Add both to JSON-LD
  `sameAs`. This is the direct fix for the OpenVera/OVM confusion.

## 7. Phase 4: measure

| Signal | Tool | Baseline (2026-10-13) | 3-month target |
|---|---|---|---|
| AI Visibility score | Conviora re-scan, same 12 questions, monthly | 0 / 100 | Present in ≥ 10 % of answers; cited at least once |
| Branded accuracy | the 3 branded questions, by hand, monthly | 2 of 5 engines accurate; 3 confuse or doubt | 5 of 5 describe the right product |
| Indexed pages | Search Console / Bing WMT | 0 known | all sitemap URLs |
| Organic clicks and impressions | Search Console | none | rising week on week for "DOORS alternative", "ReqIF tool", "self-hosted requirements management" |
| AI crawler fetches | nginx access log grouped by bot user agent (the log format already records it) | unknown | GPTBot, OAI-SearchBot, ClaudeBot, PerplexityBot all fetching weekly |
| AI referrals | referrer `chatgpt.com`, `perplexity.ai`, `gemini.google.com`, `copilot.microsoft.com` | unknown | non-zero |

Expect retrieval-based engines (Perplexity, ChatGPT search, Google AI Mode)
to move first, within a few weeks of indexing. Answers from model memory
will lag by one or more model releases and depend almost entirely on
Phase 3.

## 8. Order of work

| # | Item | Who | Effort |
|---|---|---|---|
| 1 | Decisions 1–3 | maintainer | a conversation |
| 2 | GitHub description, homepage, topics, README opener | maintainer (or agent with repo admin) | 10 min |
| 3 | robots / sitemap / llms.txt, canonical host, og:image (4.1, 4.3); then set `OPENV_SITE_URL=https://openv.app` on the production frontend | repo PR, then maintainer | done in this PR |
| 4 | Pre-render public pages with per-route meta and JSON-LD (4.2, 4.3) | repo PR | done in this PR |
| 5 | Search Console and Bing Webmaster Tools, sitemap submit | maintainer | 1 hour |
| 6 | `/capabilities` from the OpenV Platform project, `/self-hosted…`, `/reqif`, FAQ additions | repo PRs | 1 week |
| 7 | Traceability, verification-evidence, baselines, safety-critical pages | repo PRs | 1–2 weeks |
| 8 | `/compare` hub and per-competitor pages | repo PRs | 1 week |
| 9 | Listings, listicle outreach, LinkedIn, Wikidata, Show HN | maintainer | ongoing |
| 10 | Monthly re-scan and the table in section 7 | maintainer | monthly |

Items 3 and 4 are the unlock: until they ship, nothing in 6–9 can be read
by a crawler, and outreach would send people to a page the engines see as
blank.
