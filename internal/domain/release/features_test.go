package release

import "testing"

// TestEnabled: nightly sees everything; stable sees a feature only once its
// turned-on release was cut from that nightly or later; no stable yet means
// every gate is closed.
func TestEnabled(t *testing.T) {
	f := Feature{Key: "x", ShippedIn: "2026-09-20"}
	if !Enabled(f, "nightly", "") {
		t.Fatalf("nightly gate closed")
	}
	if Enabled(f, "stable", "") {
		t.Fatalf("stable with no release saw the feature")
	}
	if Enabled(f, "stable", "2026-09-19") {
		t.Fatalf("stable cut before the feature saw it")
	}
	if !Enabled(f, "stable", "2026-09-20") || !Enabled(f, "stable", "2026-10-01.2") {
		t.Fatalf("stable cut after the feature did not see it")
	}
}

func TestFeaturesForLooksUpTheStable(t *testing.T) {
	saved := Registry
	Registry = []Feature{{Key: "a", ShippedIn: "2026-09-10"}, {Key: "b", ShippedIn: "2026-09-13"}}
	defer func() { Registry = saved }()
	notes, err := Parse("## 2026.09\n\nCut on 2026-10-01 from 2026-09-12.2.\n\n- x\n\n## 2026-09-13\n\n- y\n\n## 2026-09-12.2\n\n- z\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	svc := &DefaultService{notes: notes}
	got := FeaturesFor(svc, "stable", "2026.09")
	if !got["a"] || got["b"] {
		t.Fatalf("stable 2026.09: %v", got)
	}
	got = FeaturesFor(svc, "stable", "2026.08")
	if got["a"] || got["b"] {
		t.Fatalf("unknown stable opened a gate: %v", got)
	}
	got = FeaturesFor(svc, "nightly", "")
	if !got["a"] || !got["b"] {
		t.Fatalf("nightly: %v", got)
	}
	if k := Keys(); len(k) != 2 || k[0] != "a" {
		t.Fatalf("keys = %v", k)
	}
}
