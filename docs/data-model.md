# Data Model Specification

## Core Entities

### Project
Represents a collection of artifacts, links, and baselines.

```
id: UUID (primary key)
name: string
description: text
created_at: timestamp
updated_at: timestamp
```

### Artifact
Represents a single requirement, test case, hazard, design item, or other typed item.

```
id: UUID (primary key)
project_id: UUID (foreign key)
type: string
  - requirement
  - test-case
  - hazard
  - design-item
  - other
title: string (max 512 chars)
body: text (markdown or rich text)
attributes: JSONB (extensible key-value storage)
version: integer (incremented on each update)
valid_from: timestamp (when this version became active)
valid_to: timestamp (null for current version)
created_at: timestamp
updated_at: timestamp
```

**Indexes:**
- idx_project_id (project_id)
- idx_type (type)
- idx_valid_to (valid_to) - for efficient current version queries

**Relationships:**
- Has many Links (via from_id or to_id)

### Link
Represents a traceability relationship between two artifacts.

```
id: UUID (primary key)
from_id: UUID (foreign key -> Artifact.id)
to_id: UUID (foreign key -> Artifact.id)
type: string
  - verifies (test verifies requirement)
  - satisfies (design satisfies requirement)
  - mitigates (control mitigates hazard)
  - implements (component implements design)
  - depends-on (generic dependency)
attributes: JSONB (extensible storage for link metadata)
version: integer
created_at: timestamp
updated_at: timestamp
```

**Indexes:**
- idx_from_id (from_id)
- idx_to_id (to_id)
- idx_type (type)

**Constraints:**
- Foreign key: from_id references Artifact(id) on delete cascade
- Foreign key: to_id references Artifact(id) on delete cascade

### Baseline (Future)
Represents a snapshot of the project at a specific point in time.

```
id: UUID (primary key)
project_id: UUID (foreign key)
name: string
description: text
created_at: timestamp
artifact_versions: array<{artifact_id, version}>
link_versions: array<{link_id, version}>
```

---

## Versioning Strategy

### Artifact Versioning
- **Temporal Versioning**: Each update creates a new row with incremented version
- **Valid From/To**: Tracks when each version was active
- **Current Version**: WHERE valid_to IS NULL
- **History**: All rows are retained for audit trails

Example:
```
Row 1: artifact_1, v1, valid_from: 2024-01-01, valid_to: 2024-01-05
Row 2: artifact_1, v2, valid_from: 2024-01-05, valid_to: null (current)
```

### Link Versioning
- **Update in Place**: Links are updated in place (no temporal versioning yet)
- **Version Counter**: Incremented on each update
- **Future**: May implement temporal versioning in v0.2.0

---

## Extensibility

### Custom Attributes
All entities support custom attributes via JSONB:

```json
{
  "priority": "high",
  "status": "active",
  "tags": ["safety-critical", "performance"],
  "owner": "user@example.com"
}
```

### Plugin-Supplied Fields (Future)
Plugins can:
- Add new artifact types
- Add new link types
- Inject custom attributes into JSONB

---

## Query Patterns

### Get Current Version of Artifact
```sql
SELECT * FROM artifacts 
WHERE id = $1 AND valid_to IS NULL
```

### Get All Artifacts in Project
```sql
SELECT * FROM artifacts 
WHERE project_id = $1 AND valid_to IS NULL
ORDER BY created_at DESC
```

### Get Artifact History
```sql
SELECT * FROM artifacts 
WHERE id = $1
ORDER BY version DESC
```

### Get Outgoing Links
```sql
SELECT * FROM links 
WHERE from_id = $1
ORDER BY created_at DESC
```

### Get Incoming Links
```sql
SELECT * FROM links 
WHERE to_id = $1
ORDER BY created_at DESC
```

### Get Trace Path
```sql
-- Recursively trace from requirement to test coverage
WITH RECURSIVE trace AS (
  SELECT from_id, to_id, type, 1 as depth
  FROM links
  WHERE from_id = $1
  
  UNION ALL
  
  SELECT l.from_id, l.to_id, l.type, t.depth + 1
  FROM links l
  INNER JOIN trace t ON l.from_id = t.to_id
  WHERE t.depth < 5  -- limit recursion depth
)
SELECT * FROM trace
```

---

## Storage Considerations

### Artifact Body Field
- Stored as TEXT in PostgreSQL
- Supports markdown, HTML, or plain text
- Can be indexed for full-text search (future)
- Max size: ~2GB (PostgreSQL text limit)

### Attributes JSONB Field
- Index using `GIN` for fast queries: `CREATE INDEX idx_artifacts_attributes ON artifacts USING GIN (attributes)`
- Supports queries like: `WHERE attributes->>'priority' = 'high'`
- Max size: ~1GB

### Links Storage
- Simple row-per-link model
- No document embedding
- Supports both directed and undirected semantics via link type

---

## Scaling Considerations

### Phase 1 (v0.1 - v0.3)
- Single PostgreSQL database
- All data in one database
- Suitable for projects with <100k artifacts

### Phase 2 (v0.4+)
- Optional graph database (Neo4j/Dgraph) for complex traceability
- Artifact search index (Elasticsearch optional)
- Attachment storage in S3/MinIO

### Phase 3 (v1.0+)
- Sharding by project_id
- Distributed traceability queries
- Multi-region replication

---

## Audit Trail

All entities include:
- `created_at`: Initial creation timestamp
- `updated_at`: Last modification timestamp
- `version`: Version counter

Future versions will add:
- `created_by`: User who created the entity
- `updated_by`: User who last modified the entity
- `changelog`: Per-field modification history
