# Website refresh plan

Status: draft for the maintainer, 2026-11-13. Nothing here is built.

Starts once the UI changes land on master (the open one today is
[#789](https://github.com/desktopmachineshop/OpenV/pull/789), the Timeline's
level of detail). Everything is recorded and captured after that, so no
video or screenshot shows a screen that has already changed.

## 1. What is asked for

1. Industries, by sector.
2. A getting-started video: one project, start to finish.
3. New short feature videos to replace the five there now.
4. A full feature walkthrough with screenshots.
5. Plainer wording everywhere.
6. Better SEO everywhere.

## 2. Where the site is today

- **The code.** openv.app's site is `site/frontend/` in `openv-hosted`, laid
  over OpenV's `frontend/`. OpenV still carries its own copy in
  `frontend/src/site` (the separation plan removes it later). The two are
  identical today apart from `changelog/releases.json`. This work goes into
  `openv-hosted`; the OpenV copy is left alone unless a test there needs it.
- **The pages.** Front page, `/how-it-works`, `/demos`, `/faq`, `/security`,
  `/privacy`, `/open-source`, `/customers`, `/white-papers`, `/changelog`,
  `/compare` and five comparison pages, seven topic pages and `/glossary`.
  There is no industries page and no features page. `/safety-critical` is
  the nearest thing to a sector page.
- **The videos.** Five mp4s in OpenV's `frontend/public/videos`, 1280×720,
  with a poster each. Three desktop, two phone. The first is titled "The
  requirements module in five minutes", is listed as 3 minutes, and runs
  1:59. None of them shows the plan, the Timeline or All projects, which is
  most of what has shipped since.
- **How the videos were made is not in the repository.** There is no
  recording script, narration text or build step for them in either
  repository, so they cannot be re-recorded when the UI changes. That is
  the first thing to fix (section 4).
- **Screenshots.** Three in total: one desktop, two phone.
- **SEO plumbing is in place.** Pre-rendered pages, robots, sitemap,
  `llms.txt`, canonical links, JSON-LD on `/`, `/faq` and the topic pages,
  and an IndexNow ping after each production deploy. Still open from
  `search-and-ai-visibility.md`: `/capabilities`, two comparison pages
  (Codebeamer, StrictDoc and Doorstop), Search Console and Bing Webmaster
  Tools, and all of Phase 3.
- **The wording.** I searched the copy for the stock phrases (seamless,
  powerful, unlock, streamline and the rest) and for em dashes. There is
  one em dash and none of the phrases. So the "AI style" is in the shape of
  the sentences, not the vocabulary: long lists of nouns, a slogan with a
  twist ("DOORS-grade rigour without the DOORS-grade licence",
  "Programmes, not just projects"), three-item runs, and every card built
  the same way. Section 8 is written for that.

## 3. Order of work

Six pull requests in `openv-hosted`, plus one in OpenV for the recording
tooling. Each is shippable alone.

| # | Pull request | Depends on |
|---|---|---|
| 1 | Recording and screenshot tooling, and the sample project | UI landed |
| 2 | Wording pass over the existing pages | none, can start now |
| 3 | `/features`: the walkthrough with screenshots | 1 |
| 4 | Videos: getting started, the short set, the new `/demos` | 1 |
| 5 | `/industries` and the sector pages | 2 (so they are written in the new voice) |
| 6 | SEO sweep over everything, old and new | 3, 4, 5 |

2 can be written tonight before the UI lands, because it touches no
screens. 1 is the long pole.

None of this reaches openv.app until a promotion, which is the
maintainer's call. Staging shows it as soon as it merges.

## 4. Recording tooling (pull request 1)

One script produces every video and every screenshot, so a UI change means
running it again, not a day of re-recording.

- **Where.** `scripts/site-media/` in OpenV, beside the Playwright journeys
  it borrows from. It writes into `openv-hosted`'s
  `site/frontend/public/videos` and `public/screenshots`.
- **What it runs against.** A local server built from master with a seeded
  sample project, not production and not the platform's own requirements.
  The same seed every time, so two recordings a month apart match.
- **The sample project.** One product, small enough to follow and deep
  enough to show everything: about 8 user needs, 25 requirements, a few
  design items and hazards, 12 test cases, two test runs (one with a
  failure), two baselines, a plan with milestones, one child project for a
  supplier. Kept as a JSON export in the repository and imported at the
  start of each recording.
- **Video.** Playwright drives Chromium with `recordVideo`, 1920×1080 for
  desktop and an iPhone profile for phone. Each video is a script of steps;
  each step carries its narration line, so the picture waits for the voice
  and not the other way round. ffmpeg joins the pieces, adds the voice, and
  writes the mp4, a poster and a WebVTT caption file from the same lines.
- **Screenshots.** The same steps, with a `shot('name')` call where a
  screenshot is wanted. Light theme, 2× scale, written as WebP with the
  width and height recorded so the page reserves the space.
- **Narration.** The lines live in the repository as text. The voice is an
  open question (section 10): this container has ffmpeg and no speech
  engine, and nothing records which voice the current five use.
- **A check.** `make site-media-check` runs the scripts without recording
  and fails if a step no longer finds its button. That tells us when a UI
  change has made a video stale.

## 5. The videos (pull request 4)

### Getting started: one project, start to finish

About 9 minutes, in chapters a viewer can jump between. The chapters are
also cut as separate clips for the manual.

| Chapter | What happens | About |
|---|---|---|
| 1 | Create an account, a workspace and a project from a template | 0:50 |
| 2 | Write user needs; the guided definition asks its questions | 1:10 |
| 3 | Derive requirements, link them to needs, fix a wording finding | 1:20 |
| 4 | Design items and hazards; the tree and the traceability matrix | 1:00 |
| 5 | Test cases, a test run with one failure, coverage and the gap list | 1:20 |
| 6 | The plan: milestones, the Timeline, All projects | 1:00 |
| 7 | Review and approve; capture a baseline | 0:50 |
| 8 | Change a requirement; suspect links, impact, compare two baselines | 1:00 |
| 9 | Download the specification as a PDF; share a read-only link | 0:30 |

It sits at the top of `/demos` and on the front page in place of the
five-poster strip. The manual's getting-started chapter links to it.

### Short feature videos

60 to 90 seconds each, one subject each, and the title says what is in it.
They replace the current five.

1. Requirements and the project tree
2. Traceability: links, the matrix, impact
3. Verification: runs, coverage, gaps
4. Baselines and comparing them
5. Documents and export (PDF, Word, ReqIF)
6. The plan and the Timeline
7. Agents and review of what they propose
8. Stakeholder interviews
9. On a phone: review and approve
10. On a phone: the plan and agents

`/demos` becomes: getting started first, then the short set grouped as
desktop and phone, each with its transcript under it (folded shut). The
old files stay on disk for one release so old links still play, then go.

The durations on the page come from the files, read at build time, so the
"five minutes" that is really two cannot happen again.

## 6. The feature walkthrough (pull request 3)

A new page, `/features`. One long page, a section per area, each with two
or three screenshots and a paragraph that says what the screen is for and
what to do on it. A contents list at the top; every section has its own
anchor so other pages and search results can land on it.

Sections, in the order a project meets them: projects and the tree,
requirements and attributes, wording checks, traceability, hazards,
verification, the plan, review and approval, baselines, documents and
export, import and ReqIF, agents, automations, interviews, programmes and
child projects, notifications, phone, workspace and members, self-hosting.

Each section ends with a link to its manual topic and, where one exists,
its short video and its topic page. The front page's "What it does" cards
link into the sections.

It replaces nothing: `/how-it-works` stays as the explanation of the model,
`/features` is the tour of the screens. The `/capabilities` table in the
visibility plan (generated from the OpenV Platform project) is still its
own piece of work and is not in this plan.

## 7. Industries by sector (pull request 5)

A hub at `/industries` and a page per sector at `/industries/<sector>`,
with a section on the front page that links to each. They use the topic
page layout and its rules: open with the answer, close with what OpenV
does not do, link into the manual, carry a date.

Proposed sectors, to be confirmed (section 10):

| Sector | What that reader works to |
|---|---|
| Aerospace and defence | ARP4754A, DO-178C, DO-254 |
| Automotive | ISO 26262, ASPICE |
| Medical devices | IEC 62304, ISO 13485, ISO 14971, FDA design controls |
| Rail | EN 50126, EN 50128, EN 50129 |
| Industrial and machinery | IEC 61508, ISO 13849 |
| Space | ECSS, NASA systems engineering |
| Hardware start-ups and product teams | no standard yet, wants the habit |

Each page says, for that sector: the artifacts those teams keep and how
they map to OpenV's types; the trace chain an auditor asks for and where
OpenV shows it; which evidence OpenV holds and which it does not; a
screenshot or two from a sample project in that sector's vocabulary; and
the plain statement the site already makes on `/safety-critical`: no
certification, no tool qualification kit, no claim of compliance with any
standard. That statement is on every sector page, not just the hub.

`/safety-critical` stays and links to the hub. A starter template per
sector inside the app would make these pages stronger, but it is product
work and a new feature, so it is left out of this plan.

## 8. Wording (pull request 2, and the rule for 3 to 5)

A short style sheet goes in `openv-hosted` as `site/STYLE.md`, and every
string on the site is rewritten against it: `landing/content.ts`,
`site/content.ts`, the topic and comparison data, the FAQ, How it works,
the page summaries in `crawl.ts` (which feed the meta descriptions and
`llms.txt`), `index.html`, and the video narration.

The rules, drawn from what the copy does now:

- Say what the reader can do, in a sentence with a verb. Not a list of
  nouns. "Link each requirement to the test that proves it" rather than
  "typed links, version history, suspect-link review and immutable
  baselines".
- No slogans built on a twist. "DOORS-grade rigour without the DOORS-grade
  licence" becomes what it means: what it costs and what it does.
- No "not just X" titles. Say the thing.
- Lists are as long as the facts. No padding to three.
- Cards on a page do not all have the same shape and length.
- Words the reader uses: "test", not "verification activity", unless the
  page is about the term.
- One idea per sentence where the sentence is over about 25 words.
- Keep what is already right: limits stated as plainly as features, no
  claimed standards, dates on pages.

The front page and the pricing section are the maintainer's voice, so those
two come as a before-and-after for approval before they merge. The rest is
reviewed in the pull request.

A test keeps it from drifting back: a short list of patterns
(`not just`, `-grade`, the stock adjectives) that fails the build when one
appears in site copy, the way `topics.test.ts` already guards the topic
pages.

## 9. SEO (pull request 6, and a checklist for 3 to 5)

Every new page meets these when it is built; the sweep applies the same
list to the pages that exist.

**Each page**
- A title under 60 characters and a description under 155, both written for
  the search someone would type, no two pages alike. A test enforces the
  lengths and the uniqueness.
- One H1 that contains the subject. H2s that read as the questions people
  ask.
- A `BreadcrumbList`, and breadcrumbs on screen for sector, comparison and
  topic pages.
- Its own share image. Today every page shares one `og.png`.
- A real `lastmod` in the sitemap, taken from the page's "Updated" date.

**Videos and images**
- `VideoObject` JSON-LD for each video: name, description, duration,
  thumbnail, upload date and the transcript.
- A video sitemap, and the transcript in the page's HTML so the words are
  indexed.
- Captions on every video (the `.vtt` from section 4).
- Screenshots as WebP with width, height, lazy loading below the fold, and
  alt text that describes the screen.
- File names that say what the picture is.

**Links between pages**
- Each sector page links to the topics and comparisons that matter to it,
  and back. `/features` sections link to topics, videos and the manual.
  A test fails on a public page nothing links to.
- The footer lists industries and features.

**Speed**
- Measure the front page, `/features` and `/demos` with Lighthouse on a
  phone profile before and after. `/features` will be heavy with images;
  the budget is a largest contentful paint under 2.5 s on that profile.
- Videos keep `preload="metadata"` and load nothing until played.

**Left from the visibility plan, picked up here**
- `/compare/codebeamer` and `/compare/strictdoc-doorstop`.
- `HowTo` JSON-LD on the getting-started section, matching its chapters.
- `SoftwareApplication` gains `screenshot` and `featureList` once
  `/features` exists.

**For the maintainer (cannot be done from a session)**
- The GitHub repository description has lost its first letter. It reads
  "equirements management and V&V platform". It is the first line engines
  read about the project.
- Search Console and Bing Webmaster Tools: confirm both are verified and
  the sitemap is submitted. After the release, request indexing for
  `/industries`, `/features` and `/demos`.
- Re-run the Conviora scan a month after release, against the 0/100
  baseline.

## 10. Decisions needed

1. **The sectors.** Are the seven in section 7 the right ones, and is there
   one to lead with? If there are customers or conversations in a
   particular sector, that page gets written first and best.
2. **The narration voice.** What produced the voice on the current five?
   If it is a service, this session needs a key for it. If it does not
   matter, I pick an open speech model that runs offline and is the same
   every time.
3. **The sample product.** Something generic (a ventilator, a drone, a
   brake controller) or something from Desktop Machine Shop's own world?
   One product is used for the getting-started video, the short videos and
   every `/features` screenshot.
4. **Where the video files live.** They are in OpenV's
   `frontend/public/videos` now. Ten 1080p videos and a nine-minute one is
   roughly 150 to 250 MB in git. Options: keep them in `openv-hosted` and
   accept the size, or put them in a bucket behind the site's own host
   name (the content-security policy stays same-origin). I lean to the
   bucket.
5. **The old five videos.** Keep for one release and then delete, as
   proposed, or delete at once?

## 11. What each pull request also carries

- `openv-hosted` has no release notes file; the OpenV pull request for the
  tooling is labelled `no-release-notes` and `no-docs-change`, since no
  workspace member sees it.
- The OpenV Platform project gets requirements under the existing
  Discoverability need for the sector pages, the feature page and video
  captions, a test case each, and a test run when they pass. A baseline
  after the last one merges.
- The manual's getting-started topic links to the new video. That is a
  manual change, made in the chapters and synced.
- `e2e/tests/landing.spec.ts` and the prerender, crawl and route tests are
  extended for each new path.
- Nothing here asks for a promotion.
