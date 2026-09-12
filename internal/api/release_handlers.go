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

// releaseResponse is the current release plus every earlier one.
type releaseResponse struct {
	// Version and Date are empty when the build names no release yet.
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Notes   []string `json:"notes"`
	// Categories groups the current release's notes for a reader.
	Categories []release.Category `json:"categories"`
	// Markdown is the current release's section as written.
	Markdown string `json:"markdown"`
	// Releases is the history, newest first. It is built from the parsed
	// sections rather than the notes file, so nothing the file carries for
	// contributors — or has not released yet — can reach a customer.
	Releases []release.Release `json:"releases"`
}

// GetRelease answers the release the server is running and its notes.
func (h *Handler) GetRelease(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r) == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	resp := releaseResponse{Notes: []string{}, Releases: []release.Release{}}
	if h.releaseService != nil {
		resp.Releases = append(resp.Releases, h.releaseService.Released()...)
		if cur := h.releaseService.Current(); cur != nil {
			resp.Version, resp.Date, resp.Markdown = cur.Version, cur.Date, cur.Markdown
			resp.Notes = append(resp.Notes, cur.Notes...)
			resp.Categories = cur.Categories
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

func (s staticRelease) Current() *release.Release   { return s.notes.Current() }
func (s staticRelease) Released() []release.Release { return s.notes.Releases }
