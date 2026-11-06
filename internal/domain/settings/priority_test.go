package settings

import "testing"

// TestPriorityDetectionRoundTrip: off by default, on is stored, other
// settings survive, and off removes the key.
func TestPriorityDetectionRoundTrip(t *testing.T) {
	store := &fakeStore{project: map[string]interface{}{"parties": []interface{}{}}}
	svc := NewService(store)
	if on, err := svc.ProjectPriorityDetection("p1"); err != nil || on {
		t.Fatalf("default = %v, %v; want off", on, err)
	}
	if _, err := svc.SetProjectPriorityDetection("p1", true); err != nil {
		t.Fatalf("set: %v", err)
	}
	if on, _ := svc.ProjectPriorityDetection("p1"); !on {
		t.Fatalf("not stored: %v", store.project)
	}
	if _, ok := store.project["parties"]; !ok {
		t.Fatalf("other settings lost: %v", store.project)
	}
	if _, err := svc.SetProjectPriorityDetection("p1", false); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok := store.project[PriorityDetectionKey]; ok {
		t.Fatalf("off left the key behind")
	}
	if PriorityDetectionFromSettings(map[string]interface{}{PriorityDetectionKey: "yes"}) {
		t.Fatalf("a non-boolean value read as on")
	}
}
