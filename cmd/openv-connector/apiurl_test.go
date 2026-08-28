package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func reachableAlways(string) bool { return true }
func reachableNever(string) bool  { return false }

func TestResolveAPIURL(t *testing.T) {
	tests := []struct {
		name       string
		serverURL  string
		browserURL string
		reachable  func(string) bool
		want       string
		wantReason bool
	}{
		{
			name:       "server URL empty falls back to browser URL",
			serverURL:  "",
			browserURL: "https://openv.example.com",
			reachable:  reachableNever,
			want:       "https://openv.example.com",
			wantReason: true,
		},
		{
			name:       "identical URLs need no probe",
			serverURL:  "https://openv.example.com",
			browserURL: "https://openv.example.com",
			reachable:  reachableNever, // must not matter
			want:       "https://openv.example.com",
			wantReason: false,
		},
		{
			name:       "URLs identical after trailing-slash normalization",
			serverURL:  "https://openv.example.com/",
			browserURL: "https://openv.example.com",
			reachable:  reachableNever,
			want:       "https://openv.example.com",
			wantReason: false,
		},
		{
			name:       "reachable server URL wins over differing browser URL",
			serverURL:  "https://public.example.com",
			browserURL: "http://192.168.1.20:8080",
			reachable:  reachableAlways,
			want:       "https://public.example.com",
			wantReason: false,
		},
		{
			name:       "unreachable server URL falls back to browser URL",
			serverURL:  "https://public.example.com",
			browserURL: "http://192.168.1.20:8080",
			reachable:  reachableNever,
			want:       "http://192.168.1.20:8080",
			wantReason: true,
		},
		{
			name:       "browser URL empty keeps server URL without probing",
			serverURL:  "https://public.example.com",
			browserURL: "",
			reachable:  reachableNever,
			want:       "https://public.example.com",
			wantReason: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := resolveAPIURL(tt.serverURL, tt.browserURL, tt.reachable)
			if got != tt.want {
				t.Errorf("resolveAPIURL(%q, %q) = %q, want %q", tt.serverURL, tt.browserURL, got, tt.want)
			}
			if (reason != "") != tt.wantReason {
				t.Errorf("resolveAPIURL(%q, %q) reason = %q, want reason: %v", tt.serverURL, tt.browserURL, reason, tt.wantReason)
			}
		})
	}
}

// TestResolveAPIURLProbesOnlyServerURL pins down that the browser-observed
// address is trusted without a probe (the browser just used it to pair) and
// that only the server-reported URL is ever probed.
func TestResolveAPIURLProbesOnlyServerURL(t *testing.T) {
	var probed []string
	resolveAPIURL("https://public.example.com", "http://10.0.0.5:8080", func(u string) bool {
		probed = append(probed, u)
		return false
	})
	if len(probed) != 1 || probed[0] != "https://public.example.com" {
		t.Errorf("probed %v, want exactly [https://public.example.com]", probed)
	}
}

func TestProbeHealth(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("probe hit %q, want /health", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer healthy.Close()

	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer notFound.Close()

	if !probeHealth(healthy.URL, time.Second) {
		t.Error("probeHealth = false for a healthy server, want true")
	}
	// Trailing slash must not produce //health.
	if !probeHealth(healthy.URL+"/", time.Second) {
		t.Error("probeHealth = false for a healthy server with trailing slash, want true")
	}
	// A server that answers but is not an OpenV API (no /health) is not "reachable".
	if probeHealth(notFound.URL, time.Second) {
		t.Error("probeHealth = true for a non-OpenV server, want false")
	}

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	dead.Close() // now nothing listens there
	if probeHealth(dead.URL, time.Second) {
		t.Error("probeHealth = true for a closed server, want false")
	}
}
