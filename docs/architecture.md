# OpenV Architecture

## High-Level System Design

```
┌──────────────────────────────────────────────────────────┐
│                    Frontend (React/TS)                   │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ Module View │ Editor │ Suite (agents/crews/kanban) │ │
│  └─────────────────────────────────────────────────────┘ │
└──────────────────────┬───────────────────────────────────┘
                       │ HTTP/REST (session cookie + X-Org-ID)
┌──────────────────────▼───────────────────────────────────┐
│               API Layer (REST Gateway)                   │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ Routes │ Auth middleware │ Org/project RBAC │ CORS  │ │
│  │ Request logging (slog) │ Prometheus │ Rate limiting │ │
│  └─────────────────────────────────────────────────────┘ │
└──────────────────────┬───────────────────────────────────┘
                       │ Domain Services (org-scoped)
┌──────────────────────▼───────────────────────────────────┐
│          Core Domain Services (Go)                       │
│  ┌────────────────────────────────────────────────────┐ │
│  │ Artifacts │ Links │ V&V │ Orgs/RBAC │ Agent suite  │ │
│  │ (temporal versioning, proposals, event bus)        │ │
│  └────────────────────────────────────────────────────┘ │
└──────────────────────┬───────────────────────────────────┘
                       │ Repository Pattern
┌──────────────────────▼───────────────────────────────────┐
│       Persistence Layer (Repository Pattern)             │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ PostgreSQL Repositories │ Local filesystem uploads  │ │
│  └─────────────────────────────────────────────────────┘ │
└──────────────────────┬───────────────────────────────────┘
                       │ SQL (numbered migration ledger)
┌──────────────────────▼───────────────────────────────────┐
│                  Data Layer                              │
│  ┌───────────────────────────────────────────────────┐  │
│  │  PostgreSQL Database │ UPLOADS_DIR (attachments)  │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘

        ▲ Agent runs are rows in the agent_runs queue. The server
        │ never calls model providers itself: a host-side worker
        └─ (cmd/agentd) polls the queue over HTTP and runs the
           operator's vendor CLI. See "Multi-agent suite" below.
```

## Layered Architecture

### 1. Frontend Layer (React + TypeScript)
**Responsibility**: User interface, state management, client-side logic

**Components:**
- **Views**: Page-level components (ModuleView, Dashboard, etc.)
- **Components**: Reusable UI components (ArtifactEditor, LinkPanel, etc.)
- **State**: Zustand store for application state
- **API Client**: Axios-based HTTP client for backend communication

**Design Pattern**: Functional components with React Hooks

```
Frontend/
├── src/
│   ├── components/        # Reusable UI components
│   ├── views/             # Page-level views
│   ├── state/             # Zustand stores
│   ├── api/               # HTTP client
│   └── App.tsx            # Root component
```

### 2. API Layer (REST Gateway)
**Responsibility**: Request routing, middleware, input validation, response formatting

**Characteristics:**
- No business logic
- Thin adapter between frontend and domain
- Content negotiation (JSON)
- CORS restricted to the configured frontend origin (`CORS_ORIGIN`), with credentials
- HTTP middleware: authentication (`authmiddleware.go`), org/project RBAC
  (`authz.go`), request logging (`slog`), Prometheus metrics, sanitized error
  responses (`httperr.go`), and per-IP/per-invite rate limiting on the public
  interview endpoints

```go
// Example handler structure
type Handler struct {
    artifactService artifacts.Service
    linkService     links.Service
}

func (h *Handler) CreateArtifact(w http.ResponseWriter, r *http.Request) {
    // Parse request -> call service -> format response
}
```

### 3. Domain Services (Business Logic)
**Responsibility**: Core business rules, orchestration, validation

**Services:**
- **Artifacts Service**: CRUD operations, versioning logic
- **Links Service**: Traceability link management
- **V&V Service** (future): Coverage calculation, test result correlation

**Characteristics:**
- Pure Go, no HTTP knowledge
- Dependency injection
- Interface-based design for testability

```go
type Service interface {
    CreateArtifact(artifact *Artifact) error
    GetArtifact(id string) (*Artifact, error)
    UpdateArtifact(id string, req UpdateArtifactRequest) (*Artifact, error)
    DeleteArtifact(id string) error
    ListArtifacts(projectID string, artifactType string) ([]*Artifact, error)
}
```

### 4. Persistence Layer (Repository Pattern)
**Responsibility**: Data access abstraction, query execution

**Repositories:**
- **ArtifactRepository**: SQL queries for artifacts
- **LinkRepository**: SQL queries for links
- **ProjectRepository** (future): Project management

**Characteristics:**
- Encapsulates database implementation details
- Returns domain models, not raw SQL results
- Supports schema evolution
- Easy to mock for testing

```go
type Repository interface {
    Save(artifact *Artifact) error
    FindByID(id string) (*Artifact, error)
    FindByProjectID(projectID string) ([]*Artifact, error)
    Update(artifact *Artifact) error
    Delete(id string) error
}
```

### 5. Data Layer (Database)
**Responsibility**: Persistent storage, transactions

**Technologies:**
- **PostgreSQL**: Primary relational database
- **JSONB**: Flexible attribute storage
- **Trigram search** (`pg_trgm`): index-assisted cross-project artifact search,
  with a sequential-scan fallback when the extension is unavailable
- **Local filesystem** (`UPLOADS_DIR`): file attachments (S3/MinIO remains a
  future option)

The schema is owned by `internal/persistence/postgres/`. See
[data-model.md](data-model.md) for the full table reference.

---

## Multi-tenancy

Every tenant is an **organization** ("workspace"). Data is scoped to an org via
an `org_id` column on `projects`, `agents`, `agent_teams`, `automations`,
`agent_runs`, `guided_sessions`, `domain_events`, `provider_settings`,
`provider_logins`, and `templates` (`schema_orgs.go`).

- **Organizations** — `company` or `personal` (a personal org is auto-created
  at signup). Carry a `plan`, a `limits` JSONB, and an optional
  `monthly_budget_usd` spend cap (warn-only by default; soft-blocks launches at
  100% when `OPENV_BUDGET_ENFORCE=true`).
- **org_members** — `admin` / `member` roles.
- **org_teams / org_team_members** — people-teams within a workspace (distinct
  from agent "crews").
- **project_members** and **project_team_access** — direct and people-team
  grants of the project role ladder (`owner`/`editor`/`viewer`); a user's
  effective role is the highest of the two.
- **worker_keys** — org-scoped runner credentials (workspace or per-member).

A boot-time idempotent backfill (`BackfillOrgs` → `PromoteOrgColumns`) creates
personal orgs and promotes the `org_id` columns to `NOT NULL` on databases that
predate multi-tenancy.

---

## Observability

- **Structured logging** — the process installs a `slog` text handler
  (`OPENV_LOG_LEVEL` sets the level). `RequestLogMiddleware` logs one line per
  request, annotated by the auth middleware with the resolved org/user/actor.
- **Metrics** — a Prometheus registry (`internal/metrics`) is exposed at
  `/metrics` (optionally gated by `OPENV_METRICS_TOKEN`, never behind session
  auth). It records HTTP request counts/latency by method + route template +
  status, agent-run lifecycle counters/gauges (subscribing to run events), and
  live SSE connection counts. Cardinality is deliberately bounded — no
  per-project/user/agent labels.
- **Error responses** are sanitized (`httperr.go`): internals reach the log,
  not the client.

---

## Schema migration ledger

At startup `cmd/server/main.go` calls `postgres.MigrateAndBackfill`
(`migrations.go`), which advances the database through a numbered migration
ledger (`schema_migrations` table) under a boot advisory lock. Migration 0001 —
the frozen "baseline" — wraps the legacy idempotent init chain and re-runs on
every boot; every schema change since is an append-only numbered migration
(0002+) applied exactly once inside its own transaction (DDL and ledger row
commit together). New schema changes are **never** added to `InitSchema` or the
`schema_*.go` files — only appended to the registry in `migrations.go`.

---

## Dependency Injection

All services are constructed with their dependencies explicitly injected:

```go
// In main.go
artifactRepo := postgres.NewArtifactRepository(db)
artifactService := artifacts.NewDefaultService(artifactRepo)
handler := api.NewHandler(artifactService, linkService)
```

**Benefits:**
- Explicit dependencies
- Easy to test (inject mocks)
- No global state
- Clear initialization order

---

## Request Flow

### Create Artifact Flow
```
1. Frontend sends POST /api/v1/artifacts
2. API Handler parses request body
3. Handler validates input
4. Handler calls artifactService.CreateArtifact()
5. Service validates business rules
6. Service calls artifactRepo.Save()
7. Repository executes INSERT INTO artifacts
8. PostgreSQL returns success
9. Handler formats response and returns 201
10. Frontend receives artifact with generated ID
```

### Module View Load Flow
```
1. User enters project ID and loads page
2. Frontend calls artifactAPI.list(projectId)
3. API sends GET /api/v1/artifacts?project_id=xxx
4. Handler parses query params
5. Handler calls artifactService.ListArtifacts()
6. Service calls artifactRepo.FindByProjectID()
7. Repository executes SELECT ... WHERE project_id = $1
8. PostgreSQL returns rows
9. Repository unmarshals JSON attributes
10. Service returns []*Artifact
11. Handler returns JSON array (200 OK)
12. Frontend receives artifacts and renders grid
```

---

## Error Handling

### Layered Error Propagation
```
Database Error
    ↓
Repository wraps and logs
    ↓
Service handles or propagates
    ↓
Handler returns HTTP error
    ↓
Frontend displays to user
```

### Example Error Path
```go
// Repository
if err := rows.Scan(...); err != nil {
    log.Printf("Error scanning artifact: %v", err)
    return nil, err
}

// Service
artifacts, err := s.repo.FindByProjectID(projectID)
if err != nil {
    return nil, fmt.Errorf("failed to find artifacts: %w", err)
}

// Handler
artifacts, err := h.artifactService.ListArtifacts(projectID, "")
if err != nil {
    http.Error(w, err.Error(), http.StatusInternalServerError)
    return
}
```

---

## State Management

### Frontend State (Zustand)
```typescript
interface AppState {
    projectId: string
    artifacts: Artifact[]
    links: Link[]
    selectedArtifactId: string | null
    // ... getters and setters
}
```

### Backend In-Memory State
- No application-level caching; each request queries the database
- Database provides consistency guarantees
- One in-process component keeps live state: the event bus + SSE hub
  (`internal/events`, `internal/api/sse.go`) fans domain events out to
  connected clients. This is the single-instance assumption today (the
  interview rate limiter's token buckets are also in-process; see
  `docs/operations.md`)

---

## Deployment Topology

### Development (Docker Compose)
```
Your Machine
├── Frontend (React dev server, port 3000)
├── API (Go, port 8080)
└── PostgreSQL (port 5432)
```

### Production (Docker Compose overlay)
Production runs the same stack with `docker-compose.prod.yml` layered on top
(`make prod-up`): the frontend is a static nginx build, healthchecks and
memory limits are added, secrets come from `.env`, and Postgres stops
publishing its port. Full runbook — including backup/restore — in
[operations.md](operations.md).
```
Host
├── Frontend (nginx static build, host port 80)
├── API (Go, port 8080, X-Forwarded-For aware behind a proxy)
├── PostgreSQL (internal only)
└── (optional) agentd worker(s) + hosted-runner containers
```
Put a reverse proxy (Caddy, Traefik, nginx) in front for TLS. Kubernetes/Helm
remains a roadmap item.

---

## Testing Strategy

### Unit Tests (Services)
- Mock repositories
- Test business logic isolation
- Fast, reliable, no external dependencies

```go
func TestCreateArtifact(t *testing.T) {
    mockRepo := &MockArtifactRepository{}
    service := artifacts.NewDefaultService(mockRepo)
    
    artifact := &Artifact{...}
    err := service.CreateArtifact(artifact)
    
    assert.NoError(t, err)
    assert.True(t, mockRepo.SaveCalled)
}
```

### Integration Tests (Repository)
- Real PostgreSQL in container
- Test SQL queries and schema
- Slower but more realistic

### E2E Tests (API)
- Full stack in Docker
- Test complete workflows
- Validate frontend + backend integration

---

## Security Considerations

Authentication and authorization are enforced on every request; the details
below are generated from `internal/api/authmiddleware.go` and
`internal/api/authz.go`. See `docs/api-spec.md` for the per-route matrix.

### Authentication (`authmiddleware.go`)
Every API request authenticates as one of four principals; only `/health`,
`/metrics`, `/api/v1/auth/*`, and `/api/v1/public/*` are open:

- **Human users** — an `openv_session` HttpOnly cookie (SameSite=Lax, `Secure`
  when `SECURE_COOKIES=true`). Sign-in is email/password by default, with
  optional **Google OIDC** when `GOOGLE_CLIENT_ID`/`GOOGLE_CLIENT_SECRET` are
  set. The first user ever registered becomes the platform admin.
- **Active workspace (`X-Org-ID`)** — each session request runs in one
  organization ("workspace"). The header selects it; the middleware validates
  membership and falls back to the session's stored active org, then the
  user's personal org. An invalid header degrades to the fallback rather than
  failing the request.
- **Agent runs** — a single-run Bearer token (stored hashed), minted per run,
  scoping a worker's callbacks to that run and its own project.
- **Workers** — org-scoped Bearer worker keys (`worker_keys`, stored hashed):
  workspace keys minted by org admins, or per-member personal runner keys. The
  legacy `WORKER_API_KEY` env value is registered as the bootstrap org's
  workspace key at startup and still accepted directly.

### Authorization (`authz.go`)
- **Platform admin** (`users.is_admin`, the first user) passes every check.
- **Org roles** — `admin` and `member` (`org_members`). Org admins act as
  owners of every project in their org.
- **Project roles** — `owner` > `editor` > `viewer`. A member's effective role
  is the highest of their direct grant (`project_members`) and any people-team
  grant (`org_teams` via `project_team_access`).
- Agent runs count as editor within their own project; workers pass for any
  project in their org.

### Transport & hardening
- **CORS** is restricted to the configured frontend origin (`CORS_ORIGIN`),
  not "all origins", and allows credentials.
- **Rate limiting** on the public (token-only) interview endpoints: in-memory
  per-invite and per-IP token buckets bound provider spend from a leaked
  invite token (`internal/api/ratelimit.go`). Behind a proxy, set
  `OPENV_TRUST_PROXY=1` so limits key on the real client IP.
- **Sanitized error responses** (`internal/api/httperr.go`): clients get a
  stable public message while SQL text, file paths, and upstream details go
  only to the server log.
- **TLS in transit** is terminated by a reverse proxy in front of the stack
  (see operations.md); the compose overlay itself serves plain HTTP.

### Roadmap
- Encryption at rest
- Audit-log export and retention policy
- SSO/SAML beyond Google OIDC
- Multi-region / multi-instance isolation (today's deployment assumes a single
  API instance for the in-process event bus and rate limiter)

---

## Extensibility Points

### Adding New Services
1. Define interface in `internal/domain/{service}`
2. Implement default service
3. Inject into handler
4. Add API endpoints

### Adding New Repositories
1. Define interface in service package
2. Implement for PostgreSQL
3. Add to dependency injection

### Plugin System (Future)
- JavaScript/WASM sandbox
- Custom artifact types
- Custom link types
- Custom validation rules
- Import/export formats

---

## Multi-agent suite

OpenV's agent suite turns the platform into a queue-and-review system for
AI-assisted requirements work. The moving parts:

### Event bus
A lightweight in-process bus (`internal/events`) persists domain events
(artifact changes, test results, work-item moves, chatter) and fans them out to
subscribers: the SSE hub for live UI updates, orchestration hooks, and the
automation trigger matcher.

### agent_runs as a queue + host worker topology
Agent work is expressed as rows in `agent_runs` (status, priority, prompt,
heartbeat). The server never executes model calls itself. A host-side worker
(`cmd/agentd`) polls the queue over HTTP, launches the operator's vendor CLI
(claude/codex/gemini) for each run, and heartbeats progress back; a reaper
fails runs whose heartbeat goes stale. This keeps subscriptions and credentials
on the operator's machine.

### MCP tool surface
`cmd/openv-mcp` is an MCP server the vendor CLI attaches to. It exposes typed
tools for reading projects/artifacts/links, drafting artifacts, creating links,
recording test results, and recording candidate needs during interviews. Run
prompts carry identifiers only (lean-context rule); the agent pulls content
through these tools at run time, so authorization is enforced per call.

### Proposal review
Agents with `write_mode: proposal` (the default) never write directly. Each
intended write becomes a proposal row; approved proposals are applied through
the real domain services via appliers wired in `cmd/server/main.go`, so
validation and eventing behave exactly as for human edits.

### Crews (agent org charts)
Agents can be arranged in **crews** — org charts with a typed edge set
(`delegates-to`, `hands-off-to`, `reviews`; the DB tables keep the historical
`agent_team*` prefix, and `/api/v1/teams*` routes remain as deprecated
aliases). Orchestration hooks route follow-up runs along the graph (a lead
delegates to members), enabling multi-step flows like draft-then-review. Crews
are either project-pinned or workspace-wide.

### Automations
Unattended launch rules (`automations`) fire a run of an agent or crew. Three
kinds: `manual` (run-now), `scheduled` (cron, with catch-up), and `triggered`
(matched against the persisted `domain_events` stream, with per-rule cooldown
and hourly caps). Kanban cards can also enqueue runs by moving into an agent
column.

### Interviews
Stakeholder elicitation via shareable links. An interview has
token-authenticated invites; each participant chats with an interviewer agent
over public (token-only) endpoints, and the agent records candidate needs
through the MCP tools. These public endpoints are the ones the rate limiter
guards.

### Guided wizard + copilot
A guided requirements session (`guided_sessions`) walks a user through product
definition step by step, materializing draft artifacts that become real on
commit. A copilot chat runs alongside it: each message launches a short agent
run (linked via `agent_runs.guided_session_id`) whose reply streams back over
SSE.

### Auth model
Runs, workers, and users are resolved by a single auth middleware; see
[Security Considerations](#security-considerations) above for the full model
(session cookies + optional Google OIDC, org/project RBAC, org-scoped worker
keys, per-run tokens, and public interview invite tokens).
