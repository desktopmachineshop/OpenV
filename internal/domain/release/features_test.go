package release

import "testing"

// TestEnabled: nightly sees everything; stable sees a feature only once its
// turned-on release is the one the feature shipped in or later; no stable
// yet means every gate is closed.
func TestEnabled(t *testing.T) {
	f := Feature{Key: "x", ShippedIn: "0.3.0"}
	if !Enabled(f, "nightly", "") {
		t.Fatalf("nightly gate closed")
	}
	if Enabled(f, "stable", "") {
		t.Fatalf("stable with no release saw the feature")
	}
	if Enabled(f, "stable", "0.2.5") {
		t.Fatalf("stable older than the feature saw it")
	}
	if !Enabled(f, "stable", "0.3.0") || !Enabled(f, "stable", "0.10.0") {
		t.Fatalf("stable at or after the feature did not see it")
	}
}

func TestFeaturesForLooksUpTheStable(t *testing.T) {
	saved := Registry
	Registry = []Feature{{Key: "a", ShippedIn: "0.1.0"}, {Key: "b", ShippedIn: "0.3.0"}}
	defer func() { Registry = saved }()
	notes, err := Parse("## 0.3.0\n\n### New features\n\n- x\n\n## 0.2.0\n\nStable channel release since 2026-10-01.\n\n### New features\n\n- y\n\n## 0.1.0\n\n### New features\n\n- z\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	svc := &DefaultService{notes: notes}
	got := FeaturesFor(svc, "stable", "0.2.0")
	if !got["a"] || got["b"] {
		t.Fatalf("stable 0.2.0: %v", got)
	}
	// 0.3.0 exists but is not a stable release, so a workspace claiming it
	// gets nothing: only designated releases open gates.
	got = FeaturesFor(svc, "stable", "0.3.0")
	if got["a"] || got["b"] {
		t.Fatalf("an undesignated release opened a gate: %v", got)
	}
	got = FeaturesFor(svc, "nightly", "")
	if !got["a"] || !got["b"] {
		t.Fatalf("nightly: %v", got)
	}
	if k := Keys(); len(k) != 2 || k[0] != "a" {
		t.Fatalf("keys = %v", k)
	}
}
