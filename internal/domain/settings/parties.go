package settings

import (
	"errors"
	"fmt"
	"strings"
)

// Reference parties (REQ-147). Who can own an artifact is not only the
// project's members: on a programme with subsystems and suppliers, the
// owner of a requirement is often an organisation — the landing-gear
// supplier, the avionics team of a partner — that has no account here at
// all. Each project therefore keeps a list of parties it recognises, and
// the workspace's own company is always on it, first, without being stored
// (the handler adds it from the workspace's name). Owners are matched by
// name, so a party's name is what the "owner" attribute holds.

// PartiesKey is where a project's parties live inside its settings.
const PartiesKey = "parties"

// MaxParties bounds the list: past this a picker stops being a picker.
const MaxParties = 100

// ErrInvalidParties flags a list a caller tried to store with an empty or
// repeated name — user-facing validation (400), not a storage failure.
var ErrInvalidParties = errors.New("invalid parties")

// Party is one organisation, team or person a project recognises as an
// owner.
type Party struct {
	Name string `json:"name"`
	// Note is free text: what the party is responsible for, a contact.
	Note string `json:"note,omitempty"`
	// Default marks the workspace's own company, which is always present
	// and cannot be removed or edited.
	Default bool `json:"default,omitempty"`
}

// PartiesFromSettings reads the stored parties; absent or malformed means
// none.
func PartiesFromSettings(settings map[string]interface{}) []Party {
	raw, ok := settings[PartiesKey].([]interface{})
	if !ok {
		return nil
	}
	out := make([]Party, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if strings.TrimSpace(name) == "" {
			continue
		}
		note, _ := m["note"].(string)
		out = append(out, Party{Name: strings.TrimSpace(name), Note: strings.TrimSpace(note)})
	}
	return out
}

// ValidateParties trims the names and refuses empty, repeated (case-
// insensitively) or too many entries. The default party is never stored,
// so it is dropped here whatever a caller sent.
func ValidateParties(parties []Party) ([]Party, error) {
	if len(parties) > MaxParties {
		return nil, fmt.Errorf("%w: at most %d parties", ErrInvalidParties, MaxParties)
	}
	seen := map[string]bool{}
	out := make([]Party, 0, len(parties))
	for _, p := range parties {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: a party needs a name", ErrInvalidParties)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return nil, fmt.Errorf("%w: %q is listed twice", ErrInvalidParties, name)
		}
		seen[key] = true
		if p.Default {
			continue
		}
		out = append(out, Party{Name: name, Note: strings.TrimSpace(p.Note)})
	}
	return out, nil
}

// WithDefault puts the workspace's own party first, unless the project
// already lists it by name (in which case its note is kept and it is still
// marked default).
func WithDefault(workspace string, stored []Party) []Party {
	workspace = strings.TrimSpace(workspace)
	out := make([]Party, 0, len(stored)+1)
	if workspace != "" {
		def := Party{Name: workspace, Default: true}
		for _, p := range stored {
			if strings.EqualFold(p.Name, workspace) {
				def.Note = p.Note
			}
		}
		out = append(out, def)
	}
	for _, p := range stored {
		if workspace != "" && strings.EqualFold(p.Name, workspace) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// ProjectParties implements Service.
func (s *DefaultService) ProjectParties(projectID string) ([]Party, error) {
	current, err := s.store.ProjectSettings(projectID)
	if err != nil {
		return nil, err
	}
	return PartiesFromSettings(current), nil
}

// SetProjectParties implements Service.
func (s *DefaultService) SetProjectParties(projectID string, parties []Party) ([]Party, error) {
	clean, err := ValidateParties(parties)
	if err != nil {
		return nil, err
	}
	current, err := s.store.ProjectSettings(projectID)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	for k, v := range current {
		out[k] = v
	}
	if len(clean) == 0 {
		delete(out, PartiesKey)
	} else {
		stored := make([]interface{}, 0, len(clean))
		for _, p := range clean {
			m := map[string]interface{}{"name": p.Name}
			if p.Note != "" {
				m["note"] = p.Note
			}
			stored = append(stored, m)
		}
		out[PartiesKey] = stored
	}
	if err := s.store.SetProjectSettings(projectID, out); err != nil {
		return nil, err
	}
	return clean, nil
}
