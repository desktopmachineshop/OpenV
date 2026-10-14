package api

// Site visitors (the platform admin's visitor dashboard): the beacon the
// app sends once per page load, and the dashboard that counts visitors and
// their conversion to accounts. The beacon is public, answers 204 whatever
// it did, and never fails a page: see usermetrics/visitors.go for what is
// kept and what is not.

import (
	"net/http"
	"net/url"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/usermetrics"
)

// visitBeaconMaxBytes bounds the beacon's body: a path, a referrer and a
// utm_source.
const visitBeaconMaxBytes = 4 << 10

// privacySignal reports whether the browser asked not to be tracked, by
// Global Privacy Control or Do Not Track.
func privacySignal(r *http.Request) bool {
	return r.Header.Get("Sec-GPC") == "1" || r.Header.Get("DNT") == "1"
}

// siteHost is the host of the page that sent the beacon: the Origin, since
// the API may answer on a host of its own, else the request's Host.
func siteHost(r *http.Request) string {
	if u, err := url.Parse(r.Header.Get("Origin")); err == nil && u.Host != "" {
		return u.Host
	}
	return r.Host
}

// RecordSiteVisit is POST /api/v1/public/visits {path, referrer,
// utm_source}. It names the browser by its visitor cookie, minting one on
// its first visit, and counts the visit, or, when the browser is signed in,
// links the visitor to the account. A bot, a browser that sent a privacy
// signal or one over the rate limit is answered the same and not counted.
func (h *Handler) RecordSiteVisit(w http.ResponseWriter, r *http.Request) {
	if h.VisitorMetrics == nil || privacySignal(r) || usermetrics.IsBot(r.UserAgent()) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if ok, _ := h.visitLimiter.allow(clientIP(r)); !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var req struct {
		Path      string `json:"path"`
		Referrer  string `json:"referrer"`
		UTMSource string `json:"utm_source"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, visitBeaconMaxBytes)
	if !decodeJSON(w, r, &req) {
		return
	}
	visitorID := ""
	if c, err := r.Cookie(VisitorCookieName); err == nil {
		if id, err := uuid.Parse(c.Value); err == nil {
			visitorID = id.String()
		}
	}
	if visitorID == "" {
		visitorID = uuid.NewString()
	}
	// Refreshed on every beacon, so a returning visitor keeps its id for a
	// year after its last visit.
	h.setVisitorCookie(w, visitorID)
	v := usermetrics.Visit{
		VisitorID: visitorID,
		Path:      req.Path,
		Source:    usermetrics.NormalizeSource(req.UTMSource, req.Referrer, siteHost(r)),
		Country:   r.Header.Get(CountryHeader),
	}
	if u := h.sessionUser(r); u != nil {
		v.UserID = u.ID
	}
	h.VisitorMetrics.RecordVisit(v)
	w.WriteHeader(http.StatusNoContent)
}

// AdminVisitorMetrics answers the visitor dashboard: unique and new
// visitors, their conversion to accounts, and where they land and come
// from.
func (h *Handler) AdminVisitorMetrics(w http.ResponseWriter, r *http.Request) {
	if h.requirePlatformAdmin(w, r, "platform admins only", http.StatusUnauthorized) == nil {
		return
	}
	if h.VisitorMetrics == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "visitor metrics are not available")
		return
	}
	d, err := h.VisitorMetrics.Dashboard()
	if err != nil {
		respondInternal(w, r, "failed to build the visitor dashboard", err)
		return
	}
	respondJSON(w, http.StatusOK, d)
}
