package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

func (h *Handler) CreateWorkItem(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	var req workitems.CreateWorkItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ProjectID = projectID
	// A to-do may be raised from a note, and says which one so the note can
	// show its status. The claim is checked rather than trusted: an id
	// belonging to another project would put this project's work item title
	// on that project's note.
	if req.SourceChatterID != nil {
		if *req.SourceChatterID == "" {
			req.SourceChatterID = nil
		} else if !h.noteBelongsToProject(*req.SourceChatterID, projectID) {
			writeJSONError(w, http.StatusBadRequest, "source note is not in this project")
			return
		}
	}
	if !h.assigneeCrewChecked(w, r, projectID, req.AssigneeType, req.AssigneeID) {
		return
	}
	item, err := h.workItemService.Create(req, CurrentUserID(r), Actor(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(item)
}

// noteBelongsToProject reports whether a chatter entry exists and hangs off
// an artifact in the given project. A lookup failure answers no: refusing a
// link OpenV cannot vouch for is the safe direction.
func (h *Handler) noteBelongsToProject(chatterID, projectID string) bool {
	if h.chatterService == nil {
		return false
	}
	entry, err := h.chatterService.GetEntry(chatterID)
	if err != nil || entry == nil {
		return false
	}
	return h.projectIDForArtifact(entry.ArtifactID) == projectID
}

func (h *Handler) ListWorkItems(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	items, err := h.workItemService.ListByProject(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list work items", err)
		return
	}
	json.NewEncoder(w).Encode(items)
}

func (h *Handler) GetWorkItem(w http.ResponseWriter, r *http.Request) {
	item, activity, err := h.workItemService.GetWithActivity(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "work item not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, item.ProjectID, members.RoleViewer, missing("work item not found")) {
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"item":     item,
		"activity": activity,
	})
}

func (h *Handler) UpdateWorkItem(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	item, err := h.workItemService.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "work item not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, item.ProjectID, members.RoleEditor, missing("work item not found")) {
		return
	}
	var req workitems.UpdateWorkItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// An update that sends no assignee_type keeps the item's. One that
	// re-sends the item's own assignee stores nothing new, so it is not
	// checked again: an editor who may not know of the crew keeps it.
	assigneeType := req.AssigneeType
	if assigneeType == "" {
		assigneeType = item.AssigneeType
	}
	unchanged := assigneeType == item.AssigneeType && req.AssigneeID != nil && item.AssigneeID != nil && *req.AssigneeID == *item.AssigneeID
	if !unchanged && !h.assigneeCrewChecked(w, r, item.ProjectID, assigneeType, req.AssigneeID) {
		return
	}
	updated, err := h.workItemService.Update(id, req, Actor(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) DeleteWorkItem(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	item, err := h.workItemService.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "work item not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, item.ProjectID, members.RoleEditor, missing("work item not found")) {
		return
	}
	if err := h.workItemService.Delete(id); err != nil {
		respondInternal(w, r, "failed to delete work item", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MoveWorkItem(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	item, err := h.workItemService.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "work item not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, item.ProjectID, members.RoleEditor, missing("work item not found")) {
		return
	}
	var req workitems.MoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	moved, err := h.workItemService.Move(id, req, Actor(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(moved)
}

func (h *Handler) CommentWorkItem(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	item, err := h.workItemService.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "work item not found", err)
		return
	}
	if !h.requireProjectRoleFor(w, r, item.ProjectID, members.RoleViewer, missing("work item not found")) {
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	activity, err := h.workItemService.AddComment(id, req.Content, Actor(r))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(activity)
}
