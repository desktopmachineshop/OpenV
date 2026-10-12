package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
)

// unreadableKeys is a key table whose lookup by id fails, as a database that
// is down would.
type unreadableKeys struct{ *memWorkerKeys }

func (unreadableKeys) FindByID(string) (*workerkeys.Key, error) {
	return nil, errors.New("connection refused")
}

// strandedKeys finds the member's personal key and then loses it, so that
// the revocation answers workerkeys.ErrNotFound.
type strandedKeys struct {
	workerkeys.Service
}

func (strandedKeys) PersonalKey(orgID, userID string) (*workerkeys.Key, error) {
	return &workerkeys.Key{ID: "rk-1", OrgID: orgID, UserID: &userID}, nil
}

func (strandedKeys) Revoke(string, string) error { return workerkeys.ErrNotFound }

// TestRevokingARunnerKeyNoRowHasAnswers404 is the regression test for #644:
// DELETE /orgs/{id}/worker-keys/{keyId} answered 400 for a key the service
// refused as not found, and so did the revocation of a member's own runner
// key. A key no row has, and a key of another workspace, now answer 404 as
// one another, as for any id no row has (I3); a failure of the store keeps
// its 400, and a member with no runner key keeps the 204 the spec gives it.
func TestRevokingARunnerKeyNoRowHasAnswers404(t *testing.T) {
	const org = "org-1"
	newHandler := func(keys workerkeys.Service) *Handler {
		return newTestHandler(t, func(h *Handler) {
			h.WorkerKeyService = keys
			h.OrgService = &fakeOrgService{roles: map[string]map[string]string{
				org: {"admin": orgs.RoleAdmin, "member": orgs.RoleMember},
			}}
		})
	}
	revoke := func(call func(*Handler, http.ResponseWriter, *http.Request), h *Handler, user, keyID string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodDelete, "/", nil)
		r = mux.SetURLVars(asUser(r, &users.User{ID: user}), map[string]string{"id": org, "keyId": keyID})
		w := httptest.NewRecorder()
		call(h, w, r)
		return w
	}
	table := func() *memWorkerKeys {
		return &memWorkerKeys{keys: map[string]*workerkeys.Key{
			"h-own":   {ID: "k-own", OrgID: org, KeyHash: "h-own"},
			"h-other": {ID: "k-other", OrgID: "org-2", KeyHash: "h-other"},
		}}
	}
	const gone = `{"error":"worker key not found"}`

	t.Run("worker-keys: a key no row has, and another workspace's key, answer alike", func(t *testing.T) {
		keys := table()
		h := newHandler(workerkeys.NewDefaultService(keys))
		for _, id := range []string{"k-nobody", "k-other"} {
			w := revoke((*Handler).RevokeWorkerKey, h, "admin", id)
			if w.Code != http.StatusNotFound || strings.TrimSpace(w.Body.String()) != gone {
				t.Errorf("key %q: %d %q, want 404 %s", id, w.Code, w.Body.String(), gone)
			}
		}
		if keys.keys["h-other"].Revoked {
			t.Error("another workspace's key was revoked")
		}
	})

	t.Run("worker-keys: one of the workspace's keys is revoked", func(t *testing.T) {
		keys := table()
		w := revoke((*Handler).RevokeWorkerKey, newHandler(workerkeys.NewDefaultService(keys)), "admin", "k-own")
		if w.Code != http.StatusNoContent || !keys.keys["h-own"].Revoked {
			t.Fatalf("status = %d, revoked = %v; want 204 and revoked", w.Code, keys.keys["h-own"].Revoked)
		}
	})

	t.Run("worker-keys: a failing store keeps its 400", func(t *testing.T) {
		h := newHandler(workerkeys.NewDefaultService(unreadableKeys{table()}))
		if w := revoke((*Handler).RevokeWorkerKey, h, "admin", "k-own"); w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %q)", w.Code, w.Body.String())
		}
	})

	t.Run("my-runner-key: a key that is gone answers 404", func(t *testing.T) {
		w := revoke((*Handler).RevokeMyRunnerKey, newHandler(strandedKeys{}), "member", "")
		if w.Code != http.StatusNotFound || strings.TrimSpace(w.Body.String()) != gone {
			t.Fatalf("%d %q, want 404 %s", w.Code, w.Body.String(), gone)
		}
	})

	t.Run("my-runner-key: a member with no key keeps the 204", func(t *testing.T) {
		h := newHandler(workerkeys.NewDefaultService(table()))
		if w := revoke((*Handler).RevokeMyRunnerKey, h, "member", ""); w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204 (body %q)", w.Code, w.Body.String())
		}
	})
}
