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
7. A news section for announcements: features, versions, demos.

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
  the same way. Section 9 is written for that.

## 3. Order of work

Seven pull requests in `openv-hosted`, plus one in OpenV for the recording
tooling. Each is shippable alone.

| # | Pull request | Depends on |
|---|---|---|
| 1 | Recording and screenshot tooling, and the sample project | UI landed |
| 2 | Wording pass over the existing pages | none, can start now |
| 3 | `/features`: the walkthrough with screenshots | 1 |
| 4 | Videos: getting started, the short set, the new `/demos` | 1 |
| 5 | `/industries` and the sector pages | 2 (so they are written in the new voice) |
| 6 | `/news`: the section, its feed, and the first posts | 2 |
| 7 | SEO sweep over everything, old and new | 3, 4, 5, 6 |

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
- **Narration.** The lines live in the repository as text, one per step.
  The voice is an offline speech model, as it was for the current five.
  First choice is Kokoro (open, runs on a CPU, the most natural of the
  small models); Piper is the fallback if Kokoro will not install in a
  session. Model, voice and version are pinned in the script's config so
  every video has the same voice and a later run reproduces it.
- **Editable afterwards.** A video is never one opaque file. Each has a
  folder:
  - `video.yaml`: the steps in order, each with its actions, its narration
    line, and any pause or zoom.
  - `build/steps/NN-name/`: that step's picture (`clip.mp4`) and voice
    (`voice.wav`), kept after the run. Not in git; rebuilt on demand.
  - `overrides/NN-name/`: drop a `voice.wav` here to replace the machine
    voice for one step with a human recording, or a `clip.mp4` to replace
    the picture with one recorded by hand. The build uses an override
    where it finds one and stretches the step to fit.
  - `timeline.json`: written by the build, with each step's start and end
    in the finished video.

  So the three kinds of edit are each one small change. To reword a line,
  edit the text and run `site-media build <video> --only <step>`: only
  that step is re-voiced and the video is rejoined, with captions and
  transcript regenerated. To re-shoot a step after a UI change, the same
  command with `--record`. To cut by hand in an editor, `site-media
  export <video>` writes the step clips, the voice tracks and an edit
  list an editor can open. `scripts/site-media/README.md` says all of this
  for the next person or agent.
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
five old videos and their posters are deleted in the same pull request
that adds the new ones.

### Where the files live

The videos are ordinary files in git today, not Git LFS: neither
repository has LFS set up, and the five mp4s total about 10 MB. Ordinary
git storage on GitHub is never billed. Its limits are 100 MiB per file (a
push is refused), a warning at 50 MiB, and a recommendation to keep a
repository under 1 GB. LFS would be free up to 10 GiB of storage and
10 GiB of downloads a month and metered after that, but nothing here
needs it.

So the videos stay in git for now. Screen recordings compress well: the
current ones run at about 170 kbit/s at 720p. At 1080p the build targets
the nine-minute video under 40 MiB and the whole set under 100 MB, and
fails if a file passes 50 MiB. The one cost is that git keeps every
version, so each full re-recording adds the set's size again.

Long term the videos go on YouTube, where they are wanted anyway. To make
that a small change later: each video's entry carries an optional YouTube
id, and the player shows the YouTube embed when it is set and the local
file when it is not. Moving is then a field per video plus allowing
`youtube-nocookie.com` as a frame source in the content-security policy.
The transcript, captions and `VideoObject` data stay on our pages either
way, since those are what search reads. Uploading is the maintainer's;
the build writes the title, description, chapters and caption file ready
to paste.

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

The sectors:

| Sector | What that reader works to |
|---|---|
| Aerospace and defence | ARP4754A, DO-178C, DO-254 |
| Automotive | ISO 26262, ASPICE |
| Medical devices | IEC 62304, ISO 13485, ISO 14971, FDA design controls |
| Rail | EN 50126, EN 50128, EN 50129 |
| Industrial and machinery | IEC 61508, ISO 13849 |
| Space | ECSS, NASA systems engineering |
| Software and digital products | ISO/IEC/IEEE 29148 and 12207; SOC 2 or ISO 27001 evidence where a customer asks |
| Education and research | no standard: systems engineering courses, capstone projects, student teams (Formula Student, CubeSat, rocketry), research labs |
| Hardware start-ups and product teams | no standard yet, wants the habit |

Each page says, for that sector: the artifacts those teams keep and how
they map to OpenV's types; the trace chain an auditor asks for and where
OpenV shows it; which evidence OpenV holds and which it does not; a
screenshot or two from a sample project in that sector's vocabulary; and
the plain statement the site already makes on `/safety-critical`: no
certification, no tool qualification kit, no claim of compliance with any
standard. That statement is on every sector page, not just the hub.

The software page is for teams whose product is the software, and says
plainly where OpenV sits beside an issue tracker rather than replacing
it; embedded software in a regulated product is covered by that product's
sector. The education page is written for the lecturer and the team
captain: what a student project looks like in OpenV and how a course
marks it.

`/safety-critical` stays and links to the hub. A starter template per
sector inside the app would make these pages stronger, but it is product
work and a new feature, so it is left out of this plan.

## 8. News (pull request 6)

A place to announce things, so the site gains a dated, indexable page each
time something ships and has a reason to be linked to.

- **Pages.** A list at `/news`, newest first, and a page per post at
  `/news/<slug>`. The slug is the title in words and never changes once
  published. The front page shows the latest three, and the footer and
  the top navigation link to the list.
- **What a post is.** One Markdown file per post in
  `site/frontend/src/site/news/`, with a short header: title, date,
  summary, kind, author, and optionally a share image, a video id and the
  version it is about. Publishing is a pull request that adds a file. No
  database and no admin screen.
- **Kinds.** Release, new feature, demo or video, industry, and general
  announcement. The list filters by kind, and each kind has its own
  address (`/news/releases` and so on) so a filtered list can be linked
  and indexed.
- **How it differs from `/changelog`.** The changelog is the complete
  record, every version and every bullet, generated from the release
  notes. News is the selected story: what a feature is for, with a
  screenshot or the video, written to be read by someone who has not used
  OpenV. A release post links to its changelog entry and the changelog
  entry links back.
- **Where posts come from.** At each promotion I draft a post for the
  release from its notes and the manual topics of its new features, with
  screenshots taken by the section 4 tooling, and the maintainer approves
  or edits it before it merges. Not every patch release needs one: a post
  goes out for each minor version and for anything worth a headline in
  between. New videos, new sector pages and new comparison pages each get
  a short post too.
- **Length and voice.** 300 to 700 words, in the section 9 style, with the
  limits stated where there are any. A post that only repeats the release
  bullets adds nothing for search and is not published.
- **For search.** Each post is pre-rendered, in the sitemap with its date,
  in `llms.txt`, and carries `BlogPosting` JSON-LD with headline, date,
  author and image. An Atom feed at `/news/feed.xml` is linked from every
  page's head. Each post links to the feature section, topic page or
  video it is about, and those link back to the latest post about them,
  so news feeds the pages that are meant to rank and does not compete
  with them. The IndexNow ping that already follows a production deploy
  picks up new posts through the sitemap.
- **In the app.** The "What's new" notice a workspace sees after a release
  can link to the post. That is a change to the app, so it is noted here
  and left out of this plan.
- **First posts,** published as the rest of this plan ships: the new
  getting-started video, the feature walkthrough, industries, and one for
  the plan, Timeline and All projects work of the last few releases.
- **Guards.** A test holds every post to a title, a date not in the
  future, a summary within the description length, a unique slug and at
  least one link into the site.

## 9. Wording (pull request 2, and the rule for 3 to 6)

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

## 10. SEO (pull request 7, and a checklist for 3 to 6)

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
- The footer lists industries, features and news.

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

## 11. Decisions

Made, 2026-11-13:

- **Sectors.** The seven first proposed, plus software and education
  (section 7).
- **Narration.** An offline speech model, with every video left editable
  (section 4).
- **Old videos.** Deleted when the new ones go up.
- **Storage.** In git for now, since it is free and nothing breaks;
  YouTube later (section 5).

Still open:

1. **The sample product,** to be chosen with the maintainer. It has to
   break into sub-projects, because the programme features (child
   projects, requirements refined downward, verification rolling up,
   a supplier's project) are part of what the videos show. Candidates:

   | Product | Sub-projects | For | Against |
   |---|---|---|---|
   | Desktop CNC mill | frame and axes, spindle and drive (supplier), controller firmware, control software, guarding and e-stop | Desktop Machine Shop's own ground; hardware, firmware, software and real hazards in one product; anyone can picture it | not a regulated product, so the regulated sectors see an analogy |
   | Delivery drone | airframe, flight controller, battery pack (supplier), ground station app | familiar, clear safety case, software and hardware | aviation readers will hold it to their standards |
   | Infusion pump | pump mechanism, firmware, clinician app, pressure sensor (supplier) | the classic regulated example; hazards are obvious | reads as a medical-device tool to everyone else |
   | CubeSat | structure, power, comms, payload, ground segment | the textbook decomposition; suits education and space | remote from most buyers |

   My pick is the desktop CNC mill for the videos and `/features`, with
   one or two screenshots on each sector page taken from a small project
   in that sector's own vocabulary.
2. **Which sector leads.** If there are customers or conversations in one
   sector, that page is written first.

## 12. What each pull request also carries

- `openv-hosted` has no release notes file; the OpenV pull request for the
  tooling is labelled `no-release-notes` and `no-docs-change`, since no
  workspace member sees it.
- The OpenV Platform project gets requirements under the existing
  Discoverability need for the sector pages, the feature page, news and
  video captions, a test case each, and a test run when they pass. A baseline
  after the last one merges.
- The manual's getting-started topic links to the new video. That is a
  manual change, made in the chapters and synced.
- `e2e/tests/landing.spec.ts` and the prerender, crawl and route tests are
  extended for each new path.
- Nothing here asks for a promotion.
