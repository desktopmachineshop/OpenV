# Link Type Rules and Constraints

This document describes the link type constraints enforced in the OpenV requirements management platform.

## Link Type Definitions

### 1. verifies
- **Label**: "verifies"
- **Inverse Label**: "verified by"
- **Direction**: Test Case → Requirement
- **Description**: A test case demonstrates that a requirement is met
- **Example**: Test-001 verifies REQ-042

### 2. satisfies
- **Label**: "satisfies"
- **Inverse Label**: "satisfied by"
- **Direction**: Design Item → Requirement
- **Description**: A design element implements or fulfills a requirement
- **Example**: Component-A satisfies REQ-123

### 3. mitigates
- **Label**: "mitigates"
- **Inverse Label**: "mitigated by"
- **Direction**: Design Item → Hazard
- **Description**: A design element reduces or eliminates a hazard
- **Example**: Safety-Valve mitigates HAZ-005

### 4. decomposes to
- **Label**: "decomposes to"
- **Inverse Label**: "decomposed from"
- **Direction**: Requirement → Requirement
- **Description**: A high-level requirement is broken down into more specific sub-requirements
- **Example**: REQ-001 decomposes to REQ-001.1, REQ-001.2

### 5. impacts
- **Label**: "impacts"
- **Inverse Label**: "impacted by"
- **Direction**: Any Artifact Type → Any Artifact Type
- **Description**: A change to one artifact affects another
- **Example**: REQ-042 impacts Design-XYZ

### 6. relates-to
- **Label**: "relates-to"
- **Inverse Label**: "relates-to" (bidirectional)
- **Direction**: Any Artifact Type ↔ Any Artifact Type
- **Description**: A loose, non-semantic association between artifacts
- **Example**: DOC-123 relates-to REQ-456

## Implementation

### Frontend Validation
- Located in: `frontend/src/config/linkTypeRules.ts`
- Filters available link types based on source artifact type
- Filters available target artifacts based on selected link type
- Displays inverse labels for incoming links

### Backend Validation
- Located in: `internal/domain/links/validation.go`
- Validates link type against artifact types before creation
- Returns detailed error messages for invalid link attempts

## User Experience

When creating a link:
1. User selects an artifact (source)
2. Only valid link types for that artifact type are shown
3. User selects a link type
4. Only valid target artifact types are shown in the target dropdown
5. A helper text shows which artifact types are allowed

When viewing links:
- Outgoing links show the forward label (e.g., "verifies")
- Incoming links show the inverse label (e.g., "verified by")
- Links are grouped by type for easy navigation

## Adding New Link Types

To add a new link type:

1. Update `frontend/src/config/linkTypeRules.ts`:
   ```typescript
   {
     type: 'new-link-type',
     label: 'forward label',
     inverseLabel: 'inverse label',
     allowedFromTypes: ['source-type'],
     allowedToTypes: ['target-type'],
     description: 'Description of the link type',
   }
   ```

2. Update `internal/domain/links/validation.go`:
   ```go
   {
     Type:             "new-link-type",
     Label:            "forward label",
     InverseLabel:     "inverse label",
     AllowedFromTypes: []string{"source-type"},
     AllowedToTypes:   []string{"target-type"},
     Description:      "Description of the link type",
   }
   ```

3. Rebuild both frontend and backend services
