# OpenV — search and AI visibility assessment (2026-10-13)

Scope: how openv.app shows up in web search and in AI assistant answers.
Evidence: a free Conviora "AI Visibility" scan run 2026-10-13 (United States,
English; the shared report cannot be exported, so its findings are
transcribed here), plus direct checks of openv.app, the Railway frontend host
and the public GitHub repository on the same day. Companion to
`docs/plans/search-and-ai-visibility.md`.

**Headline: OpenV is invisible to both search engines and AI assistants.**
It was recommended in 0 of 60 unbranded AI answers, cited 0 times, and the
site serves crawlers nothing but an empty JavaScript shell. When an assistant
is asked about OpenV by name, several describe a *different* product.

## 1. The scan

12 buyer questions × 5 engines (ChatGPT, Gemini, Claude, Perplexity, Google
AI Mode) = 60 scored answers, plus 3 branded questions × 5 engines = 15
diagnostic answers that do not count toward the score.

| Measure | OpenV |
|---|---|
| AI Visibility score | 0 / 100 |
| Presence in unbranded answers | 0 % (0 of 60) |
| Recommendation rate | 0 % |
| Citation rate (openv.app cited) | 0 % |
| Branded answers that name OpenV | 15 of 15 |

### The 12 buyer questions (all 0 % for OpenV)

| Intent | Question |
|---|---|
| Recommendation | Best requirements management tools for systems engineering teams that need end-to-end traceability and verification evidence |
| Recommendation | Requirements and ALM platforms best for regulated engineering organisations that need baselines, version history and audit-ready reports |
| Recommendation | Best self-hosted or open-source requirements management software for a safety-critical engineering organisation |
| Category discovery | What to look for in a requirements management / ALM platform for safety-critical products |
| Category discovery | Types of software for managing engineering requirements, traceability, verification and validation |
| Category discovery | How modern engineering teams manage requirements, design links, hazards, test cases and audit evidence in one system |
| Alternative | Modern alternatives to traditional requirements software that keep ReqIF exchange and audit history |
| Alternative | Alternatives to expensive legacy requirements tools for rigorous traceability and safety-critical verification |
| Comparison | Best combination of ReqIF interoperability, test traceability, self-hosting and lower total cost |
| Comparison | Open-source requirements tools vs traditional enterprise platforms for traceability, baselines and compliance evidence |
| Use case | Software to identify gaps between system needs, requirements, design items, hazards and verification tests |
| Use case | How a systems engineering team records test runs, tracks verification status and generates audit-ready compliance reports |

Every one of these is a question OpenV answers well in the product. The gap
is entirely in discoverability, not in fit.

### Who is recommended instead (presence across the 60 answers)

| Brand | Presence |
|---|---|
| Jama Connect | 82 % |
| Siemens Polarion ALM | 62 % |
| IBM DOORS Next | 58 % |
| Codebeamer (incl. "PTC Codebeamer" listed separately at 27 %) | 42 % |
| ReqView | 42 % |
| Visure Requirements ALM | 20 % |
| Perforce Helix ALM, Modern Requirements4DevOps | 12 % each |
| StrictDoc, Tuleap, Jira + add-ons | 7 % each |
| Doorstop, OpenProject | 5 % each |
| OSRMT, Eclipse RMF/ProR, rmToo, Redmine, OpenMBEE, Matrix Req | 2–3 % each |

93 brands appeared in total. The open-source/self-hosted question was
answered with Tuleap, OpenProject, Redmine, StrictDoc, Doorstop, ReqView,
OSRMT, rmToo and Eclipse RMF: small projects whose only advantage is that
they have crawlable documentation and appear in third-party lists.

### Where the engines got their answers (top cited domains)

| Domain | Citations | Kind |
|---|---|---|
| ones.com | 67 | vendor blog listicles ("best requirements tools 2026") |
| jamasoftware.com | 47 | vendor site |
| ibm.com | 26 | vendor site |
| ptc.com | 24 | vendor site |
| matrixone.health | 20 | listicle |
| thenextweb.com | 13 | press |
| help.perforce.com, support.ptc.com, support.jamasoftware.com | 7–11 each | vendor docs |
| modernrequirements.com, siemens.com, visuresolutions.com, reqview.com | 7–10 each | vendor sites |
| thedigitalprojectmanager.com, thectoclub.com, titanapps.io, worldmetrics.org, gitnux.org | 2–10 each | review/listicle sites |
| strictdoc.readthedocs.io, doorstop.readthedocs.io, docs.tuleap.com | 3–8 each | open-source docs sites |
| github.com | 7 | repositories |
| swehb.nasa.gov, nasa.gov, fda.gov | 2–5 each | standards and guidance |
| reddit.com, hackernoon.com, infoq.com | 2–4 each | community and press |

Three kinds of source carry the answers: **comparison listicles**, **vendor
documentation** and **open-source docs/repositories**. OpenV is in none of
them.

### Branded diagnostics: what the engines say when asked about OpenV by name

The engines all *mention* OpenV when named, but the content is unreliable:

- **Entity confusion.** Claude twice said it could not identify OpenV as a
  requirements tool and offered OVL, OVM, OpenVera and OpenJML instead.
  Other answers described OpenV as a "verification-kernel-style platform"
  for formal verification, as "generative engineering … 3D inspection", and
  as "an experimental open-source proof of concept for automated
  design-to-verification loops, signed evidence, geometry checks" and
  "concept-to-CAD". None of that is this product.
- **Unverifiable.** ChatGPT and Claude called OpenV's self-hosting, ReqIF,
  AI and audit-trail claims "unverified" and recommended Polarion,
  Codebeamer, Jama and DOORS Next instead. One answer classed it
  "unqualified pending evidence" and advised a pilot.
- **Correct when grounded.** Gemini and Google AI Mode, which cited
  openv.app, described it accurately: "a lightweight open-source alternative
  to IBM DOORS, Siemens Polarion and Jama Connect … Docker-based
  self-hosting, ReqIF interchange, bring-your-own-AI and human-in-the-loop
  workflows, audit-oriented traceability and V&V evidence", and
  "self-hostable, zero-seat-cap … modern ReqIF support, integrated AI
  agents, transparent audit trails".

So an engine that can read the site gets OpenV right; the others fall back
to guessing from the name.

## 2. Direct checks (2026-10-13)

| Check | Finding |
|---|---|
| `GET https://openv.app/robots.txt` | 200, `content-type: text/html`, the SPA's `index.html`. No robots file exists. |
| `GET /sitemap.xml`, `/llms.txt` | Same: the SPA shell. No sitemap. |
| `GET /`, `/pricing`, `/faq`, `/how-it-works`, `/manual` | All return the identical shell: `<noscript>You need to enable JavaScript…</noscript><div id="root"></div>`. Every public page has the same `<title>` and description. |
| Body text a non-JS crawler sees | None. The real copy lives in `frontend/src/site/content.ts`, `frontend/src/landing/content.ts` and `frontend/src/manual/chapters/*.ts` and only exists after React runs. |
| Canonical tag | None. |
| Structured data (JSON-LD) | None. |
| `openv-frontend-production-ddb0.up.railway.app` | Serves an identical, indexable copy of the site (no redirect, no `X-Robots-Tag`). `og:image` and `twitter:image` in `frontend/index.html` point at this host, not openv.app. |
| `www.openv.app` | 301 to openv.app (correct). |
| AI crawler access | All of GPTBot, OAI-SearchBot, ChatGPT-User, ClaudeBot, Claude-SearchBot, Claude-User, PerplexityBot, Perplexity-User, Googlebot, Google-Extended are allowed by default. Access is not the problem; there is nothing to read. |
| Web search for "OpenV requirements management" | No result for OpenV; results are ReqCycle, OSEE, OSRMT, Doorstop, OpenProject. |
| GitHub `desktopmachineshop/OpenV` | Public, 1 star, **no description, no homepage URL, no topics**; licence detected as "Other / NOASSERTION". |
| Licence vs copy | `LICENSE` is the Elastic License 2.0, which is source-available, not OSI open source. `index.html`, the README and the site say "open-source". Engines and reviewers check this, and the mismatch feeds the "unverified" verdicts. |

## 3. What is already in our favour

- The product genuinely answers all 12 buyer questions: traceability,
  hazards, V&V runs and evidence, baselines, audit trail, ReqIF import and
  export, self-hosting, data export in six formats, AI agents.
- The marketing and manual copy already exists and is good, factual and
  plain-English (`frontend/src/site/content.ts` FAQ: data export, ReqIF with
  DOORS/Polarion, baselines, self-hosting, AI data handling). It only needs
  to reach the HTML.
- The OpenV Platform project holds every requirement with its verification
  status. A public capability matrix can be generated from evidence, not
  written as marketing.
- When an engine *can* read openv.app (Gemini, Google AI Mode), it
  describes OpenV correctly and favourably.
