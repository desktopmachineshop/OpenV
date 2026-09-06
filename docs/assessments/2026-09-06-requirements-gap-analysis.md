# OpenV Platform — requirements gap analysis (2026-09-06)

Scope: the live **OpenV Platform** project (64 requirements, 11 needs,
16 design items, 33 test cases, baseline *2026-09-04 One download, chosen
and filtered*) challenged in its entirety against the feature set actually
shipped on `master` at `cfabf06` (Railway `release` at the same head).

Method: a route-by-route, package-by-package inventory of the codebase
(≈265 HTTP routes, 36 domain packages, 23 schema migrations, 21 SPA views,
5 Playwright journeys, 29 MCP tools) was compared with every requirement
body, every need, and every test case's evidence claim. Findings are graded:

| Grade | Meaning |
|---|---|
| **G1 — shipped, unspecified** | A capability exists with no requirement stating it. It cannot be traced, baselined or verified. |
| **G2 — specified, stale** | The requirement text no longer matches the platform. |
| **G3 — specified, unverified** | A requirement whose test case does not exist, does not do what it claims, or whose verification is missing. |
| **G4 — specified, not built** | A requirement stated as binding for something that is not implemented. |
| **G5 — need under-served** | A stakeholder need whose derived requirements do not cover how the platform actually serves it. |

Every finding below has been recorded in the live project (new draft
artifacts, edits, comments and links) so the project itself carries the
result; this file is the off-instance record. See "How the result was
recorded" at the end.

## 1. Summary

| Grade | Count | Disposition |
|---|---|---|
| G1 shipped, unspecified | 19 capability groups | 25 new draft requirements (REQ-65 … REQ-89) + 14 new test cases (TC-34 … TC-47) added to the project |
| G2 stale | 6 | 4 requirement/design bodies updated; 2 tensions left as comments for the maintainer |
| G3 unverified | 18 requirements without a verifying test case; 1 test case overclaiming | Recorded as comments; 3 new test cases cover 4 of them |
| G4 not built | 2 (REQ-44 remote MCP; macOS connector in REQ-32) | Commented; REQ-44 left draft as a roadmap requirement |
| G5 need under-served | 3 needs (NEED-5, NEED-9, NEED-7 audit) | New requirements derive from them |

The requirement set is **strong where the last four weeks of work landed**
(figures, downloads, quality rules, runner leasing, agent seeds) and **thin
where the platform grew earlier and quietly**: the review workflow, baseline
comparison, impact analysis, search, notifications, budgets, run resilience,
import/templates, observability and backups are all shipped and untraced.
Security is specified by five requirements and verified by six test cases; the
companion security assessment records what those leave uncovered.

## 2. G1 — shipped but unspecified

Each row is a capability found in the code with no requirement stating it.
The "recorded as" column is the artifact created in the live project.

| # | Capability (evidence) | Under | Recorded as |
|---|---|---|---|
| 1 | **Artifact status workflow** — draft → in_review → approved → superseded, validated transitions, `PUT /artifacts/{id}/status`; approval clears link suspicion (`internal/domain/artifacts/status.go`) | HDG-3 | REQ-65 |
| 2 | **Suspect links and the review queue** — editing either end of a link flags it suspect until a reviewer confirms it (`PUT /links/{id}/confirm`); `/projects/{id}/review-queue` lists suspect links and in-review artifacts (migration 5, `ReviewQueue.tsx`, `e2e/tests/review-queue.spec.ts`) | HDG-3 | REQ-66; TC-34 |
| 3 | **Baseline comparison** — `GET /baselines/{id}/diff` against another baseline or live, Added/Modified/Removed with field flags (`internal/domain/baselines/diff.go`, `BaselineCompare.tsx`, `e2e/tests/baseline-diff.spec.ts`) | HDG-3 | REQ-67; TC-35 |
| 4 | **Impact analysis** — upstream/downstream/both traversal over links, depth-capped at 25 (`internal/domain/vv/impact.go`, `ImpactView.tsx`) | HDG-5 | REQ-73; TC-45 |
| 5 | **Search** — trigram search across a workspace (`/api/v1/search`, migration 9), opt-in semantic search and duplicate-candidate detection over pgvector embeddings, env-gated and off by default (`internal/domain/embeddings`, migration 16, `/projects/{id}/duplicates`, `/reindex-embeddings`) | HDG-3 | REQ-68, REQ-69; TC-36, TC-37 |
| 6 | **Typed attribute definitions** — org- and project-scoped definitions (enum/date/number/text) validated into artifact attributes (`internal/domain/attributes`, migration 15) | HDG-3 | REQ-70; TC-38 |
| 7 | **Project import and templates** — JSON and ReqIF import with remapped ids, drafts stamped; save-as-template and create-from-template (`internal/domain/exports/reqif_import.go`, `internal/domain/templates`) | HDG-3 | REQ-71, REQ-72; TC-39 |
| 8 | **Notifications** — in-app rows, live SSE push, per-user email opt-out, opt-in SMTP for high-signal types (`internal/notify`, migrations 6 and 13) | HDG-7 | REQ-78; TC-44 |
| 9 | **Budgets, plans and limits** — monthly budget per workspace with 80 %/100 % admin alerts, optional launch enforcement, `free`/`team` plan defaults for runner limits, month-to-date usage endpoint (`internal/notify/budgets.go`, `internal/domain/orgs/limits.go`) | HDG-6 | REQ-76, REQ-77; TC-44, TC-47 |
| 10 | **Run resilience** — failure taxonomy with a retryable subset, automatic retry chain capped by `OPENV_RUN_MAX_ATTEMPTS`, manual retry with provenance, stale-heartbeat reaper, cancel and release semantics, reproducibility snapshot per run (migrations 3, 12, 14; `internal/domain/agentruns/service_test.go`) | HDG-8 | REQ-84, REQ-85; TC-40 |
| 11 | **Proposal review at scale** — bulk approve/reject, 100-proposal cap per run, temporary ref tokens so one run can propose an artifact and a link to it (`/proposals/bulk`, migration 17) | HDG-7 | REQ-79; TC-40, TC-41 |
| 12 | **Crew templates, clone, export/import** — two built-in templates, crew clone, crew export/import across workspaces (`internal/domain/crewtemplates`, `/crews/{id}/export`, `/crews/import`) | HDG-7 | REQ-80; TC-42 |
| 13 | **Agent-executed test runs** — "Run N with agent" builds an instruction naming automated cases only; `manual`/`physical` cases are withheld and any agent result for them is refused (403); agent results carry a marker. "Draft test cases" launches the seeded test-case-author agent in proposal mode (`docs/agents.md`, `/test-runs/{id}/agent-run`, `/projects/{id}/draft-test-cases`) | HDG-5 | REQ-74, REQ-75; TC-45 |
| 14 | **Kanban-driven runs** — moving a card into an agent column enqueues a run; run progress lands on the card's activity (`internal/orchestration/hooks.go`) | HDG-7 | REQ-81 |
| 15 | **Agent configuration surface** — provider, model from a catalog or custom, reasoning effort, `allowed_tools` allowlist, `max_turns`, timeouts; provider settings with worker-reported detection (`internal/domain/providers`, `docs/agents.md`) | HDG-7 | REQ-82 |
| 16 | **AI context surface** — stable refs on every artifact, `get_project_map` (and `/projects/{id}/ai-map`), release-stamped maps by baseline, `get_context` bundles (`internal/api/ai_map.go`, `internal/mcp/context_bundle.go`) | HDG-7 | REQ-83; TC-46 |
| 17 | **Repository connections** — per-project connections, per-member local checkout paths, leased runners cloning by URL, hosted runners refusing (`internal/domain/repoconns`, `internal/runner/workspace.go`) | HDG-8 | REQ-86 (decomposed from REQ-25) |
| 18 | **Observability** — Prometheus `/metrics` with bounded cardinality and an optional bearer gate, structured request log annotated with the resolved actor, `/health` (`internal/metrics`, `internal/api/requestlog.go`) | HDG-17 Operations (new, under HDG-11) | REQ-87; TC-47 |
| 19 | **Backup, restore and schema migration** — `make backup`/`restore`, opt-in scheduled sidecar with retention, numbered migration ledger applied once under a boot advisory lock (`scripts/backup.sh`, `internal/persistence/postgres/migrations.go`) | HDG-17 Operations | REQ-88, REQ-89; TC-47 |

Also shipped and unspecified, recorded as one requirement each or folded into
the rows above: generic OIDC sign-in (folded into REQ-18), interview persona
linking and invite expiry/revocation (folded into REQ-8), the in-app manual
and contextual help, the global search box, theme switching, workspace member
self-removal, and the Usage tab.

## 3. G2 — stale requirements

| Ref | What is stale | Action taken |
|---|---|---|
| REQ-18 Authentication | Says email/password and Google only. The platform also offers **generic OIDC** (`OPENV_OIDC_*`, `oidc_handlers.go`) and refuses cross-provider sign-in for one email (`users_test.go`). | Body updated. |
| REQ-2 Typed artifacts | Lists eight types; the catalog has nine (**other**). Says "free-form attributes"; attributes can now be typed and validated (see G1 #6). | Body updated to name all nine types and reference typed definitions. |
| REQ-3 Semantically constrained links | Lists six link types; the rule set enforces eight (**impacts**, **relates-to** are unconstrained-by-type and bidirectional respectively). | Body updated. |
| DES-1 MCP stdio tool server | Says 27 tools; `internal/mcp/tools.go` exports 29. | Body updated. |
| REQ-21 Proposal-gated writes | "**All** agent-initiated changes shall be diverted into proposals" — but `write_mode: direct` exists by design, and a workspace runner key carries no gating (REQ-42 says so). The absolute wording contradicts two other requirements. | Comment left; recommended rewording: proposal mode is the default, direct mode is an explicit per-agent choice by a workspace admin. |
| REQ-20 Lean agent context | "shall not feed agents large prompt contexts or requirement documents" — REQ-41 and REQ-49 deliberately place the product profile, wizard state, transcript and the artifact on screen in the assistant prompt, and the V&V Assistant carries WebSearch/WebFetch. Not a contradiction in practice (the content is bounded and fenced) but the text reads as one. | Comment left; recommended rewording bounds what a prompt may carry rather than forbidding content outright. |
| REQ-53 / REQ-64 | Both specify the side-panel states; REQ-64 restates part of REQ-53 with a differing rule (a hidden panel opens on click, not hover). | Comment left; REQ-64 should `decomposes-to` from REQ-53 or the two should merge. |
| `docs/operations.md` | Says hosted-runner containers run with **no resource caps**; `docs/agents.md`, `docker-compose.prod.yml` and `internal/hosting/provisioner.go` apply memory/CPU caps from plan limits. | Doc fixed in this change. |

## 4. G3 — specified but unverified

Before this work `get_vv_gaps` reported **no gaps at all** while
`get_vv_coverage` listed nine requirements as *uncovered* (REQ-18, REQ-25,
REQ-35, REQ-36, REQ-44, REQ-51, REQ-53, REQ-54, REQ-58). The gap tool flags a
missing test case only for requirements whose method is `test`; a
`demonstration` or `analysis` requirement with no evidence and no
verification status is silent there while the coverage rollup calls it
uncovered. That disagreement between two V&V views of the same project is
itself a platform finding (REQ-12 says gaps are "actionable"; a requirement
the rollup calls uncovered should appear in them). Beyond it, two things were
hidden:

**Requirements with no verifying test case at all** (18): REQ-2, REQ-4,
REQ-5, REQ-13, REQ-14, REQ-18, REQ-19, REQ-20, REQ-22, REQ-23, REQ-24,
REQ-25, REQ-26, REQ-35, REQ-44, REQ-51, REQ-53, REQ-54. Most are
`demonstration`, which is legitimate for UI behaviour, but REQ-18, REQ-25
and REQ-44 carry no `verification_status` either, and REQ-26 (must,
verified) has only a design item behind it. New test cases now cover
REQ-5 and REQ-14 (TC-36 smoke journey), REQ-18 (TC-43) and REQ-22 (TC-42).

**A test case that overclaims**: TC-8 *Link rule validation suite* says
"Automated suite: each link type accepts its allowed from/to artifact types
and rejects all others". `internal/domain/links` has **no test files**; the
only coverage is indirect, in `internal/api/proposal_appliers_test.go`.
REQ-3 (must, method test) therefore has no direct evidence. Comment left on
TC-8.

**Evidence this session could produce**: the full Go suite passes
(`go test ./cmd/... ./internal/...`, 41 packages), which is the standing
evidence behind TC-1, TC-12, TC-14, TC-16, TC-17, TC-18, TC-20, TC-23,
TC-27, TC-28, TC-29 (unit half), TC-30 (automated half), TC-31 and TC-32.
Recorded as test run *2026-09-06 Assessment evidence* (29 results: 28 pass,
1 fail — the production hardening probe, see the security assessment). Not exercised here:
the Postgres-backed suites (skip without `OPENV_TEST_DATABASE_URL`), the
Playwright journeys and the client Jest suites (no built frontend in the
session), and every live check (TC-2, TC-5, TC-6, TC-7, TC-10, TC-11).

## 5. G4 — specified but not built

- **REQ-44 Remote MCP workspace connector** is written as "shall" but its own
  body ends "Not yet implemented"; DES-12 is "Proposed, not built". It should
  either carry a status the convention recognises (draft is right; it must
  not be approved or given a verification status) or move to a roadmap
  section. No test case exists; none should until it lands.
- **REQ-32 On-demand Agent Connector** promises "a single self-contained
  executable per OS"; `Dockerfile.api` builds Windows and Linux only, and
  `docs/railway.md` says macOS has no prebuilt download. Either the
  requirement names the two supported OSes or macOS is a known gap.

## 6. G5 — needs under-served by their requirements

- **NEED-9 Predictable agent execution** derives only REQ-27 and REQ-28
  (routing). The platform's actual answer to "starts reliably" is the retry
  chain, failure taxonomy and stale-run reaper (G1 #10) — now derived from
  NEED-9.
- **NEED-5 One view of human and AI work** derives only REQ-23. The
  notifications surface and board-driven runs (G1 #8, #14) are the other
  half of that view — now derived from NEED-5.
- **NEED-7 Audit-ready traceability** has no requirement for baseline
  comparison, impact analysis or the review queue (G1 #2, #3, #4) — the
  three features an auditor would actually use — now derived from NEED-7.

## 7. Conventions and hygiene observations

- The project's rule set is ISO 29148 "shall". 30 of the 64 requirements
  are `approved`; 34 are `draft`, including `must` requirements that have
  been verified for weeks (REQ-1, REQ-7, REQ-26, REQ-34, REQ-40, REQ-41…). A
  status pass would let the coverage rollup mean something.
- `priority` is unset on REQ-34 and REQ-35.
- The MoSCoW `priority` attribute and the body keyword are kept separate
  throughout, as the rule set requires.
- Test cases TC-31…TC-33 sit at the project root rather than under HDG-10 /
  HDG-14; moved under HDG-14 as part of this work.

## 8. How the result was recorded in the live project

1. New heading **HDG-16 Assessments** with two description artifacts holding
   this report and the security assessment.
2. New need **NEED-12 Run it myself without an operations team** (the
   operational and security requirements had no need to derive from).
3. New draft requirements REQ-65 … REQ-89 (G1) under the headings named in
   §2 and a new **HDG-17 Operations** heading, each with `derives-from` to
   the need it serves and `verifies` from a new test case (TC-34 … TC-47)
   where a suite exists. REQ-86 also `decomposes-to` from REQ-25.
4. Security: REQ-90 … REQ-100, HAZ-1 … HAZ-16 under **HDG-18 Security
   hazards**, and TC-48 — see the security assessment.
5. Body updates to REQ-2, REQ-3, REQ-18 and DES-1 (G2); comments on REQ-20,
   REQ-21, REQ-32, REQ-44, REQ-53 and TC-8. TC-31 … TC-33 moved under
   HDG-14.
6. Test run **2026-09-06 Assessment evidence** with the Go-suite results and
   the production probe.
7. Baseline **2026-09-06 Requirements and security assessment**.
8. A fresh export under `docs/exports/`.
