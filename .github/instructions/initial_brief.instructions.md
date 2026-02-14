

---

# **AGENT INSTRUCTIONS — V&V Requirements Management Platform**

These instructions define how all agents, contributors, and automated tools should operate when working inside this repository. The goal is to maintain a consistent architecture, coding style, and design philosophy for the open‑source Verification & Validation (V&V) Requirements Management Platform inspired by IBM DOORS, but modern, modular, and deployable both locally and in the cloud.

---

# **1. Project Purpose**

This project implements a **single‑tenant, local‑first, cloud‑optional** requirements, verification, and validation system with:

- DOORS‑style module views  
- Typed artifacts (requirements, tests, hazards, design items, etc.)  
- Traceability and impact analysis  
- Baselines and versioning  
- V&V coverage engine  
- Plugin system for rules and integrations  
- Modern web GUI  

The system must run identically:

- on a single engineer’s laptop  
- on a team’s VM  
- in a cloud container  

Configuration changes only; code does not.

---

# **2. High-Level Architecture**

The system is composed of the following layers:

```
Frontend (React/TS)
↓
API Gateway (REST/GraphQL)
↓
Core Domain Services (Go)
↓
Persistence Layer (Postgres + S3/Filesystem)
```

Optional: Neo4j/Dgraph for advanced graph queries.

Each layer must remain **cleanly separated** and independently testable.

---

# **3. Backend Architecture (Go)**

### **3.1. Services**
Implement the backend as a modular Go application with the following services:

- **Requirements Service**  
  CRUD for artifacts, attributes, modules, and views.

- **Traceability Service**  
  Link management, impact analysis, trace matrices, graph traversal.

- **V&V Service**  
  Test cases, test results, coverage calculation, verification status.

- **Baseline Service**  
  Versioning, snapshot creation, diff engine.

- **Plugin Engine**  
  Schema extensions, rule packs, integrations (JS/WASM sandbox).

Each service must expose a clean interface and avoid leaking storage details.

---

### **3.2. API Layer**
- REST endpoints for all operations  
- Optional GraphQL for complex UI queries  
- Authentication pluggable (local, OIDC)  
- Versioned API (`/api/v1/...`)  

The API layer must contain **no business logic**.

---

### **3.3. Persistence**
Primary store: **PostgreSQL**

Use:

- Normalized tables for core entities  
- JSONB for dynamic attributes  
- Versioning via `valid_from` / `valid_to`  
- Baselines as snapshots of version IDs  

Attachments stored in:

- Local filesystem (local mode)  
- S3/MinIO (cloud mode)  

---

# **4. Frontend Architecture (React + TypeScript)**

The frontend must be a modern, modular SPA with:

- **AG Grid** for module views  
- **Cytoscape.js** for graph/traceability  
- **Zustand or Redux Toolkit** for state  
- **React Router** for navigation  

Core UI components:

- Module View  
- Artifact Editor  
- Link Panel  
- Traceability Matrix  
- Graph View  
- Baseline Diff Viewer  
- V&V Dashboard  

The frontend must communicate exclusively through the API.

---

# **5. Data Model Specification**

### **Artifact**
```
id: UUID
project_id: UUID
type: string (requirement, test-case, hazard, etc.)
title: string
body: string (markdown or rich text)
attributes: JSONB
version: int
valid_from: timestamp
valid_to: timestamp | null
```

### **Link**
```
id: UUID
from_id: UUID
to_id: UUID
type: string (verifies, satisfies, mitigates, etc.)
attributes: JSONB
versioned: yes
```

### **Test Result**
```
id: UUID
test_case_id: UUID
status: pass | fail | blocked
timestamp: datetime
evidence: file references
```

### **Baseline**
```
id: UUID
project_id: UUID
created_at: timestamp
artifact_versions: array
link_versions: array
```

---

# **6. Deployment Requirements**

### **Local Mode**
- Must run via Docker Compose or a single binary  
- Must support SQLite or Postgres  
- Must store attachments locally  
- Must run without internet access  

### **Cloud Mode**
- Must run in Docker or Kubernetes  
- Must use Postgres + S3/MinIO  
- Must support TLS termination via reverse proxy  
- Must support OIDC authentication  

The codebase must not diverge between modes.

---

# **7. Plugin System Requirements**

Plugins must be able to:

- Add new artifact types  
- Add new link types  
- Add new attributes  
- Add rule packs (e.g., DO‑178C, ISO 26262)  
- Add import/export formats  
- Add integrations (Jira, Git, CI test results)  

Plugins must:

- Register via manifest  
- Run in a sandbox (JS or WASM)  
- Never crash the core system  

---

# **8. Coding Standards**

### **Backend (Go)**
- Use Go modules  
- Use dependency injection  
- Keep domain logic pure  
- Avoid global state  
- Write unit tests for all services  
- Use interfaces for service boundaries  

### **Frontend (React/TS)**
- Use functional components  
- Use hooks for state  
- Keep components small and composable  
- Use TypeScript everywhere  
- Prefer pure functions and immutable state  

---

# **9. Repository Structure**

```
/cmd
    /server
/internal
    /api
    /domain
        /artifacts
        /links
        /vv
        /baselines
        /plugins
    /persistence
        /postgres
        /s3
/frontend
    /src
        /components
        /views
        /state
        /api
/deploy
    /docker
    /helm
/docs
    architecture.md
    api-spec.md
    data-model.md
```

---

# **10. Contribution Rules**

- All new features must include tests  
- All API changes must update the API spec  
- All UI changes must be accessible and responsive  
- All plugins must be isolated and sandboxed  
- All code must be formatted and linted  

---

# **11. Non-Goals**

The system must **not** attempt to:

- Become a multi‑tenant SaaS  
- Replace Jira or Git  
- Execute tests or CI pipelines  
- Provide AI‑generated requirements  

These may be future plugins, not core features.

---

# **12. Roadmap (Build Order)**

1. Core backend + artifact CRUD  
2. Module view + basic UI  
3. Links + traceability  
4. V&V engine  
5. Baselines + diff  
6. Plugin system  
7. Cloud deployment tooling  

