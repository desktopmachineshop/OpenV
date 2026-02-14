# OpenV - V&V Requirements Management Platform

A modern, open-source Verification & Validation (V&V) requirements management system inspired by IBM DOORS, with full traceability, baselines, and a clean web UI.

## Features

### MVP (v0.1.0)
- **Artifact Management**: Create, read, update, and delete requirements, test cases, hazards, and design items
- **Traceability**: Link artifacts with typed relationships (verifies, satisfies, mitigates, implements, etc.)
- **Module View**: Interactive grid view of artifacts with sorting and filtering
- **Artifact Editor**: Rich form-based editor with versioning
- **RESTful API**: Full-featured REST API for all operations
- **Local-first**: Runs entirely on your machine with Docker Compose

### Planned Features
- Graph-based traceability visualization
- V&V coverage reporting and dashboards
- Baseline snapshots and diff comparison
- Plugin system for custom rules and integrations
- Cloud deployment (Kubernetes, Helm)
- Advanced search and filtering
- Test result integration
- Multi-user support with OIDC

## Quick Start

### Prerequisites
- Docker & Docker Compose
- Node.js 20+ (for local development without Docker)
- Go 1.21+ (for backend development)
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
go run cmd/server/main.go
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

```
openv/
├── cmd/
│   └── server/              # Main server entry point
├── internal/
│   ├── api/                 # REST API handlers
│   ├── domain/              # Domain models and logic
│   │   ├── artifacts/       # Requirement/test case entities
│   │   └── links/           # Traceability links
│   └── persistence/
│       └── postgres/        # Database repositories
├── frontend/
│   ├── src/
│   │   ├── components/      # Reusable React components
│   │   ├── views/           # Page-level components
│   │   ├── state/           # Zustand stores
│   │   ├── api/             # API client
│   │   └── App.tsx          # Main App component
│   └── package.json
├── deploy/
│   └── docker/              # Docker configuration
├── docs/                    # Documentation
├── docker-compose.yml       # Local development stack
└── go.mod                   # Go module definition
```

## API Endpoints

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
  "type": "requirement|test-case|hazard|design-item",
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
  "type": "verifies|satisfies|mitigates|implements|depends-on",
  "attributes": {},
  "version": 1,
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

## Example Workflow

1. **Create a Project** - Use any UUID as your project ID (can generate one at uuidgenerator.net)

2. **Add Requirements** - Create artifacts of type "requirement"

3. **Add Test Cases** - Create artifacts of type "test-case"

4. **Link Them** - Create links with type "verifies" from test-cases to requirements

5. **View Traceability** - See which tests verify which requirements

## Development

### Adding New Artifact Types
Modify the artifact type enum in:
- Backend: `internal/domain/artifacts/artifact.go`
- Frontend: `frontend/src/components/ArtifactEditor.tsx`

### Adding New Link Types
Modify the link type enum in:
- Backend: `internal/domain/links/link.go`
- Frontend: `frontend/src/components/LinkPanel.tsx`

### Running Tests
```bash
# Backend tests
go test ./...

# Frontend tests
cd frontend
npm test
```

## Configuration

### Environment Variables

**Backend**
- `DB_HOST` - PostgreSQL host (default: localhost)
- `DB_PORT` - PostgreSQL port (default: 5432)
- `DB_USER` - PostgreSQL user (default: postgres)
- `DB_PASSWORD` - PostgreSQL password (default: postgres)
- `DB_NAME` - PostgreSQL database name (default: openv)
- `PORT` - API server port (default: 8080)

**Frontend**
- `REACT_APP_API_URL` - Backend API URL (default: http://localhost:8080)

## Roadmap

- [x] Core artifact CRUD
- [x] Basic traceability links
- [x] Module view UI
- [ ] Graph visualization
- [ ] V&V dashboard and coverage reports
- [ ] Baseline management
- [ ] Plugin system
- [ ] Cloud deployment (Helm charts)
- [ ] Advanced search and filtering
- [ ] Multi-user authentication
- [ ] Test result integration
- [ ] Import/export formats

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

This project is licensed under the MIT License - see the LICENSE file for details.

## Support

For issues, questions, or suggestions:
- Open an issue on GitHub
- Check existing documentation in `/docs`
- Review the API spec in `/docs/api-spec.md`

---

**OpenV** - Modern V&V for the 21st century
