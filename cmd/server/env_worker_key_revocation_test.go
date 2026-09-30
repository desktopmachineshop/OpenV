//go:build unix

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestARevokedWorkerAPIKeyStaysRevoked boots the real server four times on
// one database, the way an operator restarts it, and pins that revoking the
// server's own WORKER_API_KEY on the Runners tab stops it (issue #379's
// question 16, OpenV REQ-31): the first boot finds no workspace to register
// the value in, so the raw value acts in the first account's personal
// workspace; the second registers it there as the env-bootstrap key, which
// the account revokes, after which the value, still in the environment, is
// refused 401 like any revoked key; the third boot, with the same value,
// neither restores nor re-mints it, and logs why; the fourth, with a new
// value, registers a new key that works while the old value stays refused.
// The S5 tour cannot show this, since each area boots once on a fresh
// database, where the value is never registered.
func TestARevokedWorkerAPIKeyStaysRevoked(t *testing.T) {
	bin := serverBinary(t)
	const oldKey, newKey = "harness-env-worker-key-1", "harness-env-worker-key-2"
	s, db := bootServer(t, bin, map[string]string{"WORKER_API_KEY": oldKey})

	// send is one request to s, with the account's session or a bearer key.
	send := func(s *serverProcess, method, path, body string, session *http.Cookie, bearer string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, s.base+path, bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if session != nil {
			req.AddCookie(session)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := s.client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", method, path, err, s.output())
		}
		answer, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode, answer
	}
	claim := func(s *serverProcess, key string) (int, string) {
		t.Helper()
		status, body := send(s, http.MethodPost, "/api/v1/agent-runs/claim",
			`{"hosted":false,"min_priority":0,"providers":[],"worker_id":"harness"}`, nil, key)
		return status, strings.TrimSpace(string(body))
	}
	reboot := func(old *serverProcess, key string) *serverProcess {
		t.Helper()
		if _, _, err := old.terminate(30 * time.Second); err != nil {
			t.Fatalf("stop the server: %v\n%s", err, old.output())
		}
		for attempt := 1; ; attempt++ {
			next, err := startServer(t, bin, db, map[string]string{"WORKER_API_KEY": key})
			if err == nil {
				return next
			}
			if !errors.Is(err, errPortTaken) || attempt == 3 {
				t.Fatalf("boot again on the same database: %v\n%s", err, next.output())
			}
		}
	}

	// The first account, and with it the first personal workspace.
	body, _ := json.Marshal(map[string]string{"email": harnessEmail, "password": harnessPassword, "name": "Boot Harness"})
	req, err := http.NewRequest(http.MethodPost, s.base+"/api/v1/auth/register", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	var session *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "openv_session" {
			session = c
		}
	}
	if resp.StatusCode != http.StatusOK || session == nil {
		t.Fatalf("register: %s and no session cookie\n%s", resp.Status, s.output())
	}
	status, orgsBody := send(s, http.MethodGet, "/api/v1/orgs", "", session, "")
	var orgs struct {
		ActiveOrg string `json:"active_org"`
	}
	if err := json.Unmarshal(orgsBody, &orgs); status != http.StatusOK || err != nil || orgs.ActiveOrg == "" {
		t.Fatalf("the account's workspaces: %d %s", status, orgsBody)
	}
	type key struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Revoked bool   `json:"revoked"`
	}
	keys := func(s *serverProcess) []key {
		t.Helper()
		status, body := send(s, http.MethodGet, "/api/v1/orgs/"+orgs.ActiveOrg+"/worker-keys", "", session, "")
		var list []key
		if err := json.Unmarshal(body, &list); status != http.StatusOK || err != nil {
			t.Fatalf("the workspace's worker keys: %d %s", status, body)
		}
		return list
	}

	if status, body := claim(s, oldKey); status != http.StatusNoContent {
		t.Fatalf("first boot, no key row: the raw WORKER_API_KEY claims %d %s, want 204", status, body)
	}
	if list := keys(s); len(list) != 0 {
		t.Fatalf("first boot: %+v, want no key row (no workspace existed at boot)", list)
	}

	// The second boot registers the value as the env-bootstrap key.
	s = reboot(s, oldKey)
	list := keys(s)
	if len(list) != 1 || list[0].Name != "env-bootstrap" || list[0].Revoked {
		t.Fatalf("second boot: %+v, want one env-bootstrap key, not revoked", list)
	}
	if status, body := claim(s, oldKey); status != http.StatusNoContent {
		t.Fatalf("second boot: the registered key claims %d %s, want 204", status, body)
	}
	if status, body := send(s, http.MethodDelete, "/api/v1/orgs/"+orgs.ActiveOrg+"/worker-keys/"+list[0].ID, "", session, ""); status != http.StatusNoContent {
		t.Fatalf("revoke the env-bootstrap key: %d %s", status, body)
	}
	const refused = `{"error":"invalid token"}`
	if status, body := claim(s, oldKey); status != http.StatusUnauthorized || body != refused {
		t.Errorf("the revoked env-bootstrap key, still in the environment, claims %d %s; want 401 %s", status, body, refused)
	}

	// A boot with the same value keeps it revoked, says so, and adds no key.
	s = reboot(s, oldKey)
	if status, body := claim(s, oldKey); status != http.StatusUnauthorized || body != refused {
		t.Errorf("third boot, the same value: it claims %d %s; want 401 %s", status, body, refused)
	}
	if list := keys(s); len(list) != 1 || !list[0].Revoked {
		t.Errorf("third boot, the same value: %+v, want the one env-bootstrap key, still revoked", list)
	}
	if out := string(s.stderr.Bytes()) + string(s.stdout.Bytes()); !strings.Contains(out, "worker key revoked") {
		t.Errorf("third boot: its log does not say the value's key is revoked\n%s", s.output())
	}

	// A new value is a new key; the old one stays refused.
	s = reboot(s, newKey)
	if status, body := claim(s, newKey); status != http.StatusNoContent {
		t.Errorf("fourth boot, a new value: it claims %d %s, want 204", status, body)
	}
	if status, body := claim(s, oldKey); status != http.StatusUnauthorized {
		t.Errorf("fourth boot: the old value claims %d %s, want 401", status, body)
	}
	revoked, live := 0, 0
	for _, k := range keys(s) {
		if k.Name != "env-bootstrap" {
			t.Errorf("fourth boot: an unexpected key %+v", k)
		}
		if k.Revoked {
			revoked++
		} else {
			live++
		}
	}
	if revoked != 1 || live != 1 {
		t.Errorf("fourth boot: %d revoked and %d live env-bootstrap keys, want 1 and 1", revoked, live)
	}
}
