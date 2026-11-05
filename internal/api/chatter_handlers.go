package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/mentions"
	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// registerChatterRoutes wires an artifact's chatter feed.
func (h *Handler) registerChatterRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/chatter", h.CreateChatterEntry).Methods("POST")
	router.HandleFunc("/api/v1/chatter", h.ListChatterEntries).Methods("GET")
}

// CreateChatterEntry creates a new chatter entry
func (h *Handler) CreateChatterEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ArtifactID string `json:"artifact_id"`
		Message    string `json:"message"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	// A reviewer may comment: that is what the role is for.
	if !h.requireProjectRole(w, r, h.projectIDForArtifact(req.ArtifactID), members.RoleReviewer) {
		return
	}

	// Agent-authored comments are marked as auto entries with the agent type
	// so the feed renders them distinctly. Authorship is stamped at write time:
	// a run-authored comment is attributed to the agent, otherwise to the
	// authenticated user (created_by carries the user id; author_name is the
	// display label the feed shows without a follow-up lookup).
	entry := chatter.NewChatterEntry(req.ArtifactID, req.Message, false, "comment")
	if run := CurrentRun(r); run != nil {
		entry.IsAutoEntry = true
		entry.EntryType = "agent"
		if run.AgentName != "" {
			entry.AuthorName = run.AgentName
		} else {
			entry.AuthorName = "Agent"
		}
	} else if user := CurrentUser(r); user != nil {
		entry.CreatedBy = &user.ID
		if user.Name != "" {
			entry.AuthorName = user.Name
		} else {
			entry.AuthorName = user.Email
		}
	}
	if err := h.ChatterService.CreateEntry(entry); err != nil {
		respondInternal(w, r, "failed to create chatter entry", err)
		return
	}

	h.publish(r, events.ChatterCreated, h.projectIDForArtifact(req.ArtifactID), entry.ID, map[string]interface{}{
		"artifact_id": req.ArtifactID,
		"entry_type":  entry.EntryType,
		// The message text rides along so the notification fan-out can scan
		// for @mentions without a chatter lookup.
		"message": entry.Message,
	})

	writeJSONOK(w, entry)
}

// ListChatterEntries lists chatter entries for an artifact
func (h *Handler) ListChatterEntries(w http.ResponseWriter, r *http.Request) {
	artifactID := r.URL.Query().Get("artifact_id")
	if artifactID == "" {
		writeJSONError(w, http.StatusBadRequest, "artifact_id is required")
		return
	}

	if !h.requireProjectRole(w, r, h.projectIDForArtifact(artifactID), members.RoleViewer) {
		return
	}

	entries, err := h.ChatterService.GetEntriesByArtifactID(artifactID)
	if err != nil {
		respondInternal(w, r, "failed to load chatter entries", err)
		return
	}

	h.decorateChatterEntries(h.projectIDForArtifact(artifactID), entries)

	writeJSONOK(w, entries)
}

// decorateChatterEntries fills in the display-only fields of a notes feed:
// who each note names, and the to-do raised from it if there is one.
//
// Both are looked up once for the whole feed rather than per note, and a
// failure on either leaves the feed intact: a missing mention chip or
// to-do link is a worse page, while a 500 is no page at all, and neither
// is worth failing a comment thread over.
func (h *Handler) decorateChatterEntries(projectID string, entries []*chatter.ChatterEntry) {
	if len(entries) == 0 {
		return
	}

	// One membership read serves both halves: who the notes name, and what
	// to call the person a to-do is assigned to.
	var list []*members.Member
	if projectID != "" && h.MemberService != nil {
		var err error
		if list, err = h.MemberService.ListMembers(projectID); err != nil {
			slog.Error("chatter: failed to list members for mentions",
				"project_id", projectID, "error", err)
			list = nil
		}
	}
	names := make(map[string]string, len(list))
	for _, m := range list {
		names[m.UserID] = memberDisplayName(m)
	}
	for _, e := range entries {
		for _, m := range mentions.Resolve(e.Message, list) {
			e.Mentions = append(e.Mentions, chatter.MentionRef{
				UserID: m.UserID,
				Name:   names[m.UserID],
			})
		}
	}

	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	byChatter := h.workItemTodoRefs(projectID, ids, names)
	// A to-do of its own (issue #731) is the record of a to-do raised since
	// the todos feature; a note raised before it keeps showing its board
	// card, so nothing already written loses its link.
	for id, ref := range h.todoRefs(projectID, ids, names) {
		byChatter[id] = ref
	}
	for _, e := range entries {
		if ref, ok := byChatter[e.ID]; ok {
			e.Todo = ref
		}
	}
}

// todoRefs is each note's to-do from the to-do table, keyed by note id.
func (h *Handler) todoRefs(projectID string, ids []string, names map[string]string) map[string]*chatter.TodoRef {
	out := map[string]*chatter.TodoRef{}
	if h.TodoService == nil {
		return out
	}
	list, err := h.TodoService.ListBySourceChatterIDs(ids)
	if err != nil {
		slog.Error("chatter: failed to load to-dos raised from notes",
			"project_id", projectID, "error", err)
		return out
	}
	for _, t := range list {
		// As for a work item below: a note shows a to-do of its own
		// project only.
		if t.SourceChatterID == nil || t.ProjectID == nil || (projectID != "" && *t.ProjectID != projectID) {
			continue
		}
		ref := &chatter.TodoRef{
			TodoID:     t.ID,
			Title:      t.Text,
			Status:     t.Status(),
			AssigneeID: t.AssigneeID,
		}
		if t.AssigneeID != nil {
			ref.AssigneeName = names[*t.AssigneeID]
		}
		out[*t.SourceChatterID] = ref
	}
	return out
}

// workItemTodoRefs is each note's to-do from the board: the work item a
// note raised before to-dos had a table of their own, keyed by note id.
func (h *Handler) workItemTodoRefs(projectID string, ids []string, names map[string]string) map[string]*chatter.TodoRef {
	byChatter := map[string]*chatter.TodoRef{}
	if h.WorkItemService == nil {
		return byChatter
	}
	items, err := h.WorkItemService.ListBySourceChatterIDs(ids)
	if err != nil {
		slog.Error("chatter: failed to load to-dos raised from notes",
			"project_id", projectID, "error", err)
		return byChatter
	}

	// An item assigned to an agent or a team, or to someone since removed
	// from the project, shows its status without a name rather than not
	// showing at all.
	for _, item := range items {
		if item.SourceChatterID == nil {
			continue
		}
		// A note only ever shows a to-do from its own project. The write
		// path refuses a foreign note id, so this cannot normally happen;
		// it is here because the cost of being wrong is one project's work
		// item title rendered inside another's, which is not a mistake to
		// leave to a single check.
		if projectID != "" && item.ProjectID != projectID {
			continue
		}
		ref := &chatter.TodoRef{
			WorkItemID: item.ID,
			Title:      item.Title,
			Status:     item.Column,
			AssigneeID: item.AssigneeID,
		}
		if item.AssigneeType == workitems.AssigneeUser && item.AssigneeID != nil {
			ref.AssigneeName = names[*item.AssigneeID]
		}
		byChatter[*item.SourceChatterID] = ref
	}
	return byChatter
}

// memberDisplayName is the label a person is shown by: their name, or the
// email they signed up with when they have not set one.
func memberDisplayName(m *members.Member) string {
	if name := strings.TrimSpace(m.UserName); name != "" {
		return name
	}
	return m.UserEmail
}
