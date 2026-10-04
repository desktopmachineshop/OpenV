package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
)

// lastUsedKey is m1's personal runner key, last used at lastUsed (nil for
// never).
type lastUsedKey struct {
	workerkeys.Service
	lastUsed *time.Time
}

func (f lastUsedKey) PersonalKey(orgID, userID string) (*workerkeys.Key, error) {
	if userID != "m1" {
		return nil, nil
	}
	return &workerkeys.Key{ID: "rk-m1", OrgID: orgID, UserID: &userID, LastUsedAt: f.lastUsed}, nil
}

// A member's own runner key is online on the same terms as every other
// runner the workspace lists: it polled within workerOnlineWindow, the one
// window the worker status, the hosted runner and the guided copilot read
// (#379 bug 113, where GetMyRunnerKey kept a 30-second literal of its own).
// The cases sit two seconds either side of the window, so the answer moves
// with the constant, whatever it is set to.
func TestMyRunnerKeyIsOnlineWithinTheWorkerOnlineWindow(t *testing.T) {
	ago := func(d time.Duration) *time.Time { at := time.Now().Add(-d); return &at }
	for _, c := range []struct {
		name     string
		lastUsed *time.Time
		want     bool
	}{
		{"polled just inside the window", ago(workerOnlineWindow - 2*time.Second), true},
		{"polled just outside the window", ago(workerOnlineWindow + 2*time.Second), false},
		{"never polled", nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newTestHandler(t, func(h *Handler) {
				h.OrgService = &fakeOrgService{roles: map[string]map[string]string{"org-1": {"m1": orgs.RoleMember}}}
				h.WorkerKeyService = lastUsedKey{lastUsed: c.lastUsed}
			})
			router := mux.NewRouter()
			h.RegisterRoutes(router)
			r := httptest.NewRequest(http.MethodGet, "/api/v1/orgs/org-1/my-runner-key", nil)
			r = r.WithContext(context.WithValue(r.Context(), ctxUser, &users.User{ID: "m1"}))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			var got struct {
				KeyRecord *workerkeys.Key `json:"key_record"`
				Online    bool            `json:"online"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); w.Code != http.StatusOK || err != nil || got.KeyRecord == nil {
				t.Fatalf("GET my-runner-key: %d %s, want 200 and m1's key", w.Code, w.Body.String())
			}
			if got.Online != c.want {
				t.Errorf("online = %v, want %v with a %s window", got.Online, c.want, workerOnlineWindow)
			}
		})
	}
}
