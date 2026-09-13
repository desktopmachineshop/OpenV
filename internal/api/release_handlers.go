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
	router.HandleFunc("/api/v1/public/release", h.GetPublicRelease).Methods("GET")
}

// GetPublicRelease is the release feed dedicated instances poll (REQ-139):
// the running nightly and the newest stable with its cut date, and nothing
// else. Open, since it says only what any member could read.
func (h *Handler) GetPublicRelease(w http.ResponseWriter, r *http.Request) {
	feed := struct {
		Nightly     string `json:"nightly"`
		Stable      string `json:"stable"`
		StableCutOn string `json:"stable_cut_on"`
	}{}
	if h.releaseService != nil {
		if cur := h.releaseService.Current(); cur != nil {
			feed.Nightly = cur.Version
		}
		if s := h.releaseService.CurrentStable(); s != nil {
			feed.Stable, feed.StableCutOn = s.Version, s.CutOn
		}
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(feed)
}

// releaseResponse is the current release plus the whole notes history.
type releaseResponse struct {
	// Version and Date are empty when the build names no release yet.
	Version string         `json:"version"`
	Date    string         `json:"date"`
	Notes   []release.Note `json:"notes"`
	// Markdown is the current release's section; History the whole file.
	Markdown string `json:"markdown"`
	History  string `json:"history"`
	// Stable is the newest stable release, nil until one is cut.
	Stable *release.Stable `json:"stable"`
	// Deployment is "shared" or "dedicated" (OPENV_DEPLOYMENT).
	Deployment string `json:"deployment"`
}

// GetRelease answers the release the server is running and its notes.
func (h *Handler) GetRelease(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r) == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	resp := releaseResponse{Notes: []release.Note{}, Deployment: h.deploymentKind}
	if resp.Deployment == "" {
		resp.Deployment = "shared"
	}
	if h.releaseService != nil {
		resp.History = h.releaseService.Markdown()
		if cur := h.releaseService.Current(); cur != nil {
			resp.Version, resp.Date, resp.Markdown = cur.Version, cur.Date, cur.Markdown
			resp.Notes = append(resp.Notes, cur.Notes...)
		}
		resp.Stable = h.releaseService.CurrentStable()
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

func (s staticRelease) Current() *release.Release             { return s.notes.Current() }
func (s staticRelease) CurrentStable() *release.Stable        { return s.notes.CurrentStable() }
func (s staticRelease) Stable(version string) *release.Stable { return s.notes.Stable(version) }
func (s staticRelease) Markdown() string                      { return s.notes.Markdown }
