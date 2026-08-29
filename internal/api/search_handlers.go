package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 50
)

// GlobalSearch handles GET /api/v1/search?q=&limit= — a cross-project
// artifact search over titles and bodies, scoped to the caller's active
// workspace. Org admins (and platform admins) search every project in the
// workspace; plain members only the projects they can access (viewer role
// suffices, mirroring ListDomainEvents/ListAgentRuns).
func (h *Handler) GlobalSearch(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}

	params := r.URL.Query()
	query := strings.TrimSpace(params.Get("q"))
	limit, _ := strconv.Atoi(params.Get("limit"))
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	w.Header().Set("Content-Type", "application/json")
	if query == "" {
		json.NewEncoder(w).Encode([]*artifacts.SearchHit{})
		return
	}

	projectList, err := h.projectService.ListProjects()
	if err != nil {
		respondInternal(w, r, "failed to resolve searchable projects", err)
		return
	}

	// Scope to the active workspace.
	activeOrg := ActiveOrg(r)
	if activeOrg != "" {
		inOrg := projectList[:0]
		for _, p := range projectList {
			if p.OrgID == activeOrg {
				inOrg = append(inOrg, p)
			}
		}
		projectList = inOrg
	}

	// Plain members only search projects they can access.
	if !h.isOrgAdmin(r, activeOrg) {
		user := CurrentUser(r)
		allowed := map[string]bool{}
		if h.memberService != nil {
			ids, err := h.memberService.ProjectIDsForUser(user.ID)
			if err != nil {
				respondInternal(w, r, "failed to resolve project memberships", err)
				return
			}
			for _, id := range ids {
				allowed[id] = true
			}
		}
		accessible := projectList[:0]
		for _, p := range projectList {
			if allowed[p.ID] {
				accessible = append(accessible, p)
			}
		}
		projectList = accessible
	}

	if len(projectList) == 0 {
		json.NewEncoder(w).Encode([]*artifacts.SearchHit{})
		return
	}

	projectIDs := make([]string, 0, len(projectList))
	names := make(map[string]string, len(projectList))
	for _, p := range projectList {
		projectIDs = append(projectIDs, p.ID)
		names[p.ID] = p.Name
	}

	hits, err := h.artifactService.SearchArtifacts(projectIDs, query, limit)
	if err != nil {
		respondInternal(w, r, "search failed", err)
		return
	}
	if hits == nil {
		hits = []*artifacts.SearchHit{}
	}
	for _, hit := range hits {
		hit.ProjectName = names[hit.ProjectID]
	}

	json.NewEncoder(w).Encode(hits)
}
