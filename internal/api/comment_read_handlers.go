package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/commentreads"
	"github.com/openv/requirements-platform/internal/domain/release"
)

// registerCommentReadRoutes wires a member's comment read state: when they
// last read each artifact's comments, which the Home pages' Needs you filters
// comments by. Its call is the last in RegisterRoutes; no earlier template
// serves /me/comment-reads.
func (h *Handler) registerCommentReadRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/me/comment-reads", h.ListCommentReads).Methods("GET")
	// Marking something read is reading, not editing: it works on a
	// read-only plan.
	router.HandleFunc("/api/v1/me/comment-reads", h.alwaysWritable(h.SetCommentReads)).Methods("PUT")
}

// commentReadsRequest is the body of PUT /me/comment-reads.
type commentReadsRequest struct {
	Reads []commentreads.Read `json:"reads"`
}

func writeCommentReads(w http.ResponseWriter, list []commentreads.Read) {
	if list == nil {
		list = []commentreads.Read{}
	}
	writeJSONOK(w, map[string]interface{}{"comment_reads": list})
}

// ListCommentReads answers GET /me/comment-reads?artifact_ids=a,b: when the
// caller last read each of those artifacts' comments in their active
// workspace, for the ones they have read. An artifact they never read is
// left out: all its comments are unread.
func (h *Handler) ListCommentReads(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	var ids []string
	for _, id := range strings.Split(r.URL.Query().Get("artifact_ids"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) > commentreads.MaxPerRequest {
		writeJSONError(w, http.StatusBadRequest, commentreads.ErrTooMany.Error())
		return
	}
	org := ActiveOrg(r)
	if h.CommentReadService == nil || org == "" || len(ids) == 0 {
		writeCommentReads(w, nil)
		return
	}
	list, err := h.CommentReadService.List(userID, org, ids)
	if err != nil {
		respondInternal(w, r, "failed to load comment read state", err)
		return
	}
	writeCommentReads(w, list)
}

// SetCommentReads stores when the caller read artifacts' comments in their
// active workspace: {"reads": [{"artifact_id", "read_at"}]}, where a missing
// read_at is now and an earlier one marks the comments after it unread. It
// answers the reads it stored; an artifact that is not one of the
// workspace's is skipped. Gated on FeatureCommentReads.
func (h *Handler) SetCommentReads(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireHumanUser(w, r)
	if !ok {
		return
	}
	org := ActiveOrg(r)
	if org == "" {
		writeJSONError(w, http.StatusBadRequest, "no active workspace")
		return
	}
	if !h.featureEnabled(r, org, release.FeatureCommentReads) {
		writeJSONError(w, http.StatusForbidden, "comment read state is not available on this workspace's release channel yet")
		return
	}
	if h.CommentReadService == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "comment read state is not available on this server")
		return
	}
	var req commentReadsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	list, err := h.CommentReadService.Set(userID, org, req.Reads)
	if err != nil {
		if errors.Is(err, commentreads.ErrArtifactID) || errors.Is(err, commentreads.ErrTooMany) || errors.Is(err, commentreads.ErrNone) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondInternal(w, r, "failed to store comment read state", err)
		return
	}
	writeCommentReads(w, list)
}
