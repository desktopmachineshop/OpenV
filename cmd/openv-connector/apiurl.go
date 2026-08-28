package main

import (
	"net/http"
	"strings"
	"time"
)

// healthProbeTimeout keeps the post-pairing reachability check snappy: the
// connector is interactive and the browser-observed address is a known-good
// fallback, so there is no reason to wait long on a dead public URL.
const healthProbeTimeout = 3 * time.Second

// probeHealth reports whether an OpenV API server answers at baseURL. It hits
// GET /health, which the API keeps open without authentication (see
// internal/api/authmiddleware.go), and only a 2xx answer counts: a response
// from some other service on that address is not a reachable OpenV server.
func probeHealth(baseURL string, timeout time.Duration) bool {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// resolveAPIURL decides which API base URL to store after pairing.
//
// serverURL is the public URL the server reports about itself; browserURL is
// the address the browser actually used to reach the server (taken from the
// pairing link, so it is known to work from this machine). reachable probes a
// candidate URL; it is a parameter so the decision logic is testable.
//
// The server's public URL wins when it is usable from here; otherwise we
// prefer the address the browser actually used. The returned reason explains
// the choice for logging ("" means the trivial case: both agree, or only one
// candidate exists).
func resolveAPIURL(serverURL, browserURL string, reachable func(string) bool) (chosen, reason string) {
	server := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	browser := strings.TrimRight(strings.TrimSpace(browserURL), "/")

	if server == "" {
		return browser, "the server did not report a public URL; using the address the browser used"
	}
	if browser == "" || server == browser {
		return server, ""
	}
	if reachable(server) {
		return server, ""
	}
	return browser, "the server's public URL " + server + " is not reachable from this machine; using the address the browser used"
}
