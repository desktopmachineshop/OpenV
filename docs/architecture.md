# OpenV Architecture

## High-Level System Design

```
┌──────────────────────────────────────────────────────────┐
│                    Frontend (React/TS)                   │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ Module View │ Artifact Editor │ Link Panel │ Graph  │ │
│  └─────────────────────────────────────────────────────┘ │
└──────────────────────┬───────────────────────────────────┘
                       │ HTTP/REST
┌──────────────────────▼───────────────────────────────────┐
│               API Layer (REST Gateway)                   │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ Routes │ Middleware │ Content Negotiation │ CORS    │ │
│  └─────────────────────────────────────────────────────┘ │
└──────────────────────┬───────────────────────────────────┘
                       │ Domain Services
┌──────────────────────▼───────────────────────────────────┐
│          Core Domain Services (Go)                       │
│  ┌────────────────────────────────────────────────────┐ │
│  │ Artifacts Service  │ Links Service │ V&V Service  │ │
│  │ (CRUD + Logic)     │ (Traceability) │ (Coverage)  │ │
│  └────────────────────────────────────────────────────┘ │
└──────────────────────┬───────────────────────────────────┘
                       │ Repository Pattern
┌──────────────────────▼───────────────────────────────────┐
│       Persistence Layer (Repository Pattern)             │
│  ┌─────────────────────────────────────────────────────┐ │
│  │ PostgreSQL Repositories │ S3 File Storage (future) │ │
│  └─────────────────────────────────────────────────────┘ │
└──────────────────────┬───────────────────────────────────┘
                       │ SQL
┌──────────────────────▼───────────────────────────────────┐
│                  Data Layer                              │
│  ┌───────────────────────────────────────────────────┐  │
│  │  PostgreSQL Database │ Local Filesystem Storage  │  │
│  └───────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
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
- CORS handling
- HTTP middleware (logging, error handling)

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
- **Full-text Search**: Future enhancement
- **S3/MinIO**: File attachments (future)

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
- No application-level caching (v0.1)
- Each request queries database
- Database provides consistency guarantees

---

## Deployment Topology

### Development (Docker Compose)
```
Your Machine
├── Frontend (React, port 3000)
├── API (Go, port 8080)
├── PostgreSQL (port 5432)
└── pgAdmin (port 5050) [optional]
```

### Production (Kubernetes - future)
```
Kubernetes Cluster
├── Frontend Pod (replicated)
├── API Pod (replicated)
├── PostgreSQL StatefulSet (primary + replicas)
├── Redis Cache (optional)
└── S3 Gateway (object storage)
```

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

### v0.1 (Current)
- No authentication
- No authorization
- CORS enabled for all origins
- Suitable for local/internal use only

### v0.2 (Future)
- OIDC authentication
- Role-based access control (RBAC)
- Project-level permissions
- Audit logging

### v1.0 (Long-term)
- Multi-tenant isolation
- Encryption at rest
- TLS in transit
- OAuth2 integration

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

### Teams graph
Agents can be arranged in teams with an org-chart edge set. Orchestration
hooks route follow-up runs along the graph (lead delegates to members),
enabling multi-step flows like draft-then-review.

### Auth model
Three credential classes, resolved by a single auth middleware:
- **User sessions** (cookie-based, optional Google OAuth) for people.
- **Run tokens** minted per agent run, scoping a worker's callbacks to that run.
- **Worker key** (`WORKER_API_KEY`) authenticating the host worker's queue
  polling.
Project access is governed by membership roles (viewer/editor/admin) checked
per request; public interview pages use separate invite tokens.
