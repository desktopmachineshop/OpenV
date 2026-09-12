package api

// The running release. The API learns which release it is from the notes
// it was built with; members read it here for the What's new page and for
// the open-tab check that notices a newer release behind the same URL.

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/release"
)

func (h *Handler) registerReleaseRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/release", h.GetRelease).Methods("GET")
}

// releaseResponse is the current release plus the whole notes history.
type releaseResponse struct {
	// Version and Date are empty when the build names no release yet.
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Notes   []string `json:"notes"`
	// Markdown is the current release's section; History the whole file.
	Markdown string `json:"markdown"`
	History  string `json:"history"`
}

// GetRelease answers the release the server is running and its notes.
func (h *Handler) GetRelease(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r) == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	resp := releaseResponse{Notes: []string{}}
	if h.releaseService != nil {
		resp.History = h.releaseService.Markdown()
		if cur := h.releaseService.Current(); cur != nil {
			resp.Version, resp.Date, resp.Markdown = cur.Version, cur.Date, cur.Markdown
			resp.Notes = append(resp.Notes, cur.Notes...)
		}
	}
	// The version is what an open tab polls; a fresh answer every time is
	// the point.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// releaseServiceFor adapts a parsed notes value, for tests and for a server
// whose notes failed to parse (which serves an empty release rather than
// refusing to boot the API over a documentation file).
type staticRelease struct{ notes *release.Notes }

func (s staticRelease) Current() *release.Release { return s.notes.Current() }
func (s staticRelease) Markdown() string          { return s.notes.Markdown }
