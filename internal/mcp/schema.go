package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

func schema(required []string, props map[string]interface{}) map[string]interface{} {
	if required == nil {
		required = []string{}
	}
	return map[string]interface{}{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

func str(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc}
}

func obj(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "object", "description": desc}
}

func boolean(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "boolean", "description": desc}
}

func strList(desc string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "array",
		"items":       map[string]interface{}{"type": "string"},
		"description": desc,
	}
}

func strArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}

// strListArg reads a list-of-strings argument, tolerating a bare string for
// clients that send a single value unwrapped. Always a non-nil slice: the API
// rejects a body whose list field is a string or null.
func strListArg(args map[string]interface{}, key string) []string {
	out := []string{}
	switch v := args[key].(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	case []string:
		for _, s := range v {
			if s != "" {
				out = append(out, s)
			}
		}
	case string:
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// boolArg reads a boolean argument. Some CLIs hand the server a JSON string
// where the schema says boolean, so "true" counts as true.
func boolArg(args map[string]interface{}, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	}
	return false
}

// decodeList parses a JSON array response into generic maps.
func decodeList(raw string) ([]map[string]interface{}, error) {
	var list []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, fmt.Errorf("unexpected list response: %v", err)
	}
	return list, nil
}

// baselineSummary strips a baseline's snapshot, which is the whole project
// serialized — a hundred thousand characters that would swamp an agent's
// context for what is almost always a metadata read (REQ-20). The snapshot's
// content is reachable as a rendered map via get_project_map with
// baseline_id, which is what an agent actually wants.
func baselineSummary(raw string) (string, error) {
	var baseline struct {
		ID        string `json:"id"`
		ProjectID string `json:"project_id"`
		Name      string `json:"name"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal([]byte(raw), &baseline); err != nil {
		return "", fmt.Errorf("unexpected baseline response: %v", err)
	}
	return toJSON(baseline)
}

func toJSON(v interface{}) (string, error) {
	buf, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}
