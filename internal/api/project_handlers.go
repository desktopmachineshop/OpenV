package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/release"
)

// CreateProject creates a new project
func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	// A person, in the workspace it acts in, past its plan gate and project
	// maximum (requireProjectCreate): a run or a runner key creates none.
	orgID, ok := h.requireProjectCreate(w, r)
	if !ok {
		return
	}
	var req projects.CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	project := projects.NewProject(req)
	project.OrgID = orgID
	if err := h.projectService.CreateProject(project); err != nil {
		respondInternal(w, r, "failed to create project", err)
		return
	}

	// Creator becomes the project owner.
	if user := CurrentUser(r); user != nil && h.memberService != nil {
		if err := h.memberService.AddMember(project.ID, user.ID, members.RoleOwner); err != nil {
			slog.Warn("api: failed to add creator as project owner", "project_id", project.ID, "error", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(project)
}

// GetProject retrieves a project by ID. The guard comes first, so that a
// project the caller cannot reach answers as one no row has (I3).
func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, id, members.RoleViewer) {
		return
	}
	project, err := h.projectService.GetProject(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "project not found", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(project)
}

// ListProjects lists the projects visible to the caller within the active
// workspace: all of the org's projects for platform admins, org admins and
// workspace runner keys, membership-filtered otherwise (a personal runner key
// by its holder's), and to an agent run's token its own project alone, the
// one project it acts in (requireProjectRole).
func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	// Scope to the active workspace in SQL, and fail closed: a caller whose
	// active org could not be resolved (empty) sees no projects rather than
	// every tenant's. resolveActiveOrg already falls back to the user's
	// personal org, so "" here means even that failed.
	activeOrg := ActiveOrg(r)
	if activeOrg == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]*projects.Project{})
		return
	}

	projectList, err := h.projectService.ListProjectsByOrg(activeOrg)
	if err != nil {
		respondInternal(w, r, "failed to list projects", err)
		return
	}

	if run := CurrentRun(r); run != nil {
		own := make([]*projects.Project, 0, 1)
		for _, p := range projectList {
			if run.ProjectID != nil && p.ID == *run.ProjectID {
				own = append(own, p)
			}
		}
		projectList = own
	}

	// A member sees the projects they have a role in, and so does their
	// personal runner key, which is its holder acting (REQ-16); a platform
	// admin's session and a workspace key (REQ-42) see them all.
	person := WorkerUser(r)
	if user := CurrentUser(r); user != nil && !user.IsAdmin {
		person = user.ID
	}
	if person != "" && h.memberService != nil {
		// Org admins of the active workspace see all of its projects.
		isOrgAdmin := false
		if h.orgService != nil {
			if role, err := h.orgService.RoleInOrg(activeOrg, person); err == nil && role == orgs.RoleAdmin {
				isOrgAdmin = true
			}
		}
		if !isOrgAdmin {
			ids, err := h.memberService.ProjectIDsForUser(person)
			if err != nil {
				respondInternal(w, r, "failed to list projects", err)
				return
			}
			allowed := map[string]bool{}
			for _, id := range ids {
				allowed[id] = true
			}
			filtered := projectList[:0]
			for _, p := range projectList {
				if allowed[p.ID] {
					filtered = append(filtered, p)
				}
			}
			projectList = filtered
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projectList)
}

// UpdateProject updates a project
func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, id, members.RoleEditor) {
		return
	}

	var req projects.UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ParentProjectID != nil && !h.projectFeatureEnabled(r, id, release.FeatureFlowDown) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	// A parent the caller cannot reach at all is one no row has (I3).
	if p := req.ParentProjectID; p != nil && *p != "" && !h.requireProjectVisible(w, r, *p, parentNotFound) {
		return
	}

	project, err := h.projectService.UpdateProject(id, req)
	if err != nil {
		switch {
		case errors.Is(err, projects.ErrParentNotFound), errors.Is(err, projects.ErrParentOtherOrg),
			errors.Is(err, projects.ErrParentIsSelf), errors.Is(err, projects.ErrParentIsDescendant):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		default:
			respondInternal(w, r, "failed to update project", err)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(project)
}

// parentNotFound answers a flow-down parent no row has, or one the caller
// cannot reach at all, in projects.ErrParentNotFound's words; one it reaches
// in another workspace is still refused as such.
var parentNotFound = notFound{http.StatusBadRequest, "parent project not found"}

// ListChildProjects answers the projects filed under this one (REQ-144),
// for a settings page and for the flow-down picker. Viewer rights on the
// parent suffice: the children's names are what the parent's members see
// on every refined requirement anyway.
func (h *Handler) ListChildProjects(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, id, members.RoleViewer) {
		return
	}
	children, err := h.projectService.ListChildren(id)
	if err != nil {
		respondInternal(w, r, "failed to list child projects", err)
		return
	}
	if children == nil {
		children = []*projects.Project{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(children)
}

// ListLinkedArtifacts answers the far end of every link crossing out of the
// project (REQ-145): the parent requirements local ones refine and the
// child requirements refining local ones, named with their project, so the
// module view can show them without rights on those projects.
func (h *Handler) ListLinkedArtifacts(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, id, members.RoleViewer) {
		return
	}
	linked, err := h.exportService.LinkedArtifacts(id)
	if err != nil {
		respondInternal(w, r, "failed to resolve linked artifacts", err)
		return
	}
	if linked == nil {
		linked = []*exports.LinkedArtifact{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(linked)
}

// DeleteProject deletes a project
func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, id, members.RoleOwner) {
		return
	}

	err := h.projectService.DeleteProject(id)
	if err != nil {
		respondInternal(w, r, "failed to delete project", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
