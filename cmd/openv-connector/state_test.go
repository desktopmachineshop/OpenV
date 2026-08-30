package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// pointConfigAt makes configPath resolve inside a temp dir for one test.
func pointConfigAt(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("AppData", dir)
	} else {
		t.Setenv("XDG_CONFIG_HOME", dir)
		t.Setenv("HOME", dir)
	}
	path, err := configPath()
	if err != nil {
		t.Fatalf("configPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestPairingStateLifecycle locks in the per-workspace pairing contract:
// a legacy flat config migrates into the pairings map, new pairings add
// instead of overwrite, and selection honors the workspace hint with the
// active pairing as fallback.
func TestPairingStateLifecycle(t *testing.T) {
	path := pointConfigAt(t)

	legacy := `{"api_url":"http://localhost:8080","worker_key":"k-old","org_id":"org-a","org_name":"Alpha","user_name":"Dave"}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := loadState()
	if err != nil {
		t.Fatalf("loadState(legacy): %v", err)
	}
	if len(st.Pairings) != 1 || st.Pairings["org-a"] == nil || st.ActiveOrg != "org-a" {
		t.Fatalf("legacy config did not migrate: %+v", st)
	}

	if err := rememberPairing(&connectorConfig{
		APIURL: "https://prod.example", WorkerKey: "k-new", OrgID: "org-b", OrgName: "Beta", UserName: "Dave",
	}); err != nil {
		t.Fatalf("rememberPairing: %v", err)
	}

	st, err = loadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Pairings) != 2 {
		t.Fatalf("second pairing overwrote the first: %+v", st.Pairings)
	}
	if st.ActiveOrg != "org-b" {
		t.Errorf("ActiveOrg = %q, want org-b", st.ActiveOrg)
	}
	// Legacy mirror follows the active pairing (older builds keep working).
	if st.WorkerKey != "k-new" || st.APIURL != "https://prod.example" {
		t.Errorf("legacy mirror not updated: key=%q api=%q", st.WorkerKey, st.APIURL)
	}

	if cfg, err := selectPairing("org-a"); err != nil || cfg.WorkerKey != "k-old" {
		t.Fatalf("selectPairing(org-a) = %+v, %v; want the Alpha pairing", cfg, err)
	}
	// The hint became the active pairing for subsequent plain starts.
	if cfg, err := selectPairing(""); err != nil || cfg.OrgID != "org-a" {
		t.Fatalf("selectPairing(\"\") after hint = %+v, %v; want Alpha", cfg, err)
	}
	if _, err := selectPairing("org-unknown"); err == nil {
		t.Fatal("selectPairing(unknown) succeeded, want error naming paired workspaces")
	}
}
