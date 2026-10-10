# OpenV - V&V Requirements Management Platform

A modern, open-source Verification & Validation (V&V) requirements management system inspired by IBM DOORS, with full traceability, baselines, and a clean web UI.

Public demo: https://openv-frontend-production.up.railway.app/

## Features

### Requirements and V&V
- **Artifact Management**: Create, read, update, and delete requirements, test cases, hazards, and design items
- **Traceability**: Link artifacts with typed relationships (verifies, satisfies, mitigates, decomposes-to, refines and more; see [docs/link-type-rules.md](docs/link-type-rules.md))
- **Module View**: Interactive grid view of artifacts with sorting and filtering
- **Artifact Editor**: Rich form-based editor with versioning
- **RESTful API**: Full-featured REST API for all operations
- **Local-first**: Runs entirely on your machine with Docker Compose

### Multi-Agent Suite
AI agents that work inside your projects, powered by your own vendor CLI
subscription (Claude Code / Codex / Gemini) running on your machine:

- **Agent workers**: host-side `agentd` worker pulls queued runs from the server; no API keys stored server-side
- **Transient runners**: lease a pre-warmed cloud runner for a while and sign your agents into it from the browser — nothing to install; the lease expires when you stop using it and the runner is wiped
- **MCP tool surface**: agents read and propose changes through typed tools (`openv-mcp`), fetching requirement content at run time
- **Human review**: agent writes land as proposals or draft artifacts for approval — nothing merges unreviewed
- **Product discovery**: personas, user needs, `derives-from`/`validates` traceability, and guided product definition
- **Interviews**: shareable links where stakeholders chat with an interviewer agent that records candidate needs
- **Automations**: manual, cron, and event-triggered agent runs; kanban cards can drive runs
- **V&V reporting**: coverage rollups, gap analysis, and a downloadable V&V status PDF

Setup guide: [docs/agents.md](docs/agents.md)

### What's next
- Phone-side runner control (see `docs/plans/mobile-support.md`; Web Push notifications have shipped)
- GitHub issues and pull requests tied to requirements and verification evidence
- Plugin system for custom rules and integrations
- Helm charts for Kubernetes

## Pricing

- **Hosted service**: four tiers. **Single User** (free, bring your own AI
  subscription), **Business Lite** (higher cloud-runner limits, agents on
  your own API keys, always-on agents), **Business** (shared company
  workspaces, teams and per-project access, billed per member) and
  **Enterprise** (run on your own servers with our support, SSO/OIDC setup,
  custom integrations, support SLA; by arrangement). Prices, where the
  instance has them, are on its `/pricing` page, and a workspace admin
  subscribes from workspace settings; payment goes through Stripe.
  Workspaces created during the alpha keep every tier's features free. The
  free-tier limits in force are listed on the same page.
- **Self-hosted**: free forever under the Elastic License 2.0, all features, your hardware.
- **Charities and open-source projects**: free forever on the hosted service.
  Claim it with a [free-hosting issue](https://github.com/desktopmachineshop/OpenV/issues/new?template=free-hosting.md).

Your data is never behind the paywall: export (JSON, CSV, Excel, ReqIF, PDF,
Word) and import (JSON, ReqIF) stay available on every plan whatever its
state, and a JSON export restores into a self-hosted OpenV.

## Quick Start

### Prerequisites
- Docker & Docker Compose
- Node.js 20+ (for local development without Docker)
- Go 1.26+ (for backend development)
- PostgreSQL 15+ (for local development)

### Running with Docker Compose

```bash
# Clone the repository
git clone https://github.com/yourusername/openv.git
cd openv

# Start all services
docker-compose up -d

# Wait for services to be ready (about 30 seconds)
sleep 30

# Access the application
# Frontend: http://localhost:3000
# API: http://localhost:8080
# PostgreSQL: localhost:5432
```

For production deployment (nginx static frontend, healthchecks, memory
limits, secrets from `.env`) and backup/restore, see
[docs/operations.md](docs/operations.md). To deploy on Railway (how the
public demo runs), see [docs/railway.md](docs/railway.md).

### Local Development

**Backend (Go)**
```bash
# Install dependencies
go mod download

# Set environment variables
export DB_HOST=localhost
export DB_PORT=5432
export DB_USER=postgres
export DB_PASSWORD=postgres
export DB_NAME=openv
export PORT=8080

# Run the server
go run ./cmd/server
```

**Frontend (React)**
```bash
cd frontend

# Install dependencies
npm install

# Start development server
npm start

# Open http://localhost:3000
```

## Project Structure

The map of the code, package by package, is
[docs/architecture.md](docs/architecture.md); every file belongs to one
area of [docs/areas.json](docs/areas.json).

```
openv/
├── cmd/
│   ├── server/              # API server: main() and its wire_*.go stages
│   ├── agentd/              # Runner (agent worker)
│   ├── openv-mcp/           # MCP server the runner hands each agent
│   ├── openv-connector/     # Agent Connector (a member's one-file runner)
│   └── openv-vapid/         # Web push key tool
├── internal/
│   ├── api/                 # REST API: routes.go, one *_handlers.go per area
│   ├── domain/              # Domain models, services and rules
│   │   ├── artifacts/       # Requirements, test cases, hazards, ...
│   │   └── links/           # Traceability links and their rules
│   ├── persistence/
│   │   └── postgres/        # Repositories, one file per migration
│   ├── runner/, mcp/        # Agent execution
│   └── archtest/            # Architecture tests and ratchets
├── frontend/
│   ├── src/
│   │   ├── components/      # Reusable React components
│   │   ├── views/           # Page-level components
│   │   ├── state/           # Zustand store
│   │   ├── api/             # API client (client.ts is the barrel)
│   │   └── App.tsx          # Main App component
│   └── package.json
├── e2e/                     # Playwright end-to-end tests
├── docs/                    # Documentation (deployment: docs/operations.md)
├── Dockerfile.api           # API server image
├── Dockerfile.worker        # Agent worker image
├── docker-compose.yml       # Local development stack
├── docker-compose.prod.yml  # Production overlay (see docs/operations.md)
└── go.mod                   # Go module definition
```

## API Endpoints

The full reference, every route with who may call it, is
[docs/api-spec.md](docs/api-spec.md). A few of the core ones:

### Artifacts
- `POST /api/v1/artifacts` - Create artifact
- `GET /api/v1/artifacts` - List artifacts (query: project_id, type)
- `GET /api/v1/artifacts/{id}` - Get artifact
- `PUT /api/v1/artifacts/{id}` - Update artifact
- `DELETE /api/v1/artifacts/{id}` - Delete artifact

### Links
- `POST /api/v1/links` - Create link
- `GET /api/v1/links` - List links (query: project_id)
- `GET /api/v1/links/{id}` - Get link
- `PUT /api/v1/links/{id}` - Update link
- `DELETE /api/v1/links/{id}` - Delete link

### Health
- `GET /health` - Health check

## Data Model

### Artifact
```json
{
  "id": "uuid",
  "project_id": "uuid",
  "type": "requirement|user-need|persona|test-case|hazard|design-item|heading|description|other",
  "title": "string",
  "body": "markdown",
  "attributes": {},
  "version": 1,
  "valid_from": "2024-01-01T00:00:00Z",
  "valid_to": null,
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

### Link
```json
{
  "id": "uuid",
  "from_id": "uuid",
  "to_id": "uuid",
  "type": "verifies|satisfies|mitigates|decomposes-to|refines|derives-from|validates|impacts|relates-to",
  "attributes": {},
  "version": 1,
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

## Example Workflow

1. **Create a Project** - From the projects page, in your workspace

2. **Add Requirements** - Create artifacts of type "requirement"

3. **Add Test Cases** - Create artifacts of type "test-case"

4. **Link Them** - Create links with type "verifies" from test-cases to requirements

5. **View Traceability** - See which tests verify which requirements

## Development

### Adding New Artifact Types
The catalogue is `internal/domain/artifacts/types.go`; the frontend reads it
from `GET /api/v1/meta/artifact-types`.

### Adding New Link Types
The rule table is `internal/domain/links/validation.go`, mirrored by hand in
`frontend/src/config/linkTypeRules.ts` and
[docs/link-type-rules.md](docs/link-type-rules.md); a parity test
(`contracts/vocab.json`) fails when the frontend's copy drifts.

### Running Tests
```bash
# Backend tests (not ./...: frontend/node_modules holds a Go package)
go test . ./cmd/... ./internal/...

# Frontend tests
cd frontend
npm test
```

## Configuration

### Environment Variables

Every variable the backend reads, with its default, is in
[docs/env-vars.md](docs/env-vars.md). The ones local development needs:

**Backend**
- `DB_HOST` - PostgreSQL host (default: localhost)
- `DB_PORT` - PostgreSQL port (default: 5432)
- `DB_USER` - PostgreSQL user (default: postgres)
- `DB_PASSWORD` - PostgreSQL password (default: postgres)
- `DB_NAME` - PostgreSQL database name (default: openv)
- `PORT` - API server port (default: 8080)

**Frontend**
- `REACT_APP_API_URL` - Backend API URL for the dev server (default: http://localhost:8080). Production builds leave it empty and call `/api` on their own origin, which the frontend's nginx proxies to the API (`API_UPSTREAM`)

## Roadmap

- [x] Core artifact CRUD, versions and typed traceability links
- [x] Module view, document view and traceability matrix
- [x] Baselines, comparison and change impact analysis
- [x] V&V dashboard, coverage, gaps and test runs
- [x] Import/export: JSON, CSV, Excel, ReqIF, PDF, Word
- [x] Workspaces, teams, per-project access, Google and OIDC sign-in
- [x] Multi-agent suite: runners, proposals, crews, automations, interviews
- [x] Phone-friendly shell
- [x] Web Push notifications
- [ ] Runner control from a phone
- [ ] GitHub issues and PRs tied to requirements and evidence
- [ ] Plugin system
- [ ] Helm charts

## Contributing

Contributions are welcome! Please follow these guidelines:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## Architecture Principles

- **Layered Architecture**: Clean separation between API, domain, and persistence
- **Domain-Driven Design**: Business logic isolated in domain services
- **Single Responsibility**: Each service has one reason to change
- **Testability**: Pure functions and dependency injection
- **Modularity**: Independent, composable components
- **Local-First**: Works offline, cloud optional
- **Configuration Over Code**: Environment-driven deployment

## Licensing

OpenV is licensed under the [Elastic License 2.0](LICENSE).

### What that means in plain English

- **Self-hosting is free, for anyone, at any scale.** A company can run OpenV
  on its own servers for its own teams, for good, with every feature. There
  is no licence key, no phone-home and no seat count.
- **The source is open to read, fork and change.** Modify it, keep your
  changes private or publish them, and contribute them back if you like.
- **Paid help is fine.** A consultant or integrator can charge to install,
  customise or support a customer's own OpenV.
- **The one thing you may not do is sell OpenV as a service.** You may not
  offer OpenV, or a substantial part of it, to third parties as a hosted or
  managed service. Hosted OpenV is how the project is funded.

The licence text is short and readable. Where this summary and the text
disagree, the text wins.

Contributions are accepted under the Developer Certificate of Origin — see
[CONTRIBUTING.md](CONTRIBUTING.md).

## Support

For issues, questions, or suggestions:
- Open an issue on GitHub
- Check existing documentation in `/docs`
- Review the API spec in `/docs/api-spec.md`

---

**OpenV** - Modern V&V for the 21st century
