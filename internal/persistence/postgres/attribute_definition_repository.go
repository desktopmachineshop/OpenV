package postgres

import (
	"database/sql"
	"encoding/json"

	"github.com/openv/requirements-platform/internal/domain/attributes"
)

// AttributeDefinitionRepository implements attributes.Repository over postgres.
type AttributeDefinitionRepository struct {
	db *sql.DB
}

// NewAttributeDefinitionRepository creates a new repository.
func NewAttributeDefinitionRepository(db *sql.DB) *AttributeDefinitionRepository {
	return &AttributeDefinitionRepository{db: db}
}

const attributeDefinitionColumns = `id, org_id, project_id, key, label, data_type, enum_values, applies_to_type, required, sort_order, created_at`

// scanDefinition reads one row (column order must match attributeDefinitionColumns).
func scanDefinition(scan func(dest ...interface{}) error) (*attributes.Definition, error) {
	def := &attributes.Definition{}
	var orgID, projectID sql.NullString
	var enumJSON []byte
	if err := scan(
		&def.ID,
		&orgID,
		&projectID,
		&def.Key,
		&def.Label,
		&def.DataType,
		&enumJSON,
		&def.AppliesToType,
		&def.Required,
		&def.SortOrder,
		&def.CreatedAt,
	); err != nil {
		return nil, err
	}
	if orgID.Valid {
		v := orgID.String
		def.OrgID = &v
	}
	if projectID.Valid {
		v := projectID.String
		def.ProjectID = &v
	}
	def.EnumValues = []string{}
	if len(enumJSON) > 0 {
		if err := json.Unmarshal(enumJSON, &def.EnumValues); err != nil {
			return nil, err
		}
	}
	return def, nil
}

// Create inserts a new definition.
func (r *AttributeDefinitionRepository) Create(def *attributes.Definition) error {
	enumJSON, err := marshalEnumValues(def.EnumValues)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(`
		INSERT INTO attribute_definitions
			(`+attributeDefinitionColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`,
		def.ID,
		nullableString(def.OrgID),
		nullableString(def.ProjectID),
		def.Key,
		def.Label,
		def.DataType,
		enumJSON,
		def.AppliesToType,
		def.Required,
		def.SortOrder,
		def.CreatedAt,
	)
	return err
}

// Update replaces a definition's editable fields (scope and key are immutable).
func (r *AttributeDefinitionRepository) Update(def *attributes.Definition) error {
	enumJSON, err := marshalEnumValues(def.EnumValues)
	if err != nil {
		return err
	}
	res, err := r.db.Exec(`
		UPDATE attribute_definitions
		SET label = $2, data_type = $3, enum_values = $4, applies_to_type = $5,
			required = $6, sort_order = $7
		WHERE id = $1
	`,
		def.ID,
		def.Label,
		def.DataType,
		enumJSON,
		def.AppliesToType,
		def.Required,
		def.SortOrder,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return attributes.ErrNotFound
	}
	return nil
}

// Delete removes a definition by id.
func (r *AttributeDefinitionRepository) Delete(id string) error {
	res, err := r.db.Exec(`DELETE FROM attribute_definitions WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return attributes.ErrNotFound
	}
	return nil
}

// Get returns one definition by id.
func (r *AttributeDefinitionRepository) Get(id string) (*attributes.Definition, error) {
	def, err := scanDefinition(r.db.QueryRow(
		`SELECT `+attributeDefinitionColumns+` FROM attribute_definitions WHERE id = $1`, id,
	).Scan)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, attributes.ErrNotFound
		}
		return nil, err
	}
	return def, nil
}

// ListByOrg returns the org-wide definitions for an org (project_id NULL).
func (r *AttributeDefinitionRepository) ListByOrg(orgID string) ([]*attributes.Definition, error) {
	rows, err := r.db.Query(`
		SELECT `+attributeDefinitionColumns+`
		FROM attribute_definitions
		WHERE org_id = $1 AND project_id IS NULL
		ORDER BY applies_to_type, sort_order, key
	`, orgID)
	if err != nil {
		return nil, err
	}
	return collectDefinitions(rows)
}

// ListByProject returns the project-scoped definitions for a project.
func (r *AttributeDefinitionRepository) ListByProject(projectID string) ([]*attributes.Definition, error) {
	rows, err := r.db.Query(`
		SELECT `+attributeDefinitionColumns+`
		FROM attribute_definitions
		WHERE project_id = $1
		ORDER BY applies_to_type, sort_order, key
	`, projectID)
	if err != nil {
		return nil, err
	}
	return collectDefinitions(rows)
}

// collectDefinitions scans and closes a result set.
func collectDefinitions(rows *sql.Rows) ([]*attributes.Definition, error) {
	defer rows.Close()
	out := []*attributes.Definition{}
	for rows.Next() {
		def, err := scanDefinition(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, def)
	}
	return out, rows.Err()
}

// marshalEnumValues serializes enum values as a JSON array (never NULL).
func marshalEnumValues(values []string) ([]byte, error) {
	if values == nil {
		values = []string{}
	}
	return json.Marshal(values)
}

// nullableString maps a *string to a driver NULL when nil/empty.
func nullableString(s *string) interface{} {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}
