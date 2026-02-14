# Quick Start Guide

## Overview

OpenV v0.1.0 (MVP) includes:
- ✅ Full artifact CRUD (requirements, test cases, hazards, design items)
- ✅ Traceability linking between artifacts
- ✅ RESTful API with all endpoints
- ✅ Interactive React UI with module view
- ✅ Local PostgreSQL database
- ✅ Docker Compose for one-command setup
- ✅ Comprehensive documentation

## Prerequisites

### Minimum Requirements
- **Docker & Docker Compose** (or Go 1.21+, Node.js 20+, PostgreSQL 15+)
- **4GB RAM**
- **2GB Disk Space**

### Recommended Setup
- **Docker Desktop** (includes Docker & Docker Compose)
- **VS Code** with Docker and REST Client extensions
- **UUID Generator** (uuidgenerator.net)

## Installation

### Option 1: Docker Compose (Recommended - 1 command)

```bash
# Clone or navigate to the project
cd openv

# Start all services
docker-compose up -d

# Wait for services to start
sleep 30

# Verify services are running
docker ps
```

**Services:**
- **Frontend**: http://localhost:3000
- **API**: http://localhost:8080
- **Database**: localhost:5432

### Option 2: Local Development

#### Backend Setup
```bash
# Navigate to project root
cd openv

# Download dependencies
go mod download

# Set environment variables (Linux/Mac)
export DB_HOST=localhost
export DB_PORT=5432
export DB_USER=postgres
export DB_PASSWORD=postgres
export DB_NAME=openv
export PORT=8080

# Or on Windows PowerShell:
$env:DB_HOST="localhost"
$env:DB_PORT="5432"
$env:DB_USER="postgres"
$env:DB_PASSWORD="postgres"
$env:DB_NAME="openv"
$env:PORT="8080"

# Start PostgreSQL (must be running separately)
# Then run the server
go run cmd/server/main.go
```

#### Frontend Setup
```bash
# In another terminal
cd frontend

# Install dependencies
npm install

# Start development server
npm start

# Opens http://localhost:3000 automatically
```

## First Steps

### 1. Generate a Project ID
Visit [uuidgenerator.net](https://uuidgenerator.net) and copy a UUID.

### 2. Set Project in UI
- Open http://localhost:3000
- Paste the UUID in the "Project ID" field
- Click "Set Project"

### 3. Create Your First Artifact
- Click "+ New Artifact"
- Fill in the form:
  - **Type**: "requirement"
  - **Title**: "System shall respond within 100ms"
  - **Description**: "All API responses must complete within 100 milliseconds"
- Click "Create"

### 4. Create a Test Case
- Click "+ New Artifact"
- **Type**: "test-case"
- **Title**: "Test API response time"
- **Description**: "Measure response time of API endpoints"
- Click "Create"

### 5. Create a Link
- Select your requirement from the list
- In the right panel, fill "Create Link"
- **Link Type**: "verifies"
- **Link To**: Select your test case
- Click "Create Link"

### 6. View Artifacts
- Click on any artifact in the list to see full details
- See all attributes and relationships
- Edit or delete using the action buttons

## Testing with curl

### Create an Artifact
```bash
curl -X POST http://localhost:8080/api/v1/artifacts \
  -H "Content-Type: application/json" \
  -d '{
    "project_id": "YOUR-PROJECT-UUID",
    "type": "requirement",
    "title": "System shall respond within 100ms",
    "body": "All API responses must complete within 100 milliseconds",
    "attributes": {"priority": "high"}
  }'
```

### List Artifacts
```bash
curl -X GET "http://localhost:8080/api/v1/artifacts?project_id=YOUR-PROJECT-UUID"
```

### Get Specific Artifact
```bash
curl -X GET http://localhost:8080/api/v1/artifacts/ARTIFACT-ID
```

### Create a Link
```bash
curl -X POST http://localhost:8080/api/v1/links \
  -H "Content-Type: application/json" \
  -d '{
    "from_id": "TEST-CASE-ID",
    "to_id": "REQUIREMENT-ID",
    "type": "verifies",
    "attributes": {}
  }'
```

### List Links
```bash
curl -X GET "http://localhost:8080/api/v1/links?project_id=YOUR-PROJECT-UUID"
```

## Stopping Services

### Docker Compose
```bash
# Stop all containers
docker-compose down

# Stop and remove volumes (deletes database)
docker-compose down -v
```

### Local Development
```bash
# Press Ctrl+C in each terminal
# Or kill the processes
kill $(lsof -t -i :8080)  # Backend
kill $(lsof -t -i :3000)  # Frontend
```

## Troubleshooting

### "Connection refused" on http://localhost:3000
- **Cause**: Frontend not running
- **Solution**: Run `cd frontend && npm start`

### "Connection refused" when creating artifacts
- **Cause**: API server not running
- **Solution**: Run `go run cmd/server/main.go`

### Database connection error
- **Cause**: PostgreSQL not accessible
- **Solution**: 
  - Check PostgreSQL is running: `docker ps`
  - Wait 30 seconds after `docker-compose up` for DB to initialize
  - Test connection: `psql -h localhost -U postgres -d openv`

### npm dependencies not found
- **Cause**: node_modules not installed
- **Solution**: 
  ```bash
  cd frontend
  npm install
  npm start
  ```

### Go module issues
- **Cause**: go.mod/go.sum out of sync
- **Solution**:
  ```bash
  go mod tidy
  go mod download
  ```

## Next Steps

### Learn the Architecture
Read `/docs/architecture.md` for deep dive into system design.

### Explore the API
See `/docs/api-spec.md` for complete API reference.

### Review Data Model
Check `/docs/data-model.md` for database schema details.

### Try Advanced Features (Future)
- Graph-based visualization (coming in v0.2)
- V&V coverage dashboards (coming in v0.2)
- Baseline snapshots (coming in v0.3)
- Plugin system (coming in v0.4)

## File Structure Reference

```
openv/
├── cmd/server/              # Backend entry point
├── internal/
│   ├── api/                 # HTTP handlers
│   ├── domain/
│   │   ├── artifacts/       # Artifact entities & service
│   │   └── links/           # Link entities & service
│   └── persistence/
│       └── postgres/        # Database repositories
├── frontend/
│   ├── src/
│   │   ├── components/      # React components
│   │   ├── views/           # Page views
│   │   ├── state/           # Zustand store
│   │   └── api/             # API client
│   └── package.json
├── deploy/
│   └── docker/              # Docker configs
├── docs/                    # Documentation
├── docker-compose.yml       # Local stack
├── Dockerfile.api           # Backend Docker image
└── README.md               # Main readme
```

## Development Workflow

### Making Changes to Backend
```bash
# Edit Go files
vim internal/domain/artifacts/artifact.go

# Rebuild and run
go run cmd/server/main.go
```

### Making Changes to Frontend
```bash
# Edit React files
vim frontend/src/components/ArtifactEditor.tsx

# React auto-reloads in dev mode
# Check browser at http://localhost:3000
```

### Database Inspection
```bash
# Connect to database
docker exec -it openv-postgres psql -U postgres -d openv

# List tables
\dt

# Inspect schema
\d artifacts
\d links

# Query data
SELECT * FROM artifacts LIMIT 10;
```

## Performance Tips

- The MVP is optimized for projects with <100k artifacts
- Queries are indexed for common patterns
- JSONB attributes support fast queries
- For larger projects, consider Neo4j in v0.2+

## Security Note

⚠️ **This is MVP code for internal use.**

- No authentication is enabled
- No HTTPS/TLS enforcement
- All CORS origins are allowed
- Not suitable for public internet exposure

For production use, implement:
- OIDC authentication
- RBAC (role-based access control)
- HTTPS/TLS
- Input validation and sanitization
- Audit logging

See roadmap for security features in v0.2+.

## Getting Help

- 📖 Read docs in `/docs` folder
- 🔍 Check existing issues on GitHub
- 💬 Ask in discussions
- 🐛 Report bugs with full stack trace

## Next Milestones

### v0.2.0 (Next)
- Graph visualization (Cytoscape integration)
- Advanced search and filtering
- V&V coverage dashboard
- Pagination for large projects
- Enhanced error messages

### v0.3.0
- Baseline management
- Diff viewer
- Bulk import/export
- Custom attributes in UI

### v0.4.0
- Plugin system
- OIDC authentication
- Cloud deployment (Helm charts)
- Advanced rules engine

---

**Ready to manage your requirements? Start with Docker Compose now! 🚀**
