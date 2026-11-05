package api

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/todos"
)

// registerTodoRoutes wires to-dos (issue #731): a project's list, a person's
// own list across the workspace, and one to-do's read, edit, tick, delete
// and promotion to a board task.
// Its call is the last in RegisterRoutes (routes.go): gorilla/mux serves
// the first registered route that matches, so the call's position decides
// which of two overlapping templates answers a path (I2).
func (h *Handler) registerTodoRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/todos", h.ListProjectTodos).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/todos", h.CreateProjectTodo).Methods("POST")
	router.HandleFunc("/api/v1/todos", h.ListMyTodos).Methods("GET")
	router.HandleFunc("/api/v1/todos", h.CreatePersonalTodo).Methods("POST")
	router.HandleFunc("/api/v1/todos/{id}", h.GetTodo).Methods("GET")
	router.HandleFunc("/api/v1/todos/{id}", h.UpdateTodo).Methods("PUT")
	router.HandleFunc("/api/v1/todos/{id}", h.DeleteTodo).Methods("DELETE")
	router.HandleFunc("/api/v1/todos/{id}/done", h.SetTodoDone).Methods("POST")
	router.HandleFunc("/api/v1/todos/{id}/promote", h.PromoteTodo).Methods("POST")
}

const todoNotFound = "to-do not found"

// todosAvailable answers 404 where the deployment has no to-do service.
func (h *Handler) todosAvailable(w http.ResponseWriter) bool {
	if h.TodoService == nil {
		writeJSONError(w, http.StatusNotFound, "to-dos are not available")
		return false
	}
	return true
}

// todoCaller is the person a request acts for: the signed-in member, or the
// holder of a personal runner key ("" for a workspace key or a run).
func todoCaller(r *http.Request) string {
	if u := CurrentUser(r); u != nil {
		return u.ID
	}
	return WorkerUser(r)
}

// writeTodoError answers a to-do service error: a not-found as 404, a rule
// the request broke as 400, anything else as 500.
func writeTodoError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, todos.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, todoNotFound)
	case errors.Is(err, todos.ErrTextRequired), errors.Is(err, todos.ErrTextTooLong),
		errors.Is(err, todos.ErrParagraphs), errors.Is(err, todos.ErrNoProject),
		errors.Is(err, todos.ErrNoWorkspace):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, todos.ErrPromoted):
		writeJSONError(w, http.StatusConflict, err.Error())
	default:
		respondInternal(w, r, "failed to save the to-do", err)
	}
}

// loadTodo loads the route's to-do and decides the caller's access to it. A
// project's to-do is its project's: minRole on the project, and a caller with
// none answered as for an id no row has (I3). A personal to-do is its
// writer's alone; to anyone else it does not exist.
func (h *Handler) loadTodo(w http.ResponseWriter, r *http.Request, minRole string) (*todos.Todo, bool) {
	if !h.todosAvailable(w) {
		return nil, false
	}
	t, err := h.TodoService.Get(mux.Vars(r)["id"])
	if err != nil {
		if errors.Is(err, todos.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, todoNotFound)
		} else {
			respondInternal(w, r, "failed to load the to-do", err)
		}
		return nil, false
	}
	if t.ProjectID != nil {
		if !h.requireProjectRoleFor(w, r, *t.ProjectID, minRole, missing(todoNotFound)) {
			return nil, false
		}
		return t, true
	}
	caller := todoCaller(r)
	if caller == "" || t.CreatedBy == nil || *t.CreatedBy != caller {
		writeJSONError(w, http.StatusNotFound, todoNotFound)
		return nil, false
	}
	if !h.planGate(w, r, func() string { return t.OrgID }) {
		return nil, false
	}
	return t, true
}

// assigneeIsMember checks that a project to-do's assignee is a person on the
// project, writing 400 when not. No assignee passes.
func (h *Handler) assigneeIsMember(w http.ResponseWriter, projectID string, assigneeID *string) bool {
	if assigneeID == nil || *assigneeID == "" || h.MemberService == nil {
		return true
	}
	role, err := h.MemberService.EffectiveRole(projectID, *assigneeID)
	if err != nil || role == "" {
		writeJSONError(w, http.StatusBadRequest, "the assignee is not a member of this project")
		return false
	}
	return true
}

// ListProjectTodos answers a project's to-dos to its readers, open ones
// first, soonest due first, then the done ones.
func (h *Handler) ListProjectTodos(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	if !h.todosAvailable(w) {
		return
	}
	list, err := h.TodoService.ListByProject(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load to-dos", err)
		return
	}
	if list == nil {
		list = []*todos.Todo{}
	}
	respondJSON(w, http.StatusOK, list)
}

// CreateProjectTodo adds a to-do to a project, for an editor, where the
// workspace has the feature. One raised from a note names the note, which
// must be on an artifact of this project; the artifact it hangs off becomes
// the to-do's source artifact.
func (h *Handler) CreateProjectTodo(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	if !h.todosAvailable(w) {
		return
	}
	if !h.projectFeatureEnabled(r, projectID, release.FeatureTodos) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	var req todos.CreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.SourceChatterID != nil && *req.SourceChatterID != "" {
		// Checked rather than trusted, as for a work item raised from a
		// note: another project's note id would put this project's to-do
		// on that project's note. A lookup failure answers no.
		var entry *chatter.ChatterEntry
		if h.ChatterService != nil {
			entry, _ = h.ChatterService.GetEntry(*req.SourceChatterID)
		}
		if entry == nil || h.projectIDForArtifact(entry.ArtifactID) != projectID {
			writeJSONError(w, http.StatusBadRequest, "source note is not in this project")
			return
		}
		req.SourceArtifactID = &entry.ArtifactID
	} else if req.SourceArtifactID != nil && *req.SourceArtifactID != "" {
		if h.projectIDForArtifact(*req.SourceArtifactID) != projectID {
			writeJSONError(w, http.StatusBadRequest, "source artifact is not in this project")
			return
		}
	}
	if !h.assigneeIsMember(w, projectID, req.AssigneeID) {
		return
	}
	req.ProjectID = &projectID
	req.OrgID = h.orgIDForProject(projectID)
	t, err := h.TodoService.Create(req, CurrentUserID(r))
	if err != nil {
		writeTodoError(w, r, err)
		return
	}
	respondJSON(w, http.StatusCreated, t)
}

// ListMyTodos answers the caller's to-dos in the request's workspace: the
// project to-dos assigned to them, in projects they can still read, and
// their personal ones.
func (h *Handler) ListMyTodos(w http.ResponseWriter, r *http.Request) {
	caller := todoCaller(r)
	if caller == "" {
		writeJSONError(w, http.StatusUnauthorized, "sign in to see your to-dos")
		return
	}
	if !h.todosAvailable(w) {
		return
	}
	orgID := ActiveOrg(r)
	list, err := h.TodoService.ListForUser(orgID, caller)
	if err != nil {
		respondInternal(w, r, "failed to load to-dos", err)
		return
	}
	reach := map[string]bool{}
	out := make([]*todos.Todo, 0, len(list))
	for _, t := range list {
		if t.ProjectID != nil {
			ok, seen := reach[*t.ProjectID]
			if !seen {
				ok = h.reachesProject(r, *t.ProjectID)
				reach[*t.ProjectID] = ok
			}
			if !ok {
				continue
			}
		}
		out = append(out, t)
	}
	respondJSON(w, http.StatusOK, out)
}

// CreatePersonalTodo adds a personal to-do in the request's workspace: one
// with no project, which only its writer sees.
func (h *Handler) CreatePersonalTodo(w http.ResponseWriter, r *http.Request) {
	caller := todoCaller(r)
	if caller == "" {
		writeJSONError(w, http.StatusUnauthorized, "sign in to add a to-do")
		return
	}
	if !h.todosAvailable(w) {
		return
	}
	orgID := ActiveOrg(r)
	if !h.featureEnabled(r, orgID, release.FeatureTodos) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	if !h.requireWritable(w, r, orgID) {
		return
	}
	var req todos.CreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.OrgID = orgID
	req.ProjectID = nil
	req.SourceArtifactID = nil
	req.SourceChatterID = nil
	t, err := h.TodoService.Create(req, &caller)
	if err != nil {
		writeTodoError(w, r, err)
		return
	}
	respondJSON(w, http.StatusCreated, t)
}

// GetTodo answers one to-do to whoever may read it.
func (h *Handler) GetTodo(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadTodo(w, r, members.RoleViewer)
	if !ok {
		return
	}
	respondJSON(w, http.StatusOK, t)
}

// UpdateTodo replaces a to-do's text, assignee and due date.
func (h *Handler) UpdateTodo(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadTodo(w, r, members.RoleEditor)
	if !ok {
		return
	}
	var req todos.UpdateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if t.ProjectID != nil && !h.assigneeIsMember(w, *t.ProjectID, req.AssigneeID) {
		return
	}
	updated, err := h.TodoService.Update(t.ID, req)
	if err != nil {
		writeTodoError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// SetTodoDone ticks a to-do off, or opens it again.
func (h *Handler) SetTodoDone(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadTodo(w, r, members.RoleEditor)
	if !ok {
		return
	}
	var req todos.DoneRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	updated, err := h.TodoService.SetDone(t.ID, req.Done)
	if err != nil {
		writeTodoError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

// DeleteTodo removes a to-do.
func (h *Handler) DeleteTodo(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadTodo(w, r, members.RoleEditor)
	if !ok {
		return
	}
	if err := h.TodoService.Delete(t.ID); err != nil {
		respondInternal(w, r, "failed to delete the to-do", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PromoteTodo makes a board task of a project's to-do and ticks the to-do
// off as promoted. It answers both.
func (h *Handler) PromoteTodo(w http.ResponseWriter, r *http.Request) {
	t, ok := h.loadTodo(w, r, members.RoleEditor)
	if !ok {
		return
	}
	updated, item, err := h.TodoService.Promote(t.ID, CurrentUserID(r), Actor(r))
	if err != nil {
		writeTodoError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"todo":      updated,
		"work_item": item,
	})
}
