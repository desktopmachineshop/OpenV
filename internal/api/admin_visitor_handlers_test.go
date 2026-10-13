package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/usermetrics"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// Site visitors: the public beacon and the platform admin's dashboard.

type visitorFake struct {
	visits []usermetrics.Visit
}

func (f *visitorFake) RecordVisit(v usermetrics.Visit) { f.visits = append(f.visits, v) }
func (f *visitorFake) Dashboard() (*usermetrics.VisitorDashboard, error) {
	return &usermetrics.VisitorDashboard{NewVisitors30d: 10, Conversions30d: 2, ConversionRate30d: 0.2}, nil
}

type sessionUserFake struct {
	users.Service
	byToken map[string]*users.User
}

func (f *sessionUserFake) GetBySessionToken(token string) (*users.User, error) {
	if u, ok := f.byToken[token]; ok {
		return u, nil
	}
	return nil, users.ErrUserNotFound
}

const browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

func newVisitHandler(t *testing.T) (*Handler, *visitorFake) {
	vf := &visitorFake{}
	h := newTestHandler(t, func(h *Handler) {
		h.VisitorMetrics = vf
		h.UserService = &sessionUserFake{byToken: map[string]*users.User{"sess-1": {ID: "u1"}}}
	})
	return h, vf
}

func visitReq(body string, cookies ...*http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/public/visits", strings.NewReader(body))
	r.Header.Set("User-Agent", browserUA)
	r.Header.Set("Origin", "https://openv.app")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	return r
}

func visitorCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == VisitorCookieName {
			return c
		}
	}
	return nil
}

func TestSiteVisitMintsAVisitorAndKeepsIt(t *testing.T) {
	h, vf := newVisitHandler(t)
	w := httptest.NewRecorder()
	h.RecordSiteVisit(w, visitReq(`{"path":"/pricing","referrer":"https://www.google.com/search?q=x","utm_source":""}`))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d", w.Code)
	}
	c := visitorCookie(w)
	if c == nil || !c.HttpOnly || c.MaxAge <= 0 {
		t.Fatalf("visitor cookie = %+v", c)
	}
	if len(vf.visits) != 1 || vf.visits[0].VisitorID != c.Value || vf.visits[0].Source != "google.com" || vf.visits[0].UserID != "" {
		t.Fatalf("visits = %+v", vf.visits)
	}

	// The same browser, now signed in: the same visitor, linked to the account.
	w = httptest.NewRecorder()
	h.RecordSiteVisit(w, visitReq(`{"path":"/projects"}`, c, &http.Cookie{Name: SessionCookieName, Value: "sess-1"}))
	if got := visitorCookie(w); got == nil || got.Value != c.Value {
		t.Errorf("visitor id changed: %+v", got)
	}
	if len(vf.visits) != 2 || vf.visits[1].VisitorID != c.Value || vf.visits[1].UserID != "u1" {
		t.Errorf("signed-in visit = %+v", vf.visits)
	}
}

func TestSiteVisitReplacesAMalformedVisitorID(t *testing.T) {
	h, vf := newVisitHandler(t)
	w := httptest.NewRecorder()
	h.RecordSiteVisit(w, visitReq(`{}`, &http.Cookie{Name: VisitorCookieName, Value: "not-a-uuid"}))
	if len(vf.visits) != 1 || vf.visits[0].VisitorID == "not-a-uuid" || len(vf.visits[0].VisitorID) != 36 {
		t.Errorf("visits = %+v", vf.visits)
	}
}

func TestSiteVisitCountsNoBotsAndHonoursPrivacySignals(t *testing.T) {
	h, vf := newVisitHandler(t)
	cases := map[string]func(r *http.Request){
		"bot": func(r *http.Request) { r.Header.Set("User-Agent", "Googlebot/2.1") },
		"gpc": func(r *http.Request) { r.Header.Set("Sec-GPC", "1") },
		"dnt": func(r *http.Request) { r.Header.Set("DNT", "1") },
	}
	for name, edit := range cases {
		r := visitReq(`{"path":"/"}`)
		edit(r)
		w := httptest.NewRecorder()
		h.RecordSiteVisit(w, r)
		if w.Code != http.StatusNoContent || visitorCookie(w) != nil {
			t.Errorf("%s: status %d, cookie %v; want 204 and no cookie", name, w.Code, visitorCookie(w))
		}
	}
	if len(vf.visits) != 0 {
		t.Errorf("visits = %+v, want none", vf.visits)
	}
}

func TestSiteVisitIsRateLimited(t *testing.T) {
	h, vf := newVisitHandler(t)
	h.visitLimiter = newRateLimiter(defaultVisitBurst, defaultVisitRefill)
	for i := 0; i < defaultVisitBurst+5; i++ {
		h.RecordSiteVisit(httptest.NewRecorder(), visitReq(`{}`))
	}
	if len(vf.visits) != defaultVisitBurst {
		t.Errorf("visits = %d, want the burst of %d", len(vf.visits), defaultVisitBurst)
	}
}

func TestAdminVisitorMetricsIsGated(t *testing.T) {
	h, _ := newVisitHandler(t)
	for name, tc := range map[string]struct {
		user *users.User
		want int
	}{
		"visitor": {nil, http.StatusUnauthorized},
		"member":  {&users.User{ID: "dave"}, http.StatusForbidden},
		"admin":   {&users.User{ID: "root", IsAdmin: true}, http.StatusOK},
	} {
		w := httptest.NewRecorder()
		h.AdminVisitorMetrics(w, adminReq(http.MethodGet, "/api/v1/admin/metrics/visitors", "", tc.user, nil))
		if w.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", name, w.Code, tc.want)
		}
		if tc.want == http.StatusOK && !strings.Contains(w.Body.String(), `"conversion_rate_30d":0.2`) {
			t.Errorf("body = %s", w.Body.String())
		}
	}
}
