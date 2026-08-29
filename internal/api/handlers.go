package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attachments"
	"github.com/openv/requirements-platform/internal/domain/baselines"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/reports"
	"github.com/openv/requirements-platform/internal/domain/templates"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/automations"
	"github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/guided"
	"github.com/openv/requirements-platform/internal/domain/hostedworkers"
	"github.com/openv/requirements-platform/internal/domain/interviews"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/products"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
	"github.com/openv/requirements-platform/internal/domain/teams"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/domain/vv"
	"github.com/openv/requirements-platform/internal/domain/workerkeys"
	"github.com/openv/requirements-platform/internal/domain/workitems"
	"github.com/openv/requirements-platform/internal/hosting"
)

// HandlerDeps carries every service the API layer depends on.
type HandlerDeps struct {
	ArtifactService   artifacts.Service
	LinkService       links.Service
	ProjectService    projects.Service
	AttachmentService attachments.Service
	ExportService     exports.Service
	BaselineService   baselines.Service
	ReportService     reports.Service
	TemplateService   templates.Service
	ChatterService    chatter.Service
	UploadsDir        string

	UserService         users.Service
	MemberService       members.Service
	ProductService      products.Service
	VVService           vv.Service
	WorkItemService     workitems.Service
	GuidedService       guided.Service
	InterviewService    interviews.Service
	AgentService        agents.Service
	RunService          agentruns.Service
	AutomationService   automations.Service
	ProposalService     proposals.Service
	RepoConnService     repoconns.Service
	ProviderService     providers.Service
	LoginService        providers.LoginService
	TeamService         teams.Service
	OrgService          orgs.Service
	OrgTeamService      orgs.TeamService
	WorkerKeyService    workerkeys.Service
	HostedWorkerService hostedworkers.Service
	NotificationService notifications.Service
	Provisioner         hosting.Provisioner
	// OrgSeeder provisions default agents/crew for a new workspace.
	OrgSeeder func(orgID string) error
	// PublicAPIURL is the externally-reachable API base (connector config).
	PublicAPIURL string
	// ConnectorDistDir holds downloadable Agent Connector bundles.
	ConnectorDistDir string
	Bus              events.Bus
	EventRepo        events.Repository
	SSEHub           *SSEHub
	GoogleOAuth      *GoogleOAuthConfig
	SecureCookies    bool
}

// Handler holds references to domain services
type Handler struct {
	artifactService   artifacts.Service
	linkService       links.Service
	projectService    projects.Service
	attachmentService attachments.Service
	exportService     exports.Service
	baselineService   baselines.Service
	reportService     reports.Service
	templateService   templates.Service
	chatterService    chatter.Service
	uploadsDir        string

	userService         users.Service
	memberService       members.Service
	productService      products.Service
	vvService           vv.Service
	workItemService     workitems.Service
	guidedService       guided.Service
	interviewService    interviews.Service
	agentService        agents.Service
	runService          agentruns.Service
	automationService   automations.Service
	proposalService     proposals.Service
	repoConnService     repoconns.Service
	providerService     providers.Service
	loginService        providers.LoginService
	teamService         teams.Service
	orgService          orgs.Service
	orgTeamService      orgs.TeamService
	workerKeyService    workerkeys.Service
	hostedWorkerService hostedworkers.Service
	notificationService notifications.Service
	provisioner         hosting.Provisioner
	orgSeeder           func(orgID string) error
	publicAPIURL        string
	connectorDistDir    string
	bus                 events.Bus
	eventRepo           events.Repository
	sseHub              *SSEHub
	googleOAuth         *GoogleOAuthConfig
	secureCookies       bool

	// Rate limiters for the public (invite-token) interview endpoints; see
	// ratelimit.go for defaults and environment overrides.
	interviewMsgLimiter    *rateLimiter // per-invite participant messages
	interviewIPLimiter     *rateLimiter // per-IP intro GETs
	interviewStreamLimiter *rateLimiter // per-IP SSE stream connects (more generous: reconnects are routine)
}

// NewHandler creates a new API handler
func NewHandler(deps HandlerDeps) *Handler {
	return &Handler{
		artifactService:     deps.ArtifactService,
		linkService:         deps.LinkService,
		projectService:      deps.ProjectService,
		attachmentService:   deps.AttachmentService,
		exportService:       deps.ExportService,
		baselineService:     deps.BaselineService,
		reportService:       deps.ReportService,
		templateService:     deps.TemplateService,
		chatterService:      deps.ChatterService,
		uploadsDir:          deps.UploadsDir,
		userService:         deps.UserService,
		memberService:       deps.MemberService,
		productService:      deps.ProductService,
		vvService:           deps.VVService,
		workItemService:     deps.WorkItemService,
		guidedService:       deps.GuidedService,
		interviewService:    deps.InterviewService,
		agentService:        deps.AgentService,
		runService:          deps.RunService,
		automationService:   deps.AutomationService,
		proposalService:     deps.ProposalService,
		repoConnService:     deps.RepoConnService,
		providerService:     deps.ProviderService,
		loginService:        deps.LoginService,
		teamService:         deps.TeamService,
		orgService:          deps.OrgService,
		orgTeamService:      deps.OrgTeamService,
		workerKeyService:    deps.WorkerKeyService,
		hostedWorkerService: deps.HostedWorkerService,
		notificationService: deps.NotificationService,
		provisioner:         deps.Provisioner,
		orgSeeder:           deps.OrgSeeder,
		publicAPIURL:        deps.PublicAPIURL,
		connectorDistDir:    deps.ConnectorDistDir,
		bus:                 deps.Bus,
		eventRepo:           deps.EventRepo,
		sseHub:              deps.SSEHub,
		googleOAuth:         deps.GoogleOAuth,
		secureCookies:       deps.SecureCookies,
		interviewMsgLimiter:    newRateLimiterFromEnv(envInterviewMsgBurst, envInterviewMsgRefill, defaultInterviewMsgBurst, defaultInterviewMsgRefill),
		interviewIPLimiter:     newRateLimiterFromEnv(envInterviewIPBurst, envInterviewIPRefill, defaultInterviewIPBurst, defaultInterviewIPRefill),
		interviewStreamLimiter: newRateLimiterFromEnv(envInterviewStreamBurst, envInterviewStreamRefill, defaultInterviewStreamBurst, defaultInterviewStreamRefill),
	}
}

// publish emits a domain event when a bus is wired (nil-safe), stamped with
// the owning org (the project's org when project-scoped, else the caller's
// active workspace).
func (h *Handler) publish(r *http.Request, eventType, projectID, entityID string, payload map[string]interface{}) {
	if h.bus == nil {
		return
	}
	orgID := ""
	if projectID != "" && h.projectService != nil {
		if project, err := h.projectService.GetProject(projectID); err == nil && project != nil {
			orgID = project.OrgID
		}
	}
	if orgID == "" {
		orgID = ActiveOrg(r)
	}
	h.bus.Publish(events.New(eventType, projectID, entityID, Actor(r), payload).WithOrg(orgID))
}

// RegisterRoutes registers all API routes
func (h *Handler) RegisterRoutes(router *mux.Router) {
	// Project endpoints
	router.HandleFunc("/api/v1/projects", h.CreateProject).Methods("POST")
	router.HandleFunc("/api/v1/projects", h.ListProjects).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}", h.GetProject).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}", h.UpdateProject).Methods("PUT")
	router.HandleFunc("/api/v1/projects/{id}", h.DeleteProject).Methods("DELETE")
	router.HandleFunc("/api/v1/projects/{id}/export", h.ExportProject).Methods("GET")
	router.HandleFunc("/api/v1/projects/import", h.ImportProject).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/report", h.GenerateReport).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/review-queue", h.ReviewQueue).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/baselines", h.CreateBaseline).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/baselines", h.ListBaselines).Methods("GET")
	router.HandleFunc("/api/v1/baselines/{id}", h.GetBaseline).Methods("GET")
	router.HandleFunc("/api/v1/baselines/{id}/diff", h.DiffBaseline).Methods("GET")
	router.HandleFunc("/api/v1/baselines/{id}", h.DeleteBaseline).Methods("DELETE")
	router.HandleFunc("/api/v1/templates", h.ListTemplates).Methods("GET")
	router.HandleFunc("/api/v1/templates", h.CreateTemplate).Methods("POST")
	router.HandleFunc("/api/v1/templates/{id}/projects", h.CreateProjectFromTemplate).Methods("POST")

	// Artifact endpoints
	router.HandleFunc("/api/v1/artifacts", h.CreateArtifact).Methods("POST")
	router.HandleFunc("/api/v1/artifacts", h.ListArtifacts).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}", h.GetArtifact).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}", h.UpdateArtifact).Methods("PUT")
	router.HandleFunc("/api/v1/artifacts/{id}/status", h.ChangeArtifactStatus).Methods("PUT")
	router.HandleFunc("/api/v1/artifacts/{id}", h.DeleteArtifact).Methods("DELETE")
	router.HandleFunc("/api/v1/artifacts/{id}/versions", h.GetArtifactVersions).Methods("GET")
	router.HandleFunc("/api/v1/artifacts/{id}/restore", h.RestoreArtifactVersion).Methods("POST")
	router.HandleFunc("/api/v1/artifacts/{id}/links", h.GetArtifactVersionLinks).Methods("GET")

	// Link endpoints
	router.HandleFunc("/api/v1/links", h.CreateLink).Methods("POST")
	router.HandleFunc("/api/v1/links", h.ListLinks).Methods("GET")
	router.HandleFunc("/api/v1/links/{id}", h.GetLink).Methods("GET")
	router.HandleFunc("/api/v1/links/{id}", h.UpdateLink).Methods("PUT")
	router.HandleFunc("/api/v1/links/{id}/confirm", h.ConfirmLink).Methods("PUT")
	router.HandleFunc("/api/v1/links/{id}", h.DeleteLink).Methods("DELETE")

	// Attachment endpoints
	router.HandleFunc("/api/v1/attachments/upload", h.UploadAttachment).Methods("POST")
	router.HandleFunc("/api/v1/attachments/{id}", h.GetAttachmentMeta).Methods("GET")
	router.HandleFunc("/api/v1/attachments/{id}/download", h.DownloadAttachment).Methods("GET")
	router.HandleFunc("/api/v1/attachments/{id}", h.DeleteAttachment).Methods("DELETE")
	router.HandleFunc("/api/v1/artifacts/{artifactID}/attachments", h.ListArtifactAttachments).Methods("GET")

	// Chatter endpoints
	router.HandleFunc("/api/v1/chatter", h.CreateChatterEntry).Methods("POST")
	router.HandleFunc("/api/v1/chatter", h.ListChatterEntries).Methods("GET")

	// Global artifact search (see search_handlers.go)
	router.HandleFunc("/api/v1/search", h.GlobalSearch).Methods("GET")

	// Extended route groups (auth, meta, suite, agents, orgs) live in their own files.
	h.registerAuthRoutes(router)
	h.registerMetaRoutes(router)
	h.registerSuiteRoutes(router)
	h.registerNotificationRoutes(router)
	h.registerAgentRoutes(router)
	h.registerOrgRoutes(router)

	// Health check
	router.HandleFunc("/health", h.Health).Methods("GET")
}

// Health returns the health status
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// CreateArtifact creates a new artifact
func (h *Handler) CreateArtifact(w http.ResponseWriter, r *http.Request) {
	var req artifacts.CreateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !h.requireProjectRole(w, r, req.ProjectID, members.RoleEditor) {
		return
	}

	// Interviewer-created artifacts always land as interview-tagged drafts,
	// regardless of what attributes the model supplied.
	if run := CurrentRun(r); run != nil && run.InterviewSessionID != nil {
		if req.Attributes == nil {
			req.Attributes = map[string]interface{}{}
		}
		req.Attributes["status"] = "draft"
		req.Attributes["origin"] = "interview"
		req.Attributes["interview_session_id"] = *run.InterviewSessionID
	}

	// Likewise for guided-copilot turn runs: anything they create is a
	// guided-flow-tagged draft.
	if run := CurrentRun(r); run != nil && run.GuidedSessionID != nil {
		if req.Attributes == nil {
			req.Attributes = map[string]interface{}{}
		}
		req.Attributes["status"] = "draft"
		req.Attributes["origin"] = "guided-flow"
		req.Attributes["guided_session_id"] = *run.GuidedSessionID
	}

	if h.maybePropose(w, r, req.ProjectID, proposals.OpCreateArtifact, nil, req) {
		return
	}

	artifact := artifacts.NewArtifact(req)
	if err := h.artifactService.CreateArtifact(artifact); err != nil {
		respondInternal(w, r, "failed to create artifact", err)
		return
	}

	h.publish(r, events.ArtifactCreated, artifact.ProjectID, artifact.ID, map[string]interface{}{
		"artifact_type": artifact.Type,
		"title":         artifact.Title,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(artifact)
}

// GetArtifact retrieves an artifact by ID
func (h *Handler) GetArtifact(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	artifact, err := h.artifactService.GetArtifact(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "artifact not found", err)
		return
	}

	if !h.requireProjectRole(w, r, artifact.ProjectID, members.RoleViewer) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(artifact)
}

// Artifact listing pagination bounds. Artifacts render as a parent_id tree in
// the module view, so the UI still needs the complete set to build structure;
// the generous default keeps single-request behavior for typical projects
// while bounding worst-case response size. Clients page with limit/offset and
// the X-Total-Count header until they have everything.
const (
	defaultArtifactPageLimit = 1000
	maxArtifactPageLimit     = 1000
)

// ListArtifacts lists artifacts by project and optional type filter, one page
// at a time. Query params: project_id (required), type, limit (default and
// cap 1000), offset. The response body stays a plain JSON array for
// compatibility; the total number of matching artifacts rides on the
// X-Total-Count header so clients can page until exhaustion.
func (h *Handler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	projectID := q.Get("project_id")
	artifactType := q.Get("type")

	if projectID == "" {
		writeJSONError(w, http.StatusBadRequest, "project_id is required")
		return
	}

	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > maxArtifactPageLimit {
		limit = defaultArtifactPageLimit
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}

	page, total, err := h.artifactService.ListArtifactsPage(projectID, artifactType, limit, offset)
	if err != nil {
		respondInternal(w, r, "failed to list artifacts", err)
		return
	}
	if page == nil {
		page = []*artifacts.Artifact{}
	}

	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(page)
}

// UpdateArtifact updates an artifact
func (h *Handler) UpdateArtifact(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req artifacts.UpdateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch the old artifact BEFORE updating to track changes
	oldArtifact, err := h.artifactService.GetArtifact(id)
	if err != nil {
		respondInternal(w, r, "failed to load artifact", err)
		return
	}

	if !h.requireProjectRole(w, r, oldArtifact.ProjectID, members.RoleEditor) {
		return
	}
	if h.maybePropose(w, r, oldArtifact.ProjectID, proposals.OpUpdateArtifact, &id, req) {
		return
	}

	// Attributes contract (issue #125): a request that OMITS attributes
	// (nil after decode) means "leave them unchanged" — the domain layer
	// carries the current attributes forward. An explicit map — even an
	// empty {} — replaces them wholesale. Do NOT normalize nil to an empty
	// map here: that would turn every attribute-less update (e.g. the MCP
	// update_artifact tool) into a wipe.

	// Process link changes FIRST (add/remove from table)
	var affectedArtifactIDs []string
	var addedLinks, removedLinks []*links.Link
	if len(req.PendingLinkAdds) > 0 || len(req.PendingLinkRemoves) > 0 {
		// Convert string array to interface array for removal IDs
		removeInterfaceArray := make([]interface{}, len(req.PendingLinkRemoves))
		for i, v := range req.PendingLinkRemoves {
			removeInterfaceArray[i] = v
		}

		// Fetch link details BEFORE removing them (for chatter)
		for _, linkID := range req.PendingLinkRemoves {
			if link, err := h.linkService.GetLink(linkID); err == nil {
				removedLinks = append(removedLinks, link)
			}
		}

		// Build link details for adds. Unlike removals, adds are not IDs of
		// existing links: each entry is the link object about to be created
		// ({from_id,to_id,type,...}), so read those fields for the chatter
		// summary.
		addedLinks = linksFromPendingAdds(req.PendingLinkAdds)

		affected, err := h.processManagedLinkChanges(r, oldArtifact.ProjectID, id, req.PendingLinkAdds, removeInterfaceArray)
		if err != nil {
			respondInternal(w, r, "failed to process link changes", err)
			return
		}
		affectedArtifactIDs = affected

		// After processing link changes, fetch current links and store in snapshot (deduplicated)
		seenLinkIDs := make(map[string]bool)
		allLinks := make([]interface{}, 0)

		incomingLinks, err := h.linkService.GetLinksTo(id)
		if err == nil {
			for _, link := range incomingLinks {
				if !seenLinkIDs[link.ID] {
					seenLinkIDs[link.ID] = true
					allLinks = append(allLinks, link)
				}
			}
		}

		outgoingLinks, err := h.linkService.GetLinksFrom(id)
		if err == nil {
			for _, link := range outgoingLinks {
				if !seenLinkIDs[link.ID] {
					seenLinkIDs[link.ID] = true
					allLinks = append(allLinks, link)
				}
			}
		}

		if len(allLinks) > 0 {
			// Storing the snapshot needs a concrete attributes map. If the
			// request left attributes untouched (nil), seed a copy of the
			// current ones so the snapshot write doesn't clear the rest.
			if req.Attributes == nil {
				req.Attributes = make(map[string]interface{}, len(oldArtifact.Attributes)+1)
				for k, v := range oldArtifact.Attributes {
					req.Attributes[k] = v
				}
			}
			req.Attributes["links_snapshot"] = allLinks
		}
	}

	// Update the artifact ONCE with all changes including link snapshot
	// This single update will create ONE new version
	artifact, err := h.artifactService.UpdateArtifact(id, req)
	if err != nil {
		respondInternal(w, r, "failed to update artifact", err)
		return
	}

	// Build a detailed change summary for chatter
	chatterMessage := h.buildChangesSummary(oldArtifact, artifact, addedLinks, removedLinks)
	chatterEntry := chatter.NewChatterEntry(id, chatterMessage, true, "version-change")
	if err := h.chatterService.CreateEntry(chatterEntry); err != nil {
		// Log but don't fail the request
		slog.Warn("api: failed to create chatter entry for version change", "artifact_id", id, "error", err)
	}

	// Auto-version any artifacts that had link changes
	// These are OTHER artifacts affected by link changes, not the one we just updated
	if len(affectedArtifactIDs) > 0 {
		err = h.autoVersionLinkedArtifacts(affectedArtifactIDs)
		if err != nil {
			// Log but don't fail the request
			slog.Warn("api: failed to auto-version linked artifacts", "error", err)
		}
	}

	h.publish(r, events.ArtifactUpdated, artifact.ProjectID, artifact.ID, map[string]interface{}{
		"artifact_type": artifact.Type,
		"title":         artifact.Title,
		"version":       artifact.Version,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(artifact)
}

// ChangeArtifactStatus handles PUT /api/v1/artifacts/{id}/status: one review
// state-machine transition (draft <-> in_review -> approved -> superseded).
//
// Authorization: every transition — approval included — requires project
// editor or better. Owner-only approval would be tighter, but project
// membership has no granularity between editor and owner beyond the role
// ladder, so editor-approval is the documented choice (see
// artifacts.DefaultService.ChangeStatus). Proposal-mode agent runs are
// refused outright: the proposal vocabulary has no status op, and silently
// letting a review-gated agent flip review states would defeat the gate.
func (h *Handler) ChangeArtifactStatus(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	artifact, err := h.artifactService.GetArtifact(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "artifact not found", err)
		return
	}

	if !h.requireProjectRole(w, r, artifact.ProjectID, members.RoleEditor) {
		return
	}
	if run := CurrentRun(r); run != nil && h.agentService != nil {
		if agent, err := h.agentService.Get(run.AgentID); err == nil && agent != nil && agent.WriteMode == agents.WriteModeProposal {
			writeJSONError(w, http.StatusForbidden, "proposal-mode agent runs cannot change artifact status")
			return
		}
	}

	from := artifacts.NormalizeStatus(artifact.Status)
	updated, err := h.artifactService.ChangeStatus(id, req.Status)
	if err != nil {
		switch {
		case errors.Is(err, artifacts.ErrInvalidStatus):
			respondError(w, r, http.StatusBadRequest, err.Error(), nil)
		case errors.Is(err, artifacts.ErrInvalidStatusTransition):
			respondError(w, r, http.StatusConflict, err.Error(), nil)
		case errors.Is(err, artifacts.ErrNotFound):
			respondError(w, r, http.StatusNotFound, "artifact not found", err)
		default:
			respondInternal(w, r, "failed to change artifact status", err)
		}
		return
	}

	// Sign-off history lives in the event stream (issue #127).
	h.publish(r, events.ArtifactStatusChanged, updated.ProjectID, updated.ID, map[string]interface{}{
		"artifact_type": updated.Type,
		"title":         updated.Title,
		"from":          from,
		"to":            updated.Status,
		"version":       updated.Version,
	})

	entry := chatter.NewChatterEntry(id, fmt.Sprintf("Status changed: %s → %s", from, updated.Status), true, "status-change")
	if err := h.chatterService.CreateEntry(entry); err != nil {
		slog.Warn("api: failed to create chatter entry for status change", "artifact_id", id, "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

// DeleteArtifact deletes an artifact
func (h *Handler) DeleteArtifact(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	projectID := h.projectIDForArtifact(id)
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	if h.maybePropose(w, r, projectID, proposals.OpDeleteArtifact, &id, nil) {
		return
	}

	err := h.artifactService.DeleteArtifact(id)
	if err != nil {
		respondInternal(w, r, "failed to delete artifact", err)
		return
	}

	h.publish(r, events.ArtifactDeleted, projectID, id, nil)

	w.WriteHeader(http.StatusNoContent)
}

// GetArtifactVersions retrieves all versions of an artifact
func (h *Handler) GetArtifactVersions(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, h.projectIDForArtifact(id), members.RoleViewer) {
		return
	}

	versions, err := h.artifactService.GetArtifactVersions(id)
	if err != nil {
		respondInternal(w, r, "failed to load artifact versions", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(versions)
}

// RestoreArtifactVersion restores a previous version of an artifact
func (h *Handler) RestoreArtifactVersion(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req struct {
		Version int `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch the current artifact BEFORE restoring (to track changes)
	oldArtifact, err := h.artifactService.GetArtifact(id)
	if err != nil {
		respondInternal(w, r, "failed to load artifact", err)
		return
	}

	if !h.requireProjectRole(w, r, oldArtifact.ProjectID, members.RoleEditor) {
		return
	}

	artifact, err := h.artifactService.RestoreArtifactVersion(id, req.Version)
	if err != nil {
		respondInternal(w, r, "failed to restore artifact version", err)
		return
	}

	// Create a chatter entry for the restore
	restoredFromVersion := req.Version
	chatterMessage := h.buildRestoreMessage(oldArtifact, artifact, restoredFromVersion)
	chatterEntry := chatter.NewChatterEntry(id, chatterMessage, true, "restore")
	if err := h.chatterService.CreateEntry(chatterEntry); err != nil {
		// Log but don't fail the request
		slog.Warn("api: failed to create chatter entry for restore", "artifact_id", id, "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(artifact)
}

// buildRestoreMessage creates a message describing what version was restored
func (h *Handler) buildRestoreMessage(oldArtifact, newArtifact *artifacts.Artifact, restoredFromVersion int) string {
	message := fmt.Sprintf("Restored to version %d (from version %d)", newArtifact.Version, restoredFromVersion)

	// Reuse the changes summary logic to show what changed
	var addedLinks, removedLinks []*links.Link
	changes := h.buildChangesList(oldArtifact, newArtifact, addedLinks, removedLinks)

	if len(changes) > 0 {
		message += "\n\nChanges:\n"
		for _, change := range changes {
			if strings.HasPrefix(change, "  -") || strings.HasPrefix(change, "  +") {
				// Diff lines - include as-is (indented)
				message += change + "\n"
			} else if strings.HasPrefix(change, "    ") {
				// Indented lines
				message += change + "\n"
			} else if strings.HasPrefix(change, "Links:") || strings.HasPrefix(change, "Description modified:") {
				// Headers
				message += "- " + change + "\n"
			} else {
				// Regular changes
				message += "- " + change + "\n"
			}
		}
	}

	return message
}

// buildChangesList creates the list of changes without wrapping in a message
func (h *Handler) buildChangesList(oldArtifact, newArtifact *artifacts.Artifact, addedLinks, removedLinks []*links.Link) []string {
	var changes []string

	// Check for field changes
	if oldArtifact.Title != newArtifact.Title {
		if oldArtifact.Title == "" {
			changes = append(changes, fmt.Sprintf("Title: → \"%s\"", newArtifact.Title))
		} else {
			changes = append(changes, fmt.Sprintf("Title: \"%s\" → \"%s\"", oldArtifact.Title, newArtifact.Title))
		}
	}

	if oldArtifact.Body != newArtifact.Body {
		// For multiline content, show a git-style diff
		if strings.Contains(oldArtifact.Body, "\n") || strings.Contains(newArtifact.Body, "\n") {
			changes = append(changes, "Description modified:")
			oldLines := strings.Split(oldArtifact.Body, "\n")
			newLines := strings.Split(newArtifact.Body, "\n")

			// Simple diff: show removed lines starting with -, added lines starting with +
			maxLines := len(oldLines)
			if len(newLines) > maxLines {
				maxLines = len(newLines)
			}

			var diffLines []string
			for i := 0; i < maxLines; i++ {
				if i < len(oldLines) && i < len(newLines) {
					if oldLines[i] != newLines[i] {
						diffLines = append(diffLines, fmt.Sprintf("  - %s", oldLines[i]))
						diffLines = append(diffLines, fmt.Sprintf("  + %s", newLines[i]))
					}
				} else if i < len(oldLines) {
					diffLines = append(diffLines, fmt.Sprintf("  - %s", oldLines[i]))
				} else if i < len(newLines) {
					diffLines = append(diffLines, fmt.Sprintf("  + %s", newLines[i]))
				}
			}

			changes = append(changes, strings.Join(diffLines, "\n"))
		} else {
			// For single-line content, use the short format
			if oldArtifact.Body == "" {
				changes = append(changes, fmt.Sprintf("Body: → \"%s\"", newArtifact.Body))
			} else {
				changes = append(changes, fmt.Sprintf("Body: \"%s\" → \"%s\"", oldArtifact.Body, newArtifact.Body))
			}
		}
	}

	if oldArtifact.Type != newArtifact.Type {
		changes = append(changes, fmt.Sprintf("Type: \"%s\" → \"%s\"", oldArtifact.Type, newArtifact.Type))
	}

	// Check for image changes
	oldImages := oldArtifact.GetImagesSnapshot()
	newImages := newArtifact.GetImagesSnapshot()

	oldImageMap := make(map[string]bool)
	newImageMap := make(map[string]bool)

	// Build maps of filenames
	for _, img := range oldImages {
		if imgData, ok := img.(map[string]interface{}); ok {
			if filename, ok := imgData["filename"].(string); ok {
				oldImageMap[filename] = true
			}
		}
	}

	for _, img := range newImages {
		if imgData, ok := img.(map[string]interface{}); ok {
			if filename, ok := imgData["filename"].(string); ok {
				newImageMap[filename] = true
			}
		}
	}

	// Find added images
	addedImages := 0
	for filename := range newImageMap {
		if !oldImageMap[filename] {
			addedImages++
		}
	}

	// Find removed images
	removedImages := 0
	for filename := range oldImageMap {
		if !newImageMap[filename] {
			removedImages++
		}
	}

	if addedImages > 0 {
		changes = append(changes, fmt.Sprintf("Images: Added %d image(s)", addedImages))
	}

	if removedImages > 0 {
		changes = append(changes, fmt.Sprintf("Images: Removed %d image(s)", removedImages))
	}

	return changes
}

// GetArtifactVersionLinks retrieves links for a specific artifact version
func (h *Handler) GetArtifactVersionLinks(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, h.projectIDForArtifact(id), members.RoleViewer) {
		return
	}

	versionStr := r.URL.Query().Get("version")

	// If no version specified, return current links from link table
	if versionStr == "" {
		outgoingLinks, err := h.linkService.GetLinksFrom(id)
		if err != nil {
			respondInternal(w, r, "failed to load links", err)
			return
		}
		incomingLinks, err := h.linkService.GetLinksTo(id)
		if err != nil {
			respondInternal(w, r, "failed to load links", err)
			return
		}

		// Combine both incoming and outgoing links and deduplicate by ID
		seenLinkIDs := make(map[string]bool)
		allLinks := make([]*links.Link, 0)

		// Add outgoing links
		for _, link := range outgoingLinks {
			if !seenLinkIDs[link.ID] {
				seenLinkIDs[link.ID] = true
				allLinks = append(allLinks, link)
			}
		}

		// Add incoming links (deduplicated)
		for _, link := range incomingLinks {
			if !seenLinkIDs[link.ID] {
				seenLinkIDs[link.ID] = true
				allLinks = append(allLinks, link)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(allLinks)
		return
	}

	// Parse version number
	version := 0
	if _, err := fmt.Sscanf(versionStr, "%d", &version); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid version parameter")
		return
	}

	// Get the artifact version from database
	versions, err := h.artifactService.GetArtifactVersions(id)
	if err != nil {
		respondInternal(w, r, "failed to load artifact versions", err)
		return
	}

	// Find the specific version
	var artifactVersion *artifacts.Artifact
	for _, v := range versions {
		if v.Version == version {
			artifactVersion = v
			break
		}
	}

	if artifactVersion == nil {
		writeJSONError(w, http.StatusNotFound, "artifact version not found")
		return
	}

	// Extract links_snapshot from artifact attributes
	var linksSnapshot []*links.Link
	if artifactVersion.Attributes != nil {
		if snapshot, ok := artifactVersion.Attributes["links_snapshot"]; ok {
			// Convert interface{} to []*links.Link
			if snapshotData, ok := snapshot.([]interface{}); ok {
				linksSnapshot = make([]*links.Link, 0, len(snapshotData))
				for _, linkData := range snapshotData {
					// Each linkData should be a map or already a Link struct
					if linkBytes, err := json.Marshal(linkData); err == nil {
						var link links.Link
						if err := json.Unmarshal(linkBytes, &link); err == nil {
							linksSnapshot = append(linksSnapshot, &link)
						}
					}
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(linksSnapshot)
}

// CreateLink creates a new link
func (h *Handler) CreateLink(w http.ResponseWriter, r *http.Request) {
	var req links.CreateLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Fetch the from and to artifacts to validate link type
	fromArtifact, err := h.artifactService.GetArtifact(req.FromID)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "source artifact not found", err)
		return
	}

	toArtifact, err := h.artifactService.GetArtifact(req.ToID)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "target artifact not found", err)
		return
	}

	// Validate link type against artifact types
	if err := links.ValidateLinkType(req.Type, fromArtifact.Type, toArtifact.Type); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if !h.requireProjectRole(w, r, fromArtifact.ProjectID, members.RoleEditor) {
		return
	}
	// A link also writes to the target artifact (version bump + chatter via
	// autoVersionLinkedArtifacts) and exposes its title, so a cross-project
	// link needs editor rights on the target's project too.
	if toArtifact.ProjectID != fromArtifact.ProjectID && !h.requireProjectRole(w, r, toArtifact.ProjectID, members.RoleEditor) {
		return
	}
	if h.maybePropose(w, r, fromArtifact.ProjectID, proposals.OpCreateLink, nil, req) {
		return
	}

	link := links.NewLink(req)
	if err := h.linkService.CreateLink(link); err != nil {
		respondInternal(w, r, "failed to create link", err)
		return
	}

	// Refresh link snapshots for both artifacts touched by this link
	_ = h.autoVersionLinkedArtifacts([]string{link.FromID, link.ToID})

	h.publish(r, events.LinkCreated, fromArtifact.ProjectID, link.ID, map[string]interface{}{
		"link_type": link.Type,
		"from_id":   link.FromID,
		"to_id":     link.ToID,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(link)
}

// GetLink retrieves a link by ID
func (h *Handler) GetLink(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	link, err := h.linkService.GetLink(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "link not found", err)
		return
	}

	if !h.requireProjectRole(w, r, h.projectIDForArtifact(link.FromID), members.RoleViewer) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(link)
}

// ReviewQueue handles GET /api/v1/projects/{id}/review-queue: the reviewer's
// daily driver (issue #183). It returns the two things awaiting a reviewer's
// attention in one round trip — the live suspect links touching the project
// (each enriched with its endpoints' titles/types so a reviewer can judge it
// without opening both artifacts) and the artifacts sitting in the in_review
// state. Read-only, so project viewer role suffices; the per-row actions the
// UI offers (confirm a link, open an artifact) carry their own editor gates.
func (h *Handler) ReviewQueue(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	suspectLinks, err := h.linkService.ListSuspectByProject(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load suspect links", err)
		return
	}
	inReview, err := h.artifactService.ListByStatus(projectID, artifacts.StatusInReview)
	if err != nil {
		respondInternal(w, r, "failed to load in-review artifacts", err)
		return
	}

	// Normalize nil slices to [] so the JSON body always has array-typed
	// fields the frontend can map over unconditionally.
	if suspectLinks == nil {
		suspectLinks = []*links.SuspectLink{}
	}
	if inReview == nil {
		inReview = []*artifacts.Artifact{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"suspect_links":       suspectLinks,
		"in_review_artifacts": inReview,
	})
}

// ListLinks lists links by project
func (h *Handler) ListLinks(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		writeJSONError(w, http.StatusBadRequest, "project_id is required")
		return
	}

	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	links, err := h.linkService.GetAllLinks(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list links", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(links)
}

// UpdateLink updates a link
func (h *Handler) UpdateLink(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req links.UpdateLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	existing, err := h.linkService.GetLink(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "link not found", err)
		return
	}
	if !h.requireProjectRole(w, r, h.projectIDForArtifact(existing.FromID), members.RoleEditor) {
		return
	}

	link, err := h.linkService.UpdateLink(id, req)
	if err != nil {
		respondInternal(w, r, "failed to update link", err)
		return
	}

	// Refresh link snapshots for both artifacts touched by this link
	_ = h.autoVersionLinkedArtifacts([]string{link.FromID, link.ToID})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(link)
}

// ConfirmLink handles PUT /api/v1/links/{id}/confirm: an editor vouches
// that a suspect link still holds after the linked artifact's content
// changed, clearing the suspect flag (issue #131). Authorization mirrors
// UpdateLink: editor on the source artifact's project. Confirming an
// already-trusted link is a harmless no-op, so the endpoint is idempotent.
//
// Proposal-mode agent runs are refused outright (issue #176). Clearing the
// suspect flag is a human review action — it asserts that a link still holds
// after content changed — and the proposal vocabulary has no op for it.
// Letting a review-gated agent clear suspect directly would defeat the very
// review the flag exists to trigger. Refusal (not diversion to a proposal)
// mirrors ChangeArtifactStatus: both are human sign-off gates with no
// proposal representation.
func (h *Handler) ConfirmLink(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	existing, err := h.linkService.GetLink(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "link not found", err)
		return
	}
	if !h.requireProjectRole(w, r, h.projectIDForArtifact(existing.FromID), members.RoleEditor) {
		return
	}
	if run := CurrentRun(r); run != nil && h.agentService != nil {
		if agent, err := h.agentService.Get(run.AgentID); err == nil && agent != nil && agent.WriteMode == agents.WriteModeProposal {
			writeJSONError(w, http.StatusForbidden, "proposal-mode agent runs cannot clear a suspect link")
			return
		}
	}

	link, err := h.linkService.ConfirmLink(id)
	if err != nil {
		respondInternal(w, r, "failed to confirm link", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(link)
}

// DeleteLink deletes a link
func (h *Handler) DeleteLink(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	link, _ := h.linkService.GetLink(id)

	projectID := ""
	if link != nil {
		projectID = h.projectIDForArtifact(link.FromID)
	}
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	if h.maybePropose(w, r, projectID, proposals.OpDeleteLink, &id, nil) {
		return
	}

	err := h.linkService.DeleteLink(id)
	if err != nil {
		respondInternal(w, r, "failed to delete link", err)
		return
	}

	if link != nil {
		// Refresh link snapshots for both artifacts touched by this link
		_ = h.autoVersionLinkedArtifacts([]string{link.FromID, link.ToID})
		h.publish(r, events.LinkDeleted, projectID, link.ID, map[string]interface{}{
			"link_type": link.Type,
			"from_id":   link.FromID,
			"to_id":     link.ToID,
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

// ContentTypeMiddleware is kept as a router-level hook; CORS now lives in
// the credential-aware wrapper in cmd/server/main.go.
func ContentTypeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CreateProject creates a new project
func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req projects.CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	orgID := ActiveOrg(r)
	if orgID == "" {
		writeJSONError(w, http.StatusBadRequest, "no active workspace for this request")
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

// GetProject retrieves a project by ID
func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	project, err := h.projectService.GetProject(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "project not found", err)
		return
	}

	if !h.requireProjectRole(w, r, id, members.RoleViewer) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(project)
}

// ListProjects lists the projects visible to the caller within the active
// workspace: all of the org's projects for platform admins and org admins,
// membership-filtered otherwise.
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

	if user := CurrentUser(r); user != nil && !user.IsAdmin && h.memberService != nil {
		// Org admins of the active workspace see all of its projects.
		isOrgAdmin := false
		if h.orgService != nil {
			if role, err := h.orgService.RoleInOrg(activeOrg, user.ID); err == nil && role == orgs.RoleAdmin {
				isOrgAdmin = true
			}
		}
		if !isOrgAdmin {
			ids, err := h.memberService.ProjectIDsForUser(user.ID)
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

	project, err := h.projectService.UpdateProject(id, req)
	if err != nil {
		respondInternal(w, r, "failed to update project", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(project)
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

// ExportProject exports a project in the specified format
func (h *Handler) ExportProject(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, id, members.RoleViewer) {
		return
	}

	// Get format from query parameter (default to JSON)
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	exportFormat := exports.ExportFormat(format)

	// Reject formats the export service cannot produce before doing any work.
	var contentType string
	switch exportFormat {
	case exports.FormatJSON:
		contentType = "application/json"
	case exports.FormatCSV:
		contentType = "text/csv; charset=utf-8"
	default:
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("unsupported export format: %s", format))
		return
	}

	// Export project
	data, filename, err := h.exportService.ExportProject(id, exportFormat)
	if err != nil {
		if errors.Is(err, exports.ErrUnsupportedFormat) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondInternal(w, r, "failed to export project", err)
		return
	}

	// Set appropriate headers. The filename is quoted; the export service
	// sanitizes it (no quotes, backslashes, or control characters).
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// ImportProject imports project data from uploaded JSON file and creates a new project
func (h *Handler) ImportProject(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r) == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	orgID := ActiveOrg(r)
	if orgID == "" {
		writeJSONError(w, http.StatusBadRequest, "no active workspace for this request")
		return
	}

	// Read the uploaded file
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}

	// Import and create new project
	projectID, err := h.exportService.ImportProject(data, orgID)
	if err != nil {
		respondInternal(w, r, "failed to import project", err)
		return
	}

	// Creator becomes the project owner (mirrors CreateProject).
	if user := CurrentUser(r); user != nil && h.memberService != nil {
		if err := h.memberService.AddMember(projectID, user.ID, members.RoleOwner); err != nil {
			slog.Warn("api: failed to add creator as project owner", "project_id", projectID, "error", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"status":     "success",
		"message":    "Project imported successfully",
		"project_id": projectID,
	})
}

// GenerateReport generates a PDF report for a project or baseline.
func (h *Handler) GenerateReport(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	baselineID := r.URL.Query().Get("baseline_id")

	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "pdf"
	}

	var (
		data        []byte
		filename    string
		contentType string
		err         error
	)
	switch format {
	case "pdf":
		contentType = "application/pdf"
		data, filename, err = h.reportService.GenerateProjectReport(projectID, baselineID)
	case "docx":
		contentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		data, filename, err = h.reportService.GenerateProjectReportDOCX(projectID, baselineID)
	default:
		respondError(w, r, http.StatusBadRequest, "unsupported report format", fmt.Errorf("unsupported report format: %q", format))
		return
	}

	if err != nil {
		if errors.Is(err, baselines.ErrNotFound) {
			respondError(w, r, http.StatusNotFound, "baseline not found", err)
			return
		}
		respondInternal(w, r, "failed to generate project report", err)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

type createTemplateRequest struct {
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// TemplateListResponse wraps template data with source information
type TemplateListResponse struct {
	ID          string    `json:"id"`
	Key         string    `json:"key,omitempty"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Source      string    `json:"source"` // "database" or "file"
	IsDefault   bool      `json:"is_default"`
	CreatedAt   time.Time `json:"created_at"`
}

// ListTemplates returns available templates (both database and file-based).
func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	// Get database templates (the workspace's own plus global built-ins).
	dbTemplates, err := h.templateService.ListTemplates(ActiveOrg(r))
	if err != nil {
		respondInternal(w, r, "failed to list templates", err)
		return
	}

	// Convert DB templates to response format
	var allTemplates []TemplateListResponse
	for _, t := range dbTemplates {
		allTemplates = append(allTemplates, TemplateListResponse{
			ID:          t.ID,
			Key:         t.Key,
			Name:        t.Name,
			Description: t.Description,
			Source:      "database",
			IsDefault:   t.IsDefault,
			CreatedAt:   t.CreatedAt,
		})
	}

	// Get file-based templates
	// Try different locations where examples might be
	var examplesDir string
	possiblePaths := []string{
		"examples",
		"./examples",
		"/root/examples",
	}

	for _, path := range possiblePaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			examplesDir = path
			break
		}
	}

	var fileTemplates []*templates.TemplateSummary
	if examplesDir != "" {
		var err error
		fileTemplates, err = templates.LoadFileBasedTemplates(examplesDir)
		if err != nil {
			// Log the error but continue - file-based templates are optional
			slog.Warn("api: failed to load file-based templates", "dir", examplesDir, "error", err)
		}
	}

	if fileTemplates != nil {
		for _, ft := range fileTemplates {
			allTemplates = append(allTemplates, TemplateListResponse{
				ID:          ft.ID,
				Key:         ft.Key,
				Name:        ft.Name,
				Description: ft.Description,
				Source:      ft.Source,
				IsDefault:   ft.IsDefault,
				CreatedAt:   time.Now(),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allTemplates)
}

// CreateTemplate saves a project as a template.
func (h *Handler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	var req createTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ProjectID == "" {
		writeJSONError(w, http.StatusBadRequest, "project_id is required")
		return
	}

	if !h.requireProjectRole(w, r, req.ProjectID, members.RoleEditor) {
		return
	}

	created, err := h.templateService.CreateTemplateFromProject(req.ProjectID, req.Name, req.Description, ActiveOrg(r))
	if err != nil {
		respondInternal(w, r, "failed to create template", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

type createProjectFromTemplateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// CreateProjectFromTemplate creates a new project from a template.
func (h *Handler) CreateProjectFromTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := mux.Vars(r)["id"]

	if CurrentUser(r) == nil {
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	orgID := ActiveOrg(r)
	if orgID == "" {
		writeJSONError(w, http.StatusBadRequest, "no active workspace for this request")
		return
	}

	var req createProjectFromTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Try to load file-based template first
	// Check multiple possible locations
	var examplesDir string
	possiblePaths := []string{
		"examples",
		"./examples",
		"/root/examples",
	}

	for _, path := range possiblePaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			examplesDir = path
			break
		}
	}

	var snapshot []byte
	var err error

	if examplesDir != "" {
		// Try to load file-based template first
		snapshot, err = templates.GetFileBasedTemplateSnapshot(examplesDir, templateID)
	}

	if snapshot == nil || err != nil {
		// Fall back to database template
		projectID, dbErr := h.templateService.CreateProjectFromTemplate(templateID, req.Name, req.Description, orgID)
		if dbErr != nil {
			respondInternal(w, r, "failed to create project from template", dbErr)
			return
		}

		h.addProjectCreatorAsOwner(r, projectID)

		project, err := h.projectService.GetProject(projectID)
		if err != nil {
			respondInternal(w, r, "failed to load project", err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(project)
		return
	}

	// Use file-based template
	projectID, err := h.exportService.ImportProjectWithOverrides(snapshot, req.Name, req.Description, orgID)
	if err != nil {
		respondInternal(w, r, "failed to create project from template", err)
		return
	}

	h.addProjectCreatorAsOwner(r, projectID)

	project, err := h.projectService.GetProject(projectID)
	if err != nil {
		respondInternal(w, r, "failed to load project", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(project)
}

// addProjectCreatorAsOwner grants the requesting user owner membership on a
// freshly created project (mirrors CreateProject).
func (h *Handler) addProjectCreatorAsOwner(r *http.Request, projectID string) {
	if user := CurrentUser(r); user != nil && h.memberService != nil {
		if err := h.memberService.AddMember(projectID, user.ID, members.RoleOwner); err != nil {
			slog.Warn("api: failed to add creator as project owner", "project_id", projectID, "error", err)
		}
	}
}

type createBaselineRequest struct {
	Name string `json:"name"`
}

// CreateBaseline captures a baseline snapshot for a project.
func (h *Handler) CreateBaseline(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}

	var req createBaselineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := req.Name
	if name == "" {
		name = fmt.Sprintf("Baseline %s", time.Now().Format("2006-01-02 15:04"))
	}

	data, _, err := h.exportService.ExportProject(projectID, exports.FormatJSON)
	if err != nil {
		respondInternal(w, r, "failed to export project", err)
		return
	}

	baseline, err := h.baselineService.CreateBaseline(projectID, name, data)
	if err != nil {
		respondInternal(w, r, "failed to create baseline", err)
		return
	}

	h.publish(r, events.BaselineCaptured, projectID, baseline.ID, map[string]interface{}{
		"name": name,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(baseline)
}

// ListBaselines returns baselines for a project.
func (h *Handler) ListBaselines(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]

	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}

	baselines, err := h.baselineService.ListBaselines(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list baselines", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(baselines)
}

// GetBaseline returns the snapshot JSON for a baseline.
func (h *Handler) GetBaseline(w http.ResponseWriter, r *http.Request) {
	baselineID := mux.Vars(r)["id"]

	baseline, err := h.baselineService.GetBaseline(baselineID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "baseline not found", err)
		return
	}

	if !h.requireProjectRole(w, r, baseline.ProjectID, members.RoleViewer) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(baseline.Snapshot)
}

// DeleteBaseline deletes a baseline by ID.
func (h *Handler) DeleteBaseline(w http.ResponseWriter, r *http.Request) {
	baselineID := mux.Vars(r)["id"]

	baseline, err := h.baselineService.GetBaseline(baselineID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "baseline not found", err)
		return
	}
	if !h.requireProjectRole(w, r, baseline.ProjectID, members.RoleOwner) {
		return
	}

	if err := h.baselineService.DeleteBaseline(baselineID); err != nil {
		respondInternal(w, r, "failed to delete baseline", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UploadAttachment uploads an image attachment for an artifact
func (h *Handler) UploadAttachment(w http.ResponseWriter, r *http.Request) {
	artifactID := r.FormValue("artifact_id")
	if artifactID == "" {
		writeJSONError(w, http.StatusBadRequest, "artifact_id is required")
		return
	}

	if !h.requireProjectRole(w, r, h.projectIDForArtifact(artifactID), members.RoleEditor) {
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "Failed to get file from request")
		return
	}
	defer file.Close()

	// Validate file is an image
	mimeType := header.Header.Get("Content-Type")
	if !isImageMimeType(mimeType) {
		writeJSONError(w, http.StatusBadRequest, "File must be an image")
		return
	}

	// Read file content
	fileData, err := io.ReadAll(file)
	if err != nil {
		respondInternal(w, r, "Failed to read file", err)
		return
	}

	// Generate unique filename
	filename := fmt.Sprintf("%s_%s", uuid.New().String(), header.Filename)
	filepath := filepath.Join(h.uploadsDir, filename)

	// Write file to disk
	if err := os.WriteFile(filepath, fileData, 0644); err != nil {
		respondInternal(w, r, "Failed to save file", err)
		return
	}

	// Create attachment record
	attachment := attachments.NewAttachment(attachments.CreateAttachmentRequest{
		ArtifactID: artifactID,
		Filename:   header.Filename,
		MimeType:   mimeType,
		FilePath:   filepath,
		FileSize:   len(fileData),
	})

	if err := h.attachmentService.CreateAttachment(attachment); err != nil {
		// Clean up file if database save fails
		_ = os.Remove(filepath)
		respondInternal(w, r, "Failed to save attachment metadata", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(attachment)
}

// GetAttachmentMeta retrieves attachment metadata
func (h *Handler) GetAttachmentMeta(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	attachment, err := h.attachmentService.GetAttachment(id)
	if err != nil || attachment == nil {
		writeJSONError(w, http.StatusNotFound, "Attachment not found")
		return
	}

	if !h.requireProjectRole(w, r, h.projectIDForArtifact(attachment.ArtifactID), members.RoleViewer) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(attachment)
}

// DownloadAttachment serves the attachment file
func (h *Handler) DownloadAttachment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	attachment, err := h.attachmentService.GetAttachment(id)
	if err != nil || attachment == nil {
		writeJSONError(w, http.StatusNotFound, "Attachment not found")
		return
	}

	// Session-cookie auth works here too, so <img> tags keep rendering.
	if !h.requireProjectRole(w, r, h.projectIDForArtifact(attachment.ArtifactID), members.RoleViewer) {
		return
	}

	// Set appropriate headers for image serving
	w.Header().Set("Content-Type", attachment.MimeType)
	w.Header().Set("Content-Length", strconv.Itoa(attachment.FileSize))
	w.Header().Set("Cache-Control", "public, max-age=31536000")

	// Serve the file
	http.ServeFile(w, r, attachment.FilePath)
}

// DeleteAttachment deletes an attachment
func (h *Handler) DeleteAttachment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	attachment, err := h.attachmentService.GetAttachment(id)
	if err != nil || attachment == nil {
		writeJSONError(w, http.StatusNotFound, "Attachment not found")
		return
	}

	if !h.requireProjectRole(w, r, h.projectIDForArtifact(attachment.ArtifactID), members.RoleEditor) {
		return
	}

	// Delete file from disk
	if err := os.Remove(attachment.FilePath); err != nil && !os.IsNotExist(err) {
		respondInternal(w, r, "Failed to delete file", err)
		return
	}

	// Delete database record
	if err := h.attachmentService.DeleteAttachment(id); err != nil {
		respondInternal(w, r, "Failed to delete attachment", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListArtifactAttachments lists all attachments for an artifact
func (h *Handler) ListArtifactAttachments(w http.ResponseWriter, r *http.Request) {
	artifactID := mux.Vars(r)["artifactID"]

	if !h.requireProjectRole(w, r, h.projectIDForArtifact(artifactID), members.RoleViewer) {
		return
	}

	attachmentList, err := h.attachmentService.GetAttachmentsByArtifact(artifactID)
	if err != nil {
		respondInternal(w, r, "failed to list attachments", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(attachmentList)
}

// isImageMimeType checks if the mime type is a valid image type
func isImageMimeType(mimeType string) bool {
	validTypes := map[string]bool{
		"image/jpeg":    true,
		"image/png":     true,
		"image/gif":     true,
		"image/webp":    true,
		"image/svg+xml": true,
		"image/tiff":    true,
		"image/bmp":     true,
	}
	return validTypes[mimeType]
}

// buildChangesSummary creates a detailed summary of what changed in the artifact
func (h *Handler) buildChangesSummary(oldArtifact, newArtifact *artifacts.Artifact, addedLinks, removedLinks []*links.Link) string {
	// Get basic field changes
	changes := h.buildChangesList(oldArtifact, newArtifact, addedLinks, removedLinks)

	// Add detailed link changes
	if len(addedLinks) > 0 {
		changes = append(changes, fmt.Sprintf("Links:"))
		for _, link := range addedLinks {
			var otherArtifactID string
			var linkDirection string

			// Determine if this artifact is the source or target
			if link.FromID == newArtifact.ID {
				otherArtifactID = link.ToID
				linkDirection = link.Type
			} else {
				otherArtifactID = link.FromID
				linkDirection = link.Type
			}

			// Try to get the other artifact's title
			otherArtifact, err := h.artifactService.GetArtifact(otherArtifactID)
			var artifactTitle string
			if err == nil {
				artifactTitle = otherArtifact.Title
			} else {
				artifactTitle = otherArtifactID
			}

			changes = append(changes, fmt.Sprintf("    - %s: %s (added)", linkDirection, artifactTitle))
		}
	}

	if len(removedLinks) > 0 {
		if len(addedLinks) == 0 {
			changes = append(changes, fmt.Sprintf("Links:"))
		}
		for _, link := range removedLinks {
			var otherArtifactID string
			var linkDirection string

			// Determine if this artifact is the source or target
			if link.FromID == newArtifact.ID {
				otherArtifactID = link.ToID
				linkDirection = link.Type
			} else {
				otherArtifactID = link.FromID
				linkDirection = link.Type
			}

			// Try to get the other artifact's title
			otherArtifact, err := h.artifactService.GetArtifact(otherArtifactID)
			var artifactTitle string
			if err == nil {
				artifactTitle = otherArtifact.Title
			} else {
				artifactTitle = otherArtifactID
			}

			changes = append(changes, fmt.Sprintf("    - %s: %s (removed)", linkDirection, artifactTitle))
		}
	}

	// Build message
	message := fmt.Sprintf("Updated to version %d", newArtifact.Version)

	if len(changes) > 0 {
		message += "\n\nChanges:\n"
		for _, change := range changes {
			if strings.HasPrefix(change, "  -") || strings.HasPrefix(change, "  +") {
				// Diff lines - include as-is (indented)
				message += change + "\n"
			} else if strings.HasPrefix(change, "    ") {
				// Indented lines (link details)
				message += change + "\n"
			} else if strings.HasPrefix(change, "Links:") {
				// Links header
				message += "- " + change + "\n"
			} else if strings.HasPrefix(change, "Description modified:") {
				// Description header
				message += "- " + change + "\n"
			} else {
				// Regular changes
				message += "- " + change + "\n"
			}
		}
	}

	return message
}

// linksFromPendingAdds converts the pending link-add payloads of an artifact
// update ({from_id,to_id,type,...} objects, mirroring what
// processManagedLinkChanges creates) into Link values for the chatter
// summary. Entries missing any of the three required fields are skipped,
// matching the create path's behavior.
func linksFromPendingAdds(toAdd []interface{}) []*links.Link {
	var added []*links.Link
	for _, linkDataInterface := range toAdd {
		linkData, ok := linkDataInterface.(map[string]interface{})
		if !ok {
			continue
		}
		fromID, _ := linkData["from_id"].(string)
		toID, _ := linkData["to_id"].(string)
		linkType, _ := linkData["type"].(string)
		if fromID == "" || toID == "" || linkType == "" {
			continue
		}
		added = append(added, &links.Link{FromID: fromID, ToID: toID, Type: linkType})
	}
	return added
}

// processManagedLinkChanges handles link additions and removals
// Returns list of artifact IDs that had links change (for auto-versioning)
// baseProjectID is the project of the artifact being updated; the caller has
// already verified editor rights on it. Entries touching artifacts in other
// projects are skipped unless the caller has editor rights there too.
func (h *Handler) processManagedLinkChanges(r *http.Request, baseProjectID, fromArtifactID string, toAdd, toRemove []interface{}) ([]string, error) {
	affectedArtifactIDs := make(map[string]bool) // Use map to avoid duplicates

	// canEditLinkedArtifact reports whether the caller may edit the project
	// the given artifact belongs to (the base project is already authorized).
	canEditLinkedArtifact := func(artifactID string) bool {
		artifact, err := h.artifactService.GetArtifact(artifactID)
		if err != nil || artifact == nil {
			return false
		}
		return artifact.ProjectID == baseProjectID || h.hasProjectRole(r, artifact.ProjectID, members.RoleEditor)
	}

	// Process removals (hard delete from table)
	for _, linkIDInterface := range toRemove {
		linkID, ok := linkIDInterface.(string)
		if !ok {
			continue
		}

		// Get the link before deleting to determine affected artifact
		link, err := h.linkService.GetLink(linkID)
		if err == nil && link != nil {
			// Both endpoints may live outside the base project; the caller
			// needs editor rights on their projects to remove the link.
			if !canEditLinkedArtifact(link.FromID) || !canEditLinkedArtifact(link.ToID) {
				slog.Warn("api: skipping link removal, no editor access to a linked artifact's project", "link_id", linkID)
				continue
			}
			// Mark the 'to' artifact as affected (it had an incoming link removed)
			if link.ToID == fromArtifactID {
				affectedArtifactIDs[link.FromID] = true
			} else {
				affectedArtifactIDs[link.ToID] = true
			}
		}

		// Hard delete the link
		err = h.linkService.DeleteLink(linkID)
		if err != nil {
			slog.Warn("api: failed to delete link", "link_id", linkID, "error", err)
		}
	}

	// Process additions (create new links)
	for _, linkDataInterface := range toAdd {
		linkDataMap, ok := linkDataInterface.(map[string]interface{})
		if !ok {
			continue
		}

		fromID, ok := linkDataMap["from_id"].(string)
		if !ok {
			continue
		}
		toID, ok := linkDataMap["to_id"].(string)
		if !ok {
			continue
		}
		linkType, ok := linkDataMap["type"].(string)
		if !ok {
			continue
		}

		// Get or create attributes
		var attributes map[string]interface{}
		if attrs, ok := linkDataMap["attributes"].(map[string]interface{}); ok {
			attributes = attrs
		} else {
			attributes = make(map[string]interface{})
		}

		// Validate link type against artifact types
		fromArtifact, err := h.artifactService.GetArtifact(fromID)
		if err != nil {
			slog.Warn("api: failed to get source artifact for link validation", "artifact_id", fromID, "error", err)
			continue
		}

		toArtifact, err := h.artifactService.GetArtifact(toID)
		if err != nil {
			slog.Warn("api: failed to get target artifact for link validation", "artifact_id", toID, "error", err)
			continue
		}

		if err := links.ValidateLinkType(linkType, fromArtifact.Type, toArtifact.Type); err != nil {
			slog.Warn("api: invalid link type", "error", err)
			continue
		}

		// Both endpoints get a version bump + chatter; a cross-project link
		// needs editor rights on the other project too.
		if fromArtifact.ProjectID != baseProjectID && !h.hasProjectRole(r, fromArtifact.ProjectID, members.RoleEditor) {
			slog.Warn("api: skipping link add, no editor access to the source artifact's project", "from_id", fromID, "to_id", toID)
			continue
		}
		if toArtifact.ProjectID != baseProjectID && !h.hasProjectRole(r, toArtifact.ProjectID, members.RoleEditor) {
			slog.Warn("api: skipping link add, no editor access to the target artifact's project", "from_id", fromID, "to_id", toID)
			continue
		}

		// Create the link
		linkReq := links.CreateLinkRequest{
			FromID:     fromID,
			ToID:       toID,
			Type:       linkType,
			Attributes: attributes,
		}
		link := links.NewLink(linkReq)
		err = h.linkService.CreateLink(link)
		if err != nil {
			slog.Warn("api: failed to create link", "from_id", fromID, "to_id", toID, "error", err)
			continue
		}

		// Mark both artifacts as affected
		affectedArtifactIDs[fromID] = true
		affectedArtifactIDs[toID] = true
	}

	// Convert map to slice
	result := make([]string, 0, len(affectedArtifactIDs))
	for id := range affectedArtifactIDs {
		if id != fromArtifactID { // Don't include the artifact we're currently updating
			result = append(result, id)
		}
	}

	return result, nil
}

// autoVersionLinkedArtifacts creates new versions for artifacts that had link changes
func (h *Handler) autoVersionLinkedArtifacts(affectedArtifactIDs []string) error {
	for _, artifactID := range affectedArtifactIDs {
		artifact, err := h.artifactService.GetArtifact(artifactID)
		if err != nil {
			slog.Warn("api: could not find artifact for auto-versioning", "artifact_id", artifactID, "error", err)
			continue
		}

		// Get current links for this artifact and deduplicate
		seenLinkIDs := make(map[string]bool)
		allLinks := make([]interface{}, 0)

		incomingLinks, err := h.linkService.GetLinksTo(artifactID)
		if err != nil {
			slog.Warn("api: could not get incoming links", "artifact_id", artifactID, "error", err)
			continue
		}

		outgoingLinks, err := h.linkService.GetLinksFrom(artifactID)
		if err != nil {
			slog.Warn("api: could not get outgoing links", "artifact_id", artifactID, "error", err)
			continue
		}

		// Combine all links with deduplication
		for _, link := range incomingLinks {
			if !seenLinkIDs[link.ID] {
				seenLinkIDs[link.ID] = true
				allLinks = append(allLinks, link)
			}
		}
		for _, link := range outgoingLinks {
			if !seenLinkIDs[link.ID] {
				seenLinkIDs[link.ID] = true
				allLinks = append(allLinks, link)
			}
		}

		// Ensure attributes is initialized
		attributes := artifact.Attributes
		if attributes == nil {
			attributes = make(map[string]interface{})
		}

		// Merge link snapshot into attributes
		attributes["links_snapshot"] = allLinks

		// Create a new version with updated link snapshot. Content fields
		// (type/title/body) are left nil — "no change" per the issue-#170
		// contract — so this attribute-only write carries them forward and
		// never counts as a content edit (no suspect-link flagging).
		// Structural fields are omitted entirely: an absent ParentID
		// (issue-#172 contract) and nil SortOrder both mean "no change",
		// so the artifact keeps its place in the tree.
		updateReq := artifacts.UpdateArtifactRequest{
			Attributes: attributes,
		}

		_, err = h.artifactService.UpdateArtifact(artifactID, updateReq)
		if err != nil {
			slog.Warn("api: failed to auto-version artifact", "artifact_id", artifactID, "error", err)
			continue
		}

		// Create chatter entry for this auto-version
		newVersion := artifact.Version + 1
		chatterMessage := fmt.Sprintf("Auto-updated to version %d due to link changes", newVersion)
		chatterEntry := chatter.NewChatterEntry(artifactID, chatterMessage, true, "link-change")
		if err := h.chatterService.CreateEntry(chatterEntry); err != nil {
			slog.Warn("api: failed to create chatter entry for auto-versioned artifact", "artifact_id", artifactID, "error", err)
		}

		slog.Debug("api: auto-versioned artifact due to link changes",
			"artifact_id", artifactID, "incoming_links", len(incomingLinks), "outgoing_links", len(outgoingLinks))
	}

	return nil
}

// CreateChatterEntry creates a new chatter entry
func (h *Handler) CreateChatterEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ArtifactID string `json:"artifact_id"`
		Message    string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !h.requireProjectRole(w, r, h.projectIDForArtifact(req.ArtifactID), members.RoleEditor) {
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
	if err := h.chatterService.CreateEntry(entry); err != nil {
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entry)
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

	entries, err := h.chatterService.GetEntriesByArtifactID(artifactID)
	if err != nil {
		respondInternal(w, r, "failed to load chatter entries", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}
