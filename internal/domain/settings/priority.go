package settings

// Priority detection. A requirement's MoSCoW priority is its own attribute,
// separate from the normative keyword in its text. A project that grades its
// requirements by that keyword (shall for what must be met, should for what
// is wanted, may for what is allowed) can turn detection on, and the artifact
// editor then fills Priority from the text until someone picks a value. It is
// off unless a project turns it on: a project that writes every requirement
// with "shall", as ISO/IEC/IEEE 29148 does, would otherwise mark them all Must.

// PriorityDetectionKey is where the switch lives inside a project's settings.
const PriorityDetectionKey = "priority_detection"

// PriorityDetectionFromSettings reads the switch; absent or not a boolean
// means off.
func PriorityDetectionFromSettings(settings map[string]interface{}) bool {
	on, _ := settings[PriorityDetectionKey].(bool)
	return on
}

// ProjectPriorityDetection implements Service.
func (s *DefaultService) ProjectPriorityDetection(projectID string) (bool, error) {
	current, err := s.store.ProjectSettings(projectID)
	if err != nil {
		return false, err
	}
	return PriorityDetectionFromSettings(current), nil
}

// SetProjectPriorityDetection implements Service. Turning it off removes the
// key, so a project that never used it and one that stopped look the same.
func (s *DefaultService) SetProjectPriorityDetection(projectID string, on bool) (bool, error) {
	current, err := s.store.ProjectSettings(projectID)
	if err != nil {
		return false, err
	}
	out := map[string]interface{}{}
	for k, v := range current {
		out[k] = v
	}
	if on {
		out[PriorityDetectionKey] = true
	} else {
		delete(out, PriorityDetectionKey)
	}
	if err := s.store.SetProjectSettings(projectID, out); err != nil {
		return false, err
	}
	return on, nil
}
