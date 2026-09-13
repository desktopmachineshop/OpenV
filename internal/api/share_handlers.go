package api

// Project share links (REQ-149, REQ-150) and the open-source showcase
// (REQ-151).
//
// A public link is the project, live and read only, for anyone who holds
// it. A reviewer link grants the reviewer role to a signed-in account. Both
// resolve under /api/v1/public/share/{token}, which the auth middleware
// leaves open; the token is the credential, so it is rate-limited like an
// invite preview and redacted from the request log. The share page and the
// preview image exist for link unfurlers: Slack, Discord, LinkedIn and the
// rest read Open Graph tags from the HTML the link points at, and never run
// the app, so the API answers those tags itself and the app takes over from
// there.

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/projects"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/sharelinks"
)

func (h *Handler) registerShareRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/projects/{id}/share-links", h.ListShareLinks).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/share-links", h.CreateShareLink).Methods("POST")
	router.HandleFunc("/api/v1/share-links/{id}", h.RevokeShareLink).Methods("DELETE")
	router.HandleFunc("/api/v1/public/share/{token}", h.OpenShareLink).Methods("GET")
	router.HandleFunc("/api/v1/public/share/{token}/page", h.ShareLinkPage).Methods("GET")
	router.HandleFunc("/api/v1/public/share/{token}/preview.png", h.ShareLinkPreview).Methods("GET")
	router.HandleFunc("/api/v1/auth/share/accept", h.AcceptShareLink).Methods("POST")
	router.HandleFunc("/api/v1/public/open-source/projects", h.ListOpenSourceProjects).Methods("GET")
	router.HandleFunc("/api/v1/public/open-source/projects/{id}", h.GetOpenSourceProject).Methods("GET")
	router.HandleFunc("/api/v1/public/open-source/projects/{id}/page", h.OpenSourceProjectPage).Methods("GET")
	router.HandleFunc("/api/v1/public/open-source/projects/{id}/preview.png", h.OpenSourceProjectPreview).Methods("GET")
}

// shareLinkResponse is a link plus, once only, its token and the paths it
// opens at.
type shareLinkResponse struct {
	*sharelinks.Link
	Token string `json:"token,omitempty"`
	// URL is the link to hand out: the share page, which unfurls with a
	// preview and then opens the app.
	URL string `json:"url,omitempty"`
}

func (h *Handler) shareURL(token string) string {
	return h.frontendURL + "/share/" + token
}

// ListShareLinks answers a project's links (owner).
func (h *Handler) ListShareLinks(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleOwner) {
		return
	}
	list, err := h.shareLinkService.List(projectID)
	if err != nil {
		respondInternal(w, r, "failed to list share links", err)
		return
	}
	out := make([]shareLinkResponse, 0, len(list))
	for _, l := range list {
		out = append(out, shareLinkResponse{Link: l})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// CreateShareLink mints a link: {"role": "public"|"reviewer", "label",
// "expires_at"?}. The token is in this answer and nowhere else.
func (h *Handler) CreateShareLink(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleOwner) {
		return
	}
	if !h.projectFeatureEnabled(r, projectID, release.FeatureShareLinks) {
		writeJSONError(w, http.StatusForbidden, featureGateMessage)
		return
	}
	var req struct {
		Role      string     `json:"role"`
		Label     string     `json:"label"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	link, token, err := h.shareLinkService.Create(projectID, req.Role, req.Label, CurrentUserID(r), req.ExpiresAt)
	if err != nil {
		if errors.Is(err, sharelinks.ErrInvalidRole) {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondInternal(w, r, "failed to create share link", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(shareLinkResponse{Link: link, Token: token, URL: h.shareURL(token)})
}

// RevokeShareLink stops a link opening anything (owner of its project).
func (h *Handler) RevokeShareLink(w http.ResponseWriter, r *http.Request) {
	link, err := h.shareLinkService.Get(mux.Vars(r)["id"])
	if err != nil || link == nil {
		respondError(w, r, http.StatusNotFound, "share link not found", err)
		return
	}
	if !h.requireProjectRole(w, r, link.ProjectID, members.RoleOwner) {
		return
	}
	if err := h.shareLinkService.Revoke(link.ID); err != nil {
		respondInternal(w, r, "failed to revoke share link", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// sharedProject is what a share link or the open-source page shows.
type sharedProject struct {
	Role    string `json:"role"`
	Project struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"project"`
	Workspace string `json:"workspace"`
	// Baseline names the snapshot shown, when it is one rather than the
	// live project.
	Baseline *struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"baseline,omitempty"`
	Counts   map[string]int         `json:"counts"`
	Snapshot *exports.ProjectExport `json:"snapshot,omitempty"`
}

func (h *Handler) describeProject(project *projects.Project) (view sharedProject) {
	view.Project.ID, view.Project.Name, view.Project.Description = project.ID, project.Name, project.Description
	if h.orgService != nil && project.OrgID != "" {
		if org, err := h.orgService.Get(project.OrgID); err == nil && org != nil {
			view.Workspace = org.Name
		}
	}
	view.Counts = map[string]int{}
	return view
}

// artifactCounts tallies a snapshot by artifact type, headings excluded.
func artifactCounts(e *exports.ProjectExport) map[string]int {
	counts := map[string]int{}
	if e == nil {
		return counts
	}
	for _, a := range e.Artifacts {
		if a != nil && a.Type != artifacts.TypeHeading {
			counts[a.Type]++
		}
	}
	return counts
}

// countsLine words the counts for a card: "31 requirements · 6 test cases".
func countsLine(counts map[string]int) string {
	order := []string{"requirement", "user-need", "design-item", "test-case", "hazard"}
	names := map[string]string{"requirement": "requirements", "user-need": "needs", "design-item": "design items", "test-case": "test cases", "hazard": "hazards"}
	var parts []string
	for _, t := range order {
		if n := counts[t]; n > 0 {
			label := names[t]
			if n == 1 {
				label = strings.TrimSuffix(label, "s")
			}
			parts = append(parts, fmt.Sprintf("%d %s", n, label))
		}
	}
	return strings.Join(parts, " · ")
}

// allowPublicShare rate-limits a token lookup per address, the way invite
// previews are: a token is a credential and guessing must stay slow.
func (h *Handler) allowPublicShare(w http.ResponseWriter, r *http.Request) bool {
	if ok, _ := h.invitePreviewLimiter.allow(clientIP(r)); !ok {
		writeJSONError(w, http.StatusTooManyRequests, "too many requests")
		return false
	}
	return true
}

// resolveShare answers the link a token opens and its project, or writes
// the one 404 every unusable link gets.
func (h *Handler) resolveShare(w http.ResponseWriter, r *http.Request) (*sharelinks.Link, *projects.Project) {
	if !h.allowPublicShare(w, r) {
		return nil, nil
	}
	link, err := h.shareLinkService.Resolve(mux.Vars(r)["token"])
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "this link does not open anything: it may have been revoked or expired")
		return nil, nil
	}
	project, err := h.projectService.GetProject(link.ProjectID)
	if err != nil || project == nil {
		writeJSONError(w, http.StatusNotFound, "project not found")
		return nil, nil
	}
	return link, project
}

// OpenShareLink answers what a link shows. A public link carries the live
// project as a read-only snapshot; a reviewer link carries the project's
// name only, since the person signs in and then uses the app.
func (h *Handler) OpenShareLink(w http.ResponseWriter, r *http.Request) {
	link, project := h.resolveShare(w, r)
	if link == nil {
		return
	}
	view := h.describeProject(project)
	view.Role = link.Role
	if link.Role == sharelinks.RolePublic {
		snapshot, err := h.exportService.PrepareExport(project.ID, false)
		if err != nil {
			respondInternal(w, r, "failed to read project", err)
			return
		}
		view.Snapshot = snapshot
		view.Counts = artifactCounts(snapshot)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(view)
}

// AcceptShareLink takes a reviewer link up for the signed-in account:
// {"token"} → {project_id, project_name, role}. An account that already
// holds a stronger role keeps it. Session cookie only, like an invitation.
func (h *Handler) AcceptShareLink(w http.ResponseWriter, r *http.Request) {
	user := h.sessionUser(r)
	if user == nil {
		writeJSONError(w, http.StatusUnauthorized, "sign in to review this project")
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !h.allowPublicShare(w, r) {
		return
	}
	link, err := h.shareLinkService.Resolve(req.Token)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "this link does not open anything: it may have been revoked or expired")
		return
	}
	if link.Role != sharelinks.RoleReviewer {
		writeJSONError(w, http.StatusBadRequest, "this link opens without an account")
		return
	}
	project, err := h.projectService.GetProject(link.ProjectID)
	if err != nil || project == nil {
		writeJSONError(w, http.StatusNotFound, "project not found")
		return
	}
	role, _ := h.memberService.EffectiveRole(project.ID, user.ID)
	if role == "" || !members.RoleAtLeast(role, members.RoleReviewer) {
		if err := h.memberService.AddMember(project.ID, user.ID, members.RoleReviewer); err != nil {
			respondInternal(w, r, "failed to grant review access", err)
			return
		}
		role = members.RoleReviewer
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"project_id": project.ID, "project_name": project.Name, "role": role})
}

// --- Social previews -------------------------------------------------------

// previewPage is the HTML an unfurler reads: Open Graph and Twitter card
// tags naming the project, then a refresh into the app for a person.
func previewPage(title, description, pageURL, imageURL, appURL string) string {
	esc := html.EscapeString
	return `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width, initial-scale=1">` +
		`<meta name="robots" content="noindex">` +
		`<title>` + esc(title) + `</title>` +
		`<meta name="description" content="` + esc(description) + `">` +
		`<meta property="og:type" content="website">` +
		`<meta property="og:site_name" content="OpenV">` +
		`<meta property="og:title" content="` + esc(title) + `">` +
		`<meta property="og:description" content="` + esc(description) + `">` +
		`<meta property="og:url" content="` + esc(pageURL) + `">` +
		`<meta property="og:image" content="` + esc(imageURL) + `">` +
		`<meta property="og:image:width" content="1200">` +
		`<meta property="og:image:height" content="630">` +
		`<meta name="twitter:card" content="summary_large_image">` +
		`<meta name="twitter:title" content="` + esc(title) + `">` +
		`<meta name="twitter:description" content="` + esc(description) + `">` +
		`<meta name="twitter:image" content="` + esc(imageURL) + `">` +
		`<meta http-equiv="refresh" content="0; url=` + esc(appURL) + `">` +
		`</head><body style="font-family: system-ui, sans-serif; padding: 24px; color: #1f2d3d">` +
		`<p>` + esc(title) + `</p><p>` + esc(description) + `</p>` +
		`<p><a href="` + esc(appURL) + `">Open in OpenV</a></p></body></html>`
}

func (h *Handler) publicAPIBase(r *http.Request) string {
	if h.publicAPIURL != "" {
		return strings.TrimRight(h.publicAPIURL, "/")
	}
	scheme := "https"
	if r.TLS == nil && !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

func truncateWords(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	cut := strings.LastIndex(s[:max], " ")
	if cut < max/2 {
		cut = max
	}
	return strings.TrimSpace(s[:cut]) + "…"
}

// ShareLinkPage serves the unfurlable page for a share link.
func (h *Handler) ShareLinkPage(w http.ResponseWriter, r *http.Request) {
	link, project := h.resolveShare(w, r)
	if link == nil {
		return
	}
	token := mux.Vars(r)["token"]
	view := h.describeProject(project)
	kind := "Shared from OpenV, view only"
	if link.Role == sharelinks.RoleReviewer {
		kind = "Shared from OpenV for review"
	}
	description := kind
	if project.Description != "" {
		description = truncateWords(project.Description, 180) + " — " + kind
	}
	title := project.Name
	if view.Workspace != "" {
		title += " · " + view.Workspace
	}
	base := h.publicAPIBase(r)
	page := previewPage(title, description, h.shareURL(token), base+"/api/v1/public/share/"+token+"/preview.png", h.frontendURL+"/s/"+token)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(page))
}

// ShareLinkPreview draws the card for a share link.
func (h *Handler) ShareLinkPreview(w http.ResponseWriter, r *http.Request) {
	link, project := h.resolveShare(w, r)
	if link == nil {
		return
	}
	view := h.describeProject(project)
	eyebrow := "Shared specification · view only"
	if link.Role == sharelinks.RoleReviewer {
		eyebrow = "Shared for review"
	}
	var lines []string
	if snapshot, err := h.exportService.PrepareExport(project.ID, false); err == nil {
		if l := countsLine(artifactCounts(snapshot)); l != "" {
			lines = append(lines, l)
		}
	}
	if project.Description != "" {
		lines = append(lines, truncateWords(project.Description, 120))
	}
	h.writePreview(w, previewCard{Eyebrow: eyebrow, Title: project.Name, Subtitle: view.Workspace, Lines: lines, Footer: "Requirements, traceability and V&V evidence"})
}

func (h *Handler) writePreview(w http.ResponseWriter, card previewCard) {
	data, err := renderPreview(card)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not draw the preview")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write(data)
}

// --- Open-source showcase (REQ-151) ------------------------------------------

// openSourceEntry is one project on the open-source page.
type openSourceEntry struct {
	ProjectID   string         `json:"project_id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Workspace   string         `json:"workspace"`
	BaselineID  string         `json:"baseline_id"`
	Baseline    string         `json:"baseline"`
	SnapshotAt  time.Time      `json:"snapshot_at"`
	Counts      map[string]int `json:"counts"`
}

// openSourceOrgs lists the workspaces on the open-source plan.
func (h *Handler) openSourceOrgs() ([]*orgs.Org, error) {
	if h.orgService == nil {
		return nil, nil
	}
	ids, err := h.orgService.ListAll()
	if err != nil {
		return nil, err
	}
	var out []*orgs.Org
	for _, id := range ids {
		org, err := h.orgService.Get(id)
		if err != nil || org == nil || org.Plan != orgs.PlanOpenSource {
			continue
		}
		out = append(out, org)
	}
	return out, nil
}

// latestSnapshot reads a project's newest baseline, nil when it has none.
func (h *Handler) latestSnapshot(projectID string) (id, name string, at time.Time, snapshot *exports.ProjectExport) {
	list, err := h.baselineService.ListBaselines(projectID)
	if err != nil || len(list) == 0 {
		return "", "", time.Time{}, nil
	}
	latest := list[0]
	for _, b := range list[1:] {
		if b.CreatedAt.After(latest.CreatedAt) {
			latest = b
		}
	}
	var data exports.ProjectExport
	if err := json.Unmarshal(latest.Snapshot, &data); err != nil {
		return "", "", time.Time{}, nil
	}
	return latest.ID, latest.Name, latest.CreatedAt, &data
}

// ListOpenSourceProjects lists every project of an open-source workspace
// that has a baseline: the deal for free hosting is that the latest
// snapshot is public. Live work is not listed.
func (h *Handler) ListOpenSourceProjects(w http.ResponseWriter, r *http.Request) {
	orgList, err := h.openSourceOrgs()
	if err != nil {
		respondInternal(w, r, "failed to list open-source workspaces", err)
		return
	}
	out := []openSourceEntry{}
	for _, org := range orgList {
		list, err := h.projectService.ListProjectsByOrg(org.ID)
		if err != nil {
			continue
		}
		for _, p := range list {
			id, name, at, snapshot := h.latestSnapshot(p.ID)
			if snapshot == nil {
				continue
			}
			out = append(out, openSourceEntry{
				ProjectID: p.ID, Name: p.Name, Description: p.Description, Workspace: org.Name,
				BaselineID: id, Baseline: name, SnapshotAt: at, Counts: artifactCounts(snapshot),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SnapshotAt.After(out[j].SnapshotAt) })
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// openSourceProject resolves a project that is public by its workspace's
// plan, with its latest snapshot; 404 otherwise, one message for every
// reason so a project id learns nothing about private projects.
func (h *Handler) openSourceProject(w http.ResponseWriter, r *http.Request) (*projects.Project, *orgs.Org, sharedProject) {
	project, err := h.projectService.GetProject(mux.Vars(r)["id"])
	if err != nil || project == nil || project.OrgID == "" || h.orgService == nil {
		writeJSONError(w, http.StatusNotFound, "project not found")
		return nil, nil, sharedProject{}
	}
	org, err := h.orgService.Get(project.OrgID)
	if err != nil || org == nil || org.Plan != orgs.PlanOpenSource {
		writeJSONError(w, http.StatusNotFound, "project not found")
		return nil, nil, sharedProject{}
	}
	id, name, at, snapshot := h.latestSnapshot(project.ID)
	if snapshot == nil {
		writeJSONError(w, http.StatusNotFound, "project not found")
		return nil, nil, sharedProject{}
	}
	view := h.describeProject(project)
	view.Role = sharelinks.RolePublic
	view.Baseline = &struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"created_at"`
	}{ID: id, Name: name, CreatedAt: at}
	view.Counts = artifactCounts(snapshot)
	view.Snapshot = snapshot
	return project, org, view
}

// GetOpenSourceProject answers the latest snapshot of a public project.
func (h *Handler) GetOpenSourceProject(w http.ResponseWriter, r *http.Request) {
	project, _, view := h.openSourceProject(w, r)
	if project == nil {
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(view)
}

// OpenSourceProjectPage is the unfurlable page for a public project.
func (h *Handler) OpenSourceProjectPage(w http.ResponseWriter, r *http.Request) {
	project, org, view := h.openSourceProject(w, r)
	if project == nil {
		return
	}
	description := "Open-source project on OpenV: " + countsLine(view.Counts)
	if project.Description != "" {
		description = truncateWords(project.Description, 180) + " — " + description
	}
	// The link handed out is /open-source/p/{id}, which the frontend's
	// nginx routes here; the app itself lives at /open-source/{id}.
	appURL := h.frontendURL + "/open-source/" + project.ID
	base := h.publicAPIBase(r)
	page := previewPage(project.Name+" · "+org.Name, description, h.frontendURL+"/open-source/p/"+project.ID, base+"/api/v1/public/open-source/projects/"+project.ID+"/preview.png", appURL)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write([]byte(page))
}

// OpenSourceProjectPreview draws the card for a public project.
func (h *Handler) OpenSourceProjectPreview(w http.ResponseWriter, r *http.Request) {
	project, org, view := h.openSourceProject(w, r)
	if project == nil {
		return
	}
	lines := []string{countsLine(view.Counts)}
	if project.Description != "" {
		lines = append(lines, truncateWords(project.Description, 120))
	}
	h.writePreview(w, previewCard{Eyebrow: "Open-source project", Title: project.Name, Subtitle: org.Name, Lines: lines, Footer: "Latest snapshot · " + view.Baseline.Name})
}
