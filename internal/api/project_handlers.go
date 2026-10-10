package api

import (
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

// registerProjectCoreRoutes wires a project's create, list, read, update
// and delete. It is the first registrar RegisterRoutes calls; the project's
// other routes come from registrars interleaved with the download, share,
// admin and billing ones, in the order route_handlers.txt pins.
func (h *Handler) registerProjectCoreRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects", h.CreateProject).Methods("POST")
	router.HandleFunc("/api/v1/projects", h.ListProjects).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}", h.GetProject).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}", h.UpdateProject).Methods("PUT")
	router.HandleFunc("/api/v1/projects/{id}", h.alwaysWritable(h.DeleteProject)).Methods("DELETE")
}

// registerProjectChildRoutes wires the projects filed under a project
// (REQ-144).
func (h *Handler) registerProjectChildRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/children", h.ListChildProjects).Methods("GET")
}

// registerLinkedArtifactRoutes wires the far ends of the links crossing out
// of a project (REQ-145).
func (h *Handler) registerLinkedArtifactRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/linked-artifacts", h.ListLinkedArtifacts).Methods("GET")
}

// CreateProject creates a new project
func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	// A person, in the workspace it acts in, past its plan gate and project
	// maximum (requireProjectCreate): a run or a runner key creates none.
	orgID, ok := h.requireProjectCreate(w, r)
	if !ok {
		return
	}
	var req projects.CreateProjectRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	project := projects.NewProject(req)
	project.OrgID = orgID
	if err := h.ProjectService.CreateProject(project); err != nil {
		respondInternal(w, r, "failed to create project", err)
		return
	}

	// Creator becomes the project owner.
	if user := CurrentUser(r); user != nil && h.MemberService != nil {
		if err := h.MemberService.AddMember(project.ID, user.ID, members.RoleOwner); err != nil {
			slog.Warn("api: failed to add creator as project owner", "project_id", project.ID, "error", err)
		}
	}

	writeJSON(w, http.StatusCreated, project)
}

// GetProject retrieves a project by ID. The guard comes first, so that a
// project the caller cannot reach answers as one no row has (I3).
func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, id, members.RoleViewer) {
		return
	}
	project, err := h.ProjectService.GetProject(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "project not found", err)
		return
	}

	writeJSONOK(w, project)
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
		writeJSONOK(w, []*projects.Project{})
		return
	}

	projectList, err := h.ProjectService.ListProjectsByOrg(activeOrg)
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
	if person != "" && h.MemberService != nil {
		// Org admins of the active workspace see all of its projects.
		isOrgAdmin := false
		if h.OrgService != nil {
			if role, err := h.OrgService.RoleInOrg(activeOrg, person); err == nil && role == orgs.RoleAdmin {
				isOrgAdmin = true
			}
		}
		if !isOrgAdmin {
			ids, err := h.MemberService.ProjectIDsForUser(person)
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

	writeJSONOK(w, projectList)
}

// UpdateProject updates a project
func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, id, members.RoleEditor) {
		return
	}

	var req projects.UpdateProjectRequest
	if !decodeJSON(w, r, &req) {
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

	project, err := h.ProjectService.UpdateProject(id, req)
	if err != nil {
		switch {
		case errors.Is(err, projects.ErrNotFound):
			// Gone since the guard read it: as for an id no row has (#379
			// bug 88; it answered 500).
			unknownProject.write(w)
		case errors.Is(err, projects.ErrParentNotFound), errors.Is(err, projects.ErrParentOtherOrg),
			errors.Is(err, projects.ErrParentIsSelf), errors.Is(err, projects.ErrParentIsDescendant):
			writeJSONError(w, http.StatusBadRequest, err.Error())
		default:
			respondInternal(w, r, "failed to update project", err)
		}
		return
	}

	writeJSONOK(w, project)
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
	children, err := h.ProjectService.ListChildren(id)
	if err != nil {
		respondInternal(w, r, "failed to list child projects", err)
		return
	}
	if children == nil {
		children = []*projects.Project{}
	}
	writeJSONOK(w, children)
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
	linked, err := h.ExportService.LinkedArtifacts(id)
	if err != nil {
		respondInternal(w, r, "failed to resolve linked artifacts", err)
		return
	}
	if linked == nil {
		linked = []*exports.LinkedArtifact{}
	}
	writeJSONOK(w, linked)
}

// DeleteProject deletes a project and everything that belongs to it alone
// (#379 bug 86), cancelling its live agent runs as it goes (bug 137). Once
// the delete has committed, it announces the runs it cancelled, as a cancel
// does, and removes the stored files of the figures and evidence it deleted
// (bug 136). A file that will not go is logged, not answered: the delete
// happened, and the project is gone either way.
func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, id, members.RoleOwner) {
		return
	}

	removed, err := h.ProjectService.DeleteProject(id)
	if errors.Is(err, projects.ErrNotFound) {
		// Gone since the guard read it: as for an id no row has (#379 bug
		// 88; it answered 500).
		unknownProject.write(w)
		return
	}
	if err != nil {
		respondInternal(w, r, "failed to delete project", err)
		return
	}
	if removed != nil {
		if h.RunService != nil {
			h.RunService.AnnounceCancelled(removed.CancelledRuns)
		}
		removeStoredFiles(removed.Files)
	}

	w.WriteHeader(http.StatusNoContent)
}
