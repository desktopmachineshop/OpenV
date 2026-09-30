package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/workerkeys"
)

// memWorkerKeys is an in-memory worker_keys table, keyed by hash, under the
// real workerkeys service; failLookups makes every lookup by hash fail, as
// a database that is down would.
type memWorkerKeys struct {
	keys        map[string]*workerkeys.Key
	failLookups bool
}

func (m *memWorkerKeys) Save(k *workerkeys.Key) error { m.keys[k.KeyHash] = k; return nil }
func (m *memWorkerKeys) List(orgID string) ([]*workerkeys.Key, error) {
	var out []*workerkeys.Key
	for _, k := range m.keys {
		if k.OrgID == orgID {
			out = append(out, k)
		}
	}
	return out, nil
}
func (m *memWorkerKeys) FindByID(id string) (*workerkeys.Key, error) {
	for _, k := range m.keys {
		if k.ID == id {
			return k, nil
		}
	}
	return nil, nil
}
func (m *memWorkerKeys) FindByHash(hash string) (*workerkeys.Key, error) {
	if m.failLookups {
		return nil, errors.New("connection refused")
	}
	return m.keys[hash], nil
}
func (m *memWorkerKeys) FindPersonal(orgID, userID string) (*workerkeys.Key, error) { return nil, nil }
func (m *memWorkerKeys) Revoke(id string) error {
	for _, k := range m.keys {
		if k.ID == id {
			k.Revoked = true
		}
	}
	return nil
}
func (m *memWorkerKeys) Touch(id string, at time.Time) error { return nil }
func (m *memWorkerKeys) HasOnlinePersonalKey(orgID, userID string, since time.Time) (bool, error) {
	return false, nil
}

// envKeyProbe sends one request with the bearer token through a middleware
// whose WORKER_API_KEY is envKey and whose bootstrap workspace is org-boot,
// and reports the answer and the workspace the handler saw ("" when the
// request did not reach it).
func envKeyProbe(keys workerkeys.Service, envKey, token string) (*httptest.ResponseRecorder, string) {
	m := NewAuthMiddleware(nil, fakeRunLookup{}, nil, keys, envKey, func() string { return "org-boot" })
	reached := ""
	handler := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = WorkerOrg(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	r := httptest.NewRequest(http.MethodPost, "/api/v1/agent-runs/claim", strings.NewReader(`{"worker_id":"w"}`))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w, reached
}

// Revoking the server's own WORKER_API_KEY on the Runners tab stops it
// (issue #379's question 16; OpenV REQ-31, whose TC-4 has revoked keys fail
// resolution). The boot registers the value as the env-bootstrap key of
// the bootstrap workspace once one exists; until then the middleware takes
// the raw value, and after, the row decides: revoked, the value is refused
// 401 like any revoked key, however long the environment still holds it,
// and a new value is a new key.
func TestARevokedEnvBootstrapKeyIsRefused(t *testing.T) {
	repo := &memWorkerKeys{keys: map[string]*workerkeys.Key{}}
	keys := workerkeys.NewDefaultService(repo)

	// No row yet (no personal workspace at boot): the raw value resolves to
	// the bootstrap workspace, as before.
	if w, org := envKeyProbe(keys, "env-value", "env-value"); w.Code != http.StatusNoContent || org != "org-boot" {
		t.Fatalf("the unregistered env key: %d %s, workspace %q; want it to act in org-boot", w.Code, w.Body.String(), org)
	}

	// The boot registers it; it still works, now through its row.
	if err := keys.EnsureBootstrapKey("org-boot", "env-value", "env-bootstrap"); err != nil {
		t.Fatal(err)
	}
	if w, org := envKeyProbe(keys, "env-value", "env-value"); w.Code != http.StatusNoContent || org != "org-boot" {
		t.Fatalf("the registered env key: %d %s, workspace %q; want it to act in org-boot", w.Code, w.Body.String(), org)
	}

	// An admin revokes the env-bootstrap row: the same value, still in the
	// environment, is refused, and so it stays after a boot with it.
	listed, _ := keys.List("org-boot")
	if len(listed) != 1 || listed[0].Name != "env-bootstrap" {
		t.Fatalf("keys of org-boot: %+v, want the env-bootstrap one", listed)
	}
	if err := keys.Revoke("org-boot", listed[0].ID); err != nil {
		t.Fatal(err)
	}
	for _, when := range []string{"once revoked", "after a boot with the same value"} {
		w, org := envKeyProbe(keys, "env-value", "env-value")
		if w.Code != http.StatusUnauthorized || org != "" || !strings.Contains(w.Body.String(), `"invalid token"`) {
			t.Errorf("the revoked env key, %s: %d %s, workspace %q; want 401 invalid token", when, w.Code, w.Body.String(), org)
		}
		if err := keys.EnsureBootstrapKey("org-boot", "env-value", "env-bootstrap"); !errors.Is(err, workerkeys.ErrRevoked) {
			t.Errorf("a boot with the revoked value: %v, want ErrRevoked", err)
		}
	}

	// A new value registers a new key at the next boot; the old one stays
	// refused, in the environment or not.
	if err := keys.EnsureBootstrapKey("org-boot", "new-env-value", "env-bootstrap"); err != nil {
		t.Fatal(err)
	}
	if w, org := envKeyProbe(keys, "new-env-value", "new-env-value"); w.Code != http.StatusNoContent || org != "org-boot" {
		t.Errorf("the new env key: %d %s, workspace %q; want it to act in org-boot", w.Code, w.Body.String(), org)
	}
	if w, _ := envKeyProbe(keys, "new-env-value", "env-value"); w.Code != http.StatusUnauthorized {
		t.Errorf("the old env key beside the new one: %d, want 401", w.Code)
	}

	// A lookup that fails does not fall back to the raw value, which could
	// be a revoked one.
	repo.failLookups = true
	if w, org := envKeyProbe(keys, "new-env-value", "new-env-value"); w.Code != http.StatusUnauthorized || org != "" {
		t.Errorf("the env key while its lookup fails: %d, workspace %q; want 401", w.Code, org)
	}
}
