package api

import (
	"net/http"

	"github.com/gorilla/mux"
)

// registerHealthRoutes wires the health check.
func (h *Handler) registerHealthRoutes(router *mux.Router) {
	router.HandleFunc("/health", h.Health).Methods("GET")
}

// Health returns the health status, and the commit this binary was built
// from where the build told it one. The commit is what lets a deployment be
// matched to a revision: the staging smoke gate waits for it to equal the
// commit under test before running, so a green run cannot be a stale build's
// (REQ-141). It is omitted entirely when unknown, leaving the answer exactly
// as it was for local runs and the compose stack.
//
// This lives on /health rather than the release feed because /health is
// already unauthenticated (authmiddleware.go), unlogged (requestlog.go) and
// uncached, while GET /api/v1/public/release is deliberately cached for five
// minutes for the dedicated instances that poll it.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	body := map[string]string{"status": "ok"}
	if h.BuildSHA != "" {
		body["commit"] = h.BuildSHA
	}
	writeJSONOK(w, body)
}
