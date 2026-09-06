# OpenV — security assessment (2026-09-06)

Authorised defensive review of OpenV at `master` `cfabf06` (the same head
Railway `release` is serving), covering both deployment options:

- **Self-hosted** — `docker-compose.yml` + `docker-compose.prod.yml`,
  `Dockerfile.api`, `Dockerfile.worker`, `frontend/Dockerfile.prod` +
  `frontend/nginx.conf`, the backup sidecar.
- **Railway production** — the `OpenV` (API), `OpenV Frontend`, `OpenV
  Runner Pool` and `Postgres` services in the `incredible-solace` project.

Method: the security model claimed in `docs/architecture.md` and
`docs/operations.md` was verified claim by claim in code; every HTTP route
was traced for authentication and authorisation; `govulncheck` and `npm
audit --omit=dev` were run; the full Go suite was run; and the live
production API and frontend were probed with an unauthenticated client
(headers, CORS, throttling, metrics, registration, body limits). The Railway
service configuration was read through the Railway API (variable names only,
never values). No file was modified and no production data was written
outside the OpenV Platform project.

## 1. Verdict

**No authentication bypass and no cross-tenant access was found in the HTTP
API.** The multi-tenant model is coherent and consistently applied: bcrypt
passwords, every token from `crypto/rand` and stored as SHA-256, one
authorisation ladder on essentially every handler, object-to-project
resolution before any check (no IDOR found), org-scoped worker keys, a
least-privilege pool key, OIDC with nonce and state binding, sanitised 5xx
responses, parameterised SQL throughout, no raw HTML in the SPA.

The findings cluster in three places:

1. **The agent execution surface.** Vendor CLIs run with auto-approved edits
   (Gemini) or workspace-write sandboxes without a tool allowlist (Codex),
   while their prompts carry text from interview participants, repositories
   and web pages. Proposal gating protects OpenV data; it does not protect
   the runner host. This is the one **High**.
2. **Production configuration and hardening that the code supports but the
   deployment does not apply**: no throttling on sign-in, no security
   headers reaching the browser (an nginx `add_header` inheritance rule
   silently discards them), an open `/metrics`, open self-registration.
3. **Hygiene**: default secrets on the base compose file, a root API image,
   unencrypted on-host backups, a known-vulnerable Docker client module,
   invite tokens in the access log.

Every finding is recorded in the OpenV Platform project as a hazard
(HAZ-1 … HAZ-16 under *Security hazards*), each pointing at the draft
requirement (REQ-90 … REQ-100) that would close it, so `get_vv_gaps` shows
them as unmitigated until a design item mitigates them. The production
probe is test case TC-48 with a recorded **fail** in the run *2026-09-06
Assessment evidence*.

## 2. Findings

| ID | Severity | Affects | Finding | Closes with |
|---|---|---|---|---|
| SEC-1 | **High** | both | Prompt injection into auto-approving vendor CLIs. Gemini runs with `--approval-mode auto_edit` (`internal/runner/geminicli.go:112`), Codex with `--sandbox workspace-write` and no allowlist (`codexcli.go:140`), Claude Code with an allowlist only if the agent defines one (`claudecode.go:158`). The public interview transcript is concatenated into the interviewer prompt (`internal/api/suite_handlers.go:1528`) and that agent holds the OpenV tools; repo-access agents run in a real checkout; the V&V Assistant fetches the web. | REQ-91 |
| SEC-2 | Medium | self-hosted | Hosted-runner containers get memory/CPU caps only (`internal/hosting/docker.go:84-115`): no cap drop, no `no-new-privileges`, no pids limit, no dedicated network; provider keys in container env; the API holds the host Docker socket when enabled. | REQ-96 |
| SEC-3 | Medium | both | `provider_settings.api_key_env` is tenant-chosen and unvalidated (`internal/domain/providers/providers.go:66`); the worker does `os.Getenv` on it (`internal/runner/worker.go:368`), so a workspace admin can have `WORKER_API_KEY`, `RUNNER_POOL_KEY` or `DATABASE_URL` injected into an agent's environment. | REQ-92 |
| SEC-4 | Medium | self-hosted | Base compose runs Postgres as `postgres`/`postgres` with port 5432 published and `WORKER_API_KEY=dev-worker-key`; only the prod overlay makes them required. | REQ-97 |
| SEC-5 | Medium | both | No throttling on sign-in, registration, SSO callbacks or run launch (limiter exists only for public interview routes, `internal/api/handlers.go:233`). **Confirmed live**: 8 failed sign-ins in under 2 s, all 401, no back-off. | REQ-90 |
| SEC-6 | Medium | both | `image/svg+xml` accepted, uploader MIME stored, downloads served `inline` (`handlers.go:2275,2335`) → stored XSS on the API origin for a viewer opening the URL directly. Uploads use `io.ReadAll` with no `MaxBytesReader`; a 2 MB JSON body to sign-in was accepted on production. | REQ-93 |
| SEC-7 | Medium / Low on Railway | self-hosted | `govulncheck`: GO-2026-4887 and GO-2026-4883 in `github.com/docker/docker v28.5.2`, reachable via `internal/hosting`; six more advisories in required-not-called modules. Dormant on Railway (`HOSTED_RUNNERS=off`). | REQ-98 |
| SEC-8 | Low | both | `npm audit --omit=dev`: one high advisory in `fast-uri` (transitive, build-time). | REQ-98 |
| SEC-9 | Low | both | Public interview invite tokens are logged as part of the URL path (`internal/api/requestlog.go:62`). | REQ-97 |
| SEC-10 | Low | both | `Dockerfile.api` has no `USER`; the API runs as root in its container. | REQ-97 |
| SEC-11 | **Medium on Railway** / Low | both | **No browser security headers reach production.** `frontend/nginx.conf` sets X-Frame-Options, nosniff, Referrer-Policy and a CSP at server level, but every `location` block has its own `add_header`, and nginx discards inherited `add_header` directives in any location that sets one. Confirmed live: the frontend root, its static assets and every API response carry none of them, and there is no HSTS. The declared CSP is also permissive (`unsafe-inline`, `unsafe-eval`, any origin). | REQ-94 |
| SEC-12 | Low | self-hosted | Backups are plain `tar.gz` on the host with a full `pg_dump`; no encryption, no off-host copy. | REQ-88 |
| SEC-13 | Low | both (misconfig) | `CORS_ORIGIN="*"` reflects any origin *with* credentials (`cmd/server/main.go:636`). Production is configured with the exact origin; **confirmed live** that a foreign origin gets no CORS headers. | REQ-100 |
| SEC-14 | Low | Railway (config) | `/metrics` is open on production: 61 KB of Prometheus output to an unauthenticated client (Go runtime, per-route counts and latency, run gauges). `OPENV_METRICS_TOKEN` is not set on the service. | set the token |
| SEC-15 | Medium on Railway / Low | both | Self-registration is open on production with no verification, invitation or throttling; there is no way to close it. | REQ-95 |
| SEC-16 | Low | both | Sessions last 30 days with no idle expiry and no invalidation on password change (`internal/domain/users/users.go:25`). | REQ-99 |

## 3. Deployment-specific picture

### Railway production

| Aspect | State |
|---|---|
| Services | API, frontend, runner pool (1 replica each), Postgres; all online, no critical issues in the last 7 days; one failed pool deploy on 2026-09-05 since superseded. |
| Exposure | API and frontend have public domains; Postgres and the runner pool have none (private network only). |
| Variables (names only) | API: `CORS_ORIGIN`, `CROSS_SITE_COOKIES`, `DATABASE_URL`, `FRONTEND_URL`, `HOSTED_RUNNERS`, `OPENV_DATA_DIR`, `PUBLIC_URL`, `RUNNER_POOL_KEY`, `SECURE_COOKIES`, `UPLOADS_DIR`, `WORKER_API_KEY`. Not set: `OPENV_METRICS_TOKEN`, `OPENV_TRUST_PROXY`, any `OPENV_OIDC_*`/`GOOGLE_*`, `OPENV_SMTP_*`, `OPENV_EMBEDDING_*`. |
| Cookies | `SECURE_COOKIES` and `CROSS_SITE_COOKIES` set — session cookie is `SameSite=None; Secure`, which the split-domain layout requires. |
| TLS | Terminated at Railway's edge (HTTP/2). No HSTS from the origin. |
| Database | `DATABASE_URL` carries `sslmode=disable` on the private network, as `docs/railway.md` prescribes. |
| Legacy key | `WORKER_API_KEY` is set. It is accepted raw by the middleware and maps to the bootstrap workspace; prefer minted workspace keys and remove it once no runner uses it. |
| Trust proxy | `OPENV_TRUST_PROXY` unset, so per-IP limits (interview routes today, sign-in tomorrow) key on Railway's edge address rather than the client's. Set it to `1` behind Railway. |
| Open | SEC-5, SEC-11, SEC-14, SEC-15 are live on this deployment; SEC-1, SEC-3, SEC-6, SEC-9, SEC-16 apply in code. |

### Self-hosted (compose)

| Aspect | State |
|---|---|
| Prod overlay | Forces `POSTGRES_PASSWORD`, `CORS_ORIGIN`, `WORKER_API_KEY`; unpublishes 5432; healthchecks; memory limits; nginx unprivileged. Good. |
| Base file | Dev secrets and published 5432 (SEC-4). Running it on a reachable host is the risk. |
| Hosted runners | Require the Docker socket in the API container (root-equivalent host control); containers lack hardening beyond memory/CPU (SEC-2); `docker/docker` module vulnerable (SEC-7). Prefer the runner pool, which needs no socket. |
| TLS | Not provided by the stack; a reverse proxy must terminate it and should add HSTS. |
| Backups | Present and scripted, but unencrypted and on-host (SEC-12). |
| Open | SEC-1 … SEC-13, SEC-16 apply; SEC-14/15 depend on the operator's configuration. |

## 4. Controls verified as sound

- Passwords: bcrypt at default cost, minimum eight characters; SSO accounts
  bound to one provider, cross-provider sign-in refused, `email_verified`
  required.
- Tokens: worker keys, run tokens, pairing codes, invite tokens and session
  tokens are 32 random bytes, stored as SHA-256, shown once; env, pool and
  metrics keys compared in constant time.
- Authorisation: `requireProjectRole` / `requireOrgRole` / `requireRunAccess`
  applied on every non-public handler; every object addressed by id is
  resolved to its project or workspace first; workers cannot reach another
  org; pool key reaches pool endpoints only; run tokens scoped to one project
  and revoked at finish.
- OIDC: full ID-token verification, nonce replay binding, one-shot state
  cookie. Cookies `HttpOnly`, `Secure` under flag, correct `SameSite=None`
  pairing.
- Data: parameterised SQL throughout; LIKE escaped; cursor ids validated; git
  invoked by argv; agent slugs regex-validated with a path-escape check;
  attachment names sanitised (TC-23).
- Tenancy: search, events, SSE and notifications filtered to accessible
  projects / the authenticated user; shared products sanitised, session-only,
  attributable and removable (TC-14).
- Errors sanitised; no `InsecureSkipVerify`; connector refuses non-HTTPS
  pairing; provider settings store the key's env *name*, never the key;
  login records redacted; worker and frontend images non-root; base images
  pinned; CI promotion green-only with minimal permissions.

## 5. Prioritised actions

1. **SEC-1** — explicit tool allowlists for every agent; no auto-approved
   edits or workspace-write for agents that read interview, repository or
   web content; interviewer read-only plus `record_candidate_need`.
2. **SEC-3** — allowlist `api_key_env` to the provider catalog (or drop it).
3. **SEC-11** — fix the nginx `add_header` inheritance (repeat the headers
   in each location or use `include`), add headers in the Go API, add HSTS,
   tighten the CSP.
4. **SEC-5 / SEC-15** — throttle sign-in and registration; add a switch to
   close registration; set `OPENV_TRUST_PROXY=1` on Railway.
5. **SEC-14** — set `OPENV_METRICS_TOKEN` on the Railway API service.
6. **SEC-6** — `MaxBytesReader` on uploads and bodies; SVG as attachment
   with nosniff and a sandbox CSP.
7. **SEC-2 / SEC-4 / SEC-10** — harden hosted-runner containers; refuse dev
   secrets on non-loopback; non-root API image.
8. **SEC-7 / SEC-8** — `govulncheck` and `npm audit` in CI; bump when fixes
   ship.
9. **SEC-9 / SEC-12 / SEC-13 / SEC-16** — redact tokens in logs; encrypted
   off-host backups; refuse wildcard CORS; idle expiry and password-change
   invalidation.

## 6. Recorded in the OpenV Platform project

- Hazards HAZ-1 … HAZ-16 under **Security → Security hazards**.
- Draft requirements REQ-90 … REQ-100 under **Security**, derived from the
  new need NEED-12 (*Run it myself without an operations team*).
- Test case TC-48 *Production hardening probe* with a **fail** recorded in
  test run *2026-09-06 Assessment evidence* (steps 1, 2 and 4 failed; 3
  passed).
