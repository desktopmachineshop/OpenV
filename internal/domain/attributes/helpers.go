package attributes

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// validKey reports whether a key is lowercase letters, numbers, and
// underscores (a stable JSONB map key, not a display label).
func validKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_':
		default:
			return false
		}
	}
	return true
}

// validateScope enforces exactly-one-of org_id / project_id.
func validateScope(orgID, projectID *string) error {
	hasOrg := orgID != nil && strings.TrimSpace(*orgID) != ""
	hasProject := projectID != nil && strings.TrimSpace(*projectID) != ""
	if hasOrg == hasProject {
		return ErrInvalidScope
	}
	return nil
}

// cleanEnumValues trims and drops empty/duplicate enum values, preserving order.
func cleanEnumValues(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// isNumber reports whether a JSON-decoded value is numeric. encoding/json
// yields float64 for numbers; json.Number and Go integer types are accepted
// too so in-process callers are not surprised.
func isNumber(value interface{}) bool {
	switch v := value.(type) {
	case float64, float32, int, int32, int64:
		return true
	case json.Number:
		_, err := v.Float64()
		return err == nil
	default:
		return false
	}
}

// dateLayouts are the accepted date formats for DataTypeDate values.
var dateLayouts = []string{"2006-01-02", time.RFC3339}

// validDate reports whether s parses as a date in an accepted layout.
func validDate(s string) bool {
	for _, layout := range dateLayouts {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

// sortDefinitions orders definitions for stable UI rendering: by
// applies_to_type, then sort_order, then key.
func sortDefinitions(defs []*Definition) {
	sort.SliceStable(defs, func(i, j int) bool {
		a, b := defs[i], defs[j]
		if a.AppliesToType != b.AppliesToType {
			return a.AppliesToType < b.AppliesToType
		}
		if a.SortOrder != b.SortOrder {
			return a.SortOrder < b.SortOrder
		}
		return a.Key < b.Key
	})
}
