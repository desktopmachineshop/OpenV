package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/users"
)

// TestListDomainEventsBadCursor locks in issue #190(a): a malformed "before"
// cursor is rejected with 400 at the handler, before the value ever reaches
// the repository's `$4::uuid` cast (which would otherwise surface a 500). The
// validation runs before eventRepo is touched, so a nil repo is fine here — a
// bad cursor must never get that far.
func TestListDomainEventsBadCursor(t *testing.T) {
	h := &Handler{}

	for _, bad := range []string{"not-a-uuid", "123", "'; DROP TABLE domain_events;--", "abcd"} {
		target := "/api/v1/events?" + url.Values{"before": {bad}}.Encode()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		ctx := context.WithValue(req.Context(), ctxUser, &users.User{ID: "u1"})
		ctx = context.WithValue(ctx, ctxActiveOrg, "org-1")
		w := httptest.NewRecorder()

		h.ListDomainEvents(w, req.WithContext(ctx))

		if w.Code != http.StatusBadRequest {
			t.Errorf("before=%q: status = %d, want %d (body %q)", bad, w.Code, http.StatusBadRequest, w.Body.String())
		}
	}
}
