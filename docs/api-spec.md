# OpenV API Specification (v1)

## Base URL
```
http://localhost:8080/api/v1
```

## Authentication
Currently, no authentication is required. Authentication via OIDC will be added in future versions.

## Content-Type
All requests and responses use `application/json`.

## Health Check

### GET /health
Check API health status.

**Response:**
```json
{
  "status": "ok"
}
```

---

## Artifacts

### Create Artifact
**POST /artifacts**

Create a new artifact (requirement, test case, hazard, design item, etc.).

**Request Body:**
```json
{
  "project_id": "uuid",
  "type": "requirement",
  "title": "System shall initialize within 5 seconds",
  "body": "The system must complete initialization within 5 seconds of startup.",
  "attributes": {
    "priority": "high",
    "status": "active"
  }
}
```

**Response (201 Created):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "project_id": "uuid",
  "type": "requirement",
  "title": "System shall initialize within 5 seconds",
  "body": "The system must complete initialization within 5 seconds of startup.",
  "attributes": {
    "priority": "high",
    "status": "active"
  },
  "version": 1,
  "valid_from": "2024-02-14T10:00:00Z",
  "valid_to": null,
  "created_at": "2024-02-14T10:00:00Z",
  "updated_at": "2024-02-14T10:00:00Z"
}
```

### Get Artifact
**GET /artifacts/{id}**

Retrieve a specific artifact by ID.

**Response (200 OK):**
Same as Create Artifact response.

**Response (404 Not Found):**
```json
{
  "error": "artifact not found"
}
```

### List Artifacts
**GET /artifacts**

List all artifacts for a project, optionally filtered by type.

**Query Parameters:**
- `project_id` (required): UUID of the project
- `type` (optional): Artifact type filter (requirement, test-case, hazard, design-item)

**Example:**
```
GET /artifacts?project_id=123e4567-e89b-12d3-a456-426614174000&type=requirement
```

**Response (200 OK):**
```json
[
  {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "project_id": "123e4567-e89b-12d3-a456-426614174000",
    "type": "requirement",
    "title": "System shall initialize within 5 seconds",
    "body": "...",
    "attributes": {},
    "version": 1,
    "valid_from": "2024-02-14T10:00:00Z",
    "valid_to": null,
    "created_at": "2024-02-14T10:00:00Z",
    "updated_at": "2024-02-14T10:00:00Z"
  }
]
```

### Update Artifact
**PUT /artifacts/{id}**

Update an existing artifact. Creates a new version.

**Request Body:**
```json
{
  "title": "Updated title",
  "body": "Updated description",
  "attributes": {
    "priority": "medium"
  }
}
```

**Response (200 OK):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "project_id": "uuid",
  "type": "requirement",
  "title": "Updated title",
  "body": "Updated description",
  "attributes": {
    "priority": "medium"
  },
  "version": 2,
  "valid_from": "2024-02-14T10:00:00Z",
  "valid_to": null,
  "created_at": "2024-02-14T10:00:00Z",
  "updated_at": "2024-02-14T10:05:00Z"
}
```

### Delete Artifact
**DELETE /artifacts/{id}**

Soft-delete an artifact (marks as deleted but retains history).

**Response (204 No Content)**

---

## Links (Traceability)

### Create Link
**POST /links**

Create a traceability link between two artifacts.

**Request Body:**
```json
{
  "from_id": "550e8400-e29b-41d4-a716-446655440000",
  "to_id": "660e8400-e29b-41d4-a716-446655440001",
  "type": "verifies",
  "attributes": {
    "coverage": "100%"
  }
}
```

**Response (201 Created):**
```json
{
  "id": "770e8400-e29b-41d4-a716-446655440002",
  "from_id": "550e8400-e29b-41d4-a716-446655440000",
  "to_id": "660e8400-e29b-41d4-a716-446655440001",
  "type": "verifies",
  "attributes": {
    "coverage": "100%"
  },
  "version": 1,
  "created_at": "2024-02-14T10:00:00Z",
  "updated_at": "2024-02-14T10:00:00Z"
}
```

### Get Link
**GET /links/{id}**

Retrieve a specific link by ID.

**Response (200 OK):**
Same as Create Link response.

### List Links
**GET /links**

List all links for a project.

**Query Parameters:**
- `project_id` (required): UUID of the project

**Response (200 OK):**
```json
[
  {
    "id": "770e8400-e29b-41d4-a716-446655440002",
    "from_id": "550e8400-e29b-41d4-a716-446655440000",
    "to_id": "660e8400-e29b-41d4-a716-446655440001",
    "type": "verifies",
    "attributes": {},
    "version": 1,
    "created_at": "2024-02-14T10:00:00Z",
    "updated_at": "2024-02-14T10:00:00Z"
  }
]
```

### Update Link
**PUT /links/{id}**

Update an existing link.

**Request Body:**
```json
{
  "type": "satisfies",
  "attributes": {
    "coverage": "95%"
  }
}
```

**Response (200 OK):**
```json
{
  "id": "770e8400-e29b-41d4-a716-446655440002",
  "from_id": "550e8400-e29b-41d4-a716-446655440000",
  "to_id": "660e8400-e29b-41d4-a716-446655440001",
  "type": "satisfies",
  "attributes": {
    "coverage": "95%"
  },
  "version": 2,
  "created_at": "2024-02-14T10:00:00Z",
  "updated_at": "2024-02-14T10:05:00Z"
}
```

### Delete Link
**DELETE /links/{id}**

Delete a link.

**Response (204 No Content)**

---

## Artifact Types

- `requirement` - A system or functional requirement
- `test-case` - A test case for verification
- `hazard` - A hazard analysis item
- `design-item` - A design element or component
- `other` - Unspecified artifact type

## Link Types

- `verifies` - A test verifies a requirement
- `satisfies` - A design satisfies a requirement
- `mitigates` - A control mitigates a hazard
- `implements` - A module implements a design item
- `depends-on` - One artifact depends on another

---

## Error Responses

### 400 Bad Request
```json
{
  "error": "description of validation error"
}
```

### 404 Not Found
```json
{
  "error": "resource not found"
}
```

### 500 Internal Server Error
```json
{
  "error": "internal server error"
}
```

---

## Pagination (Future)
Pagination will be implemented in v0.2.0 with optional `page` and `limit` query parameters.

## Filtering (Future)
Advanced filtering will be implemented in v0.2.0.
