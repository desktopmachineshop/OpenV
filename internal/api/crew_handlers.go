package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/crewtemplates"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/teams"
)

// registerCrewRoutes wires the crews, their templates, nodes, edges and
// runs on the canonical /crews routes, then the deprecated /teams aliases
// for the same handlers.
func (h *Handler) registerCrewRoutes(router *mux.Router) {
	// Crews (canonical routes).
	router.HandleFunc("/api/v1/crews", h.ListTeams).Methods("GET")
	router.HandleFunc("/api/v1/crews", h.CreateTeam).Methods("POST")
	router.HandleFunc("/api/v1/crews/{id}", h.GetTeam).Methods("GET")
	router.HandleFunc("/api/v1/crews/{id}", h.UpdateTeam).Methods("PUT")
	router.HandleFunc("/api/v1/crews/{id}", h.DeleteTeam).Methods("DELETE")
	router.HandleFunc("/api/v1/crews/{id}/clone", h.CloneTeam).Methods("POST")
	router.HandleFunc("/api/v1/crews/{id}/export", h.ExportCrew).Methods("GET")
	router.HandleFunc("/api/v1/crews/import", h.ImportCrew).Methods("POST")
	router.HandleFunc("/api/v1/crew-templates", h.ListCrewTemplates).Methods("GET")
	router.HandleFunc("/api/v1/crews/{id}/nodes", h.AddTeamNode).Methods("POST")
	router.HandleFunc("/api/v1/crews/{id}/runs", h.LaunchTeamRun).Methods("POST")
	router.HandleFunc("/api/v1/crew-nodes/{id}", h.UpdateTeamNode).Methods("PUT")
	router.HandleFunc("/api/v1/crew-nodes/{id}", h.RemoveTeamNode).Methods("DELETE")
	router.HandleFunc("/api/v1/crews/{id}/edges", h.AddTeamEdge).Methods("POST")
	router.HandleFunc("/api/v1/crew-edges/{id}", h.UpdateTeamEdge).Methods("PUT")
	router.HandleFunc("/api/v1/crew-edges/{id}", h.RemoveTeamEdge).Methods("DELETE")

	// Teams (deprecated aliases for the crews routes above).
	router.HandleFunc("/api/v1/teams", h.ListTeams).Methods("GET")                   // deprecated: use /api/v1/crews
	router.HandleFunc("/api/v1/teams", h.CreateTeam).Methods("POST")                 // deprecated: use /api/v1/crews
	router.HandleFunc("/api/v1/teams/{id}", h.GetTeam).Methods("GET")                // deprecated: use /api/v1/crews/{id}
	router.HandleFunc("/api/v1/teams/{id}", h.UpdateTeam).Methods("PUT")             // deprecated: use /api/v1/crews/{id}
	router.HandleFunc("/api/v1/teams/{id}", h.DeleteTeam).Methods("DELETE")          // deprecated: use /api/v1/crews/{id}
	router.HandleFunc("/api/v1/teams/{id}/clone", h.CloneTeam).Methods("POST")       // deprecated: use /api/v1/crews/{id}/clone
	router.HandleFunc("/api/v1/teams/{id}/export", h.ExportCrew).Methods("GET")      // deprecated: use /api/v1/crews/{id}/export
	router.HandleFunc("/api/v1/teams/import", h.ImportCrew).Methods("POST")          // deprecated: use /api/v1/crews/import
	router.HandleFunc("/api/v1/teams/{id}/nodes", h.AddTeamNode).Methods("POST")     // deprecated: use /api/v1/crews/{id}/nodes
	router.HandleFunc("/api/v1/teams/{id}/runs", h.LaunchTeamRun).Methods("POST")    // deprecated: use /api/v1/crews/{id}/runs
	router.HandleFunc("/api/v1/team-nodes/{id}", h.UpdateTeamNode).Methods("PUT")    // deprecated: use /api/v1/crew-nodes/{id}
	router.HandleFunc("/api/v1/team-nodes/{id}", h.RemoveTeamNode).Methods("DELETE") // deprecated: use /api/v1/crew-nodes/{id}
	router.HandleFunc("/api/v1/teams/{id}/edges", h.AddTeamEdge).Methods("POST")     // deprecated: use /api/v1/crews/{id}/edges
	router.HandleFunc("/api/v1/team-edges/{id}", h.UpdateTeamEdge).Methods("PUT")    // deprecated: use /api/v1/crew-edges/{id}
	router.HandleFunc("/api/v1/team-edges/{id}", h.RemoveTeamEdge).Methods("DELETE") // deprecated: use /api/v1/crew-edges/{id}
}

func (h *Handler) ListTeams(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	list, err := h.TeamService.ListTeams(ActiveOrg(r), r.URL.Query().Get("project_id"))
	if err != nil {
		respondInternal(w, r, "failed to list crews", err)
		return
	}
	json.NewEncoder(w).Encode(list)
}

func (h *Handler) CreateTeam(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	var req struct {
		Name        string  `json:"name"`
		Description string  `json:"description"`
		ProjectID   *string `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// A project-pinned crew must belong to a project in the same workspace.
	// A project the caller cannot reach at all is one no row has (I3).
	if req.ProjectID != nil && *req.ProjectID != "" {
		if !h.requireProjectVisible(w, r, *req.ProjectID, crewPinNotFound) {
			return
		}
		project, err := h.ProjectService.GetProject(*req.ProjectID)
		if err != nil || project == nil {
			crewPinNotFound.write(w)
			return
		}
		if project.OrgID != ActiveOrg(r) {
			writeJSONError(w, http.StatusBadRequest, "project does not belong to this workspace")
			return
		}
		if !h.requireProjectRole(w, r, *req.ProjectID, members.RoleEditor) {
			return
		}
	} else if !h.requireOrgRole(w, r, ActiveOrg(r), orgs.RoleAdmin) {
		return
	}
	team, err := h.TeamService.CreateTeam(ActiveOrg(r), req.Name, req.Description, req.ProjectID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(team)
}

func (h *Handler) GetTeam(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	graph, err := h.TeamService.GetTeam(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "team not found", err)
		return
	}
	if !h.requireOrgRoleFor(w, r, graph.Team.OrgID, orgs.RoleMember, missing("team not found")) {
		return
	}
	json.NewEncoder(w).Encode(graph)
}

// teamWriteChecked loads a crew and enforces the crew-write guard, whose
// caller with no access at all gets absent: the crew's own 404, or that of
// the node or edge the handler looked up first. Returns nil when the
// response has already been written.
func (h *Handler) teamWriteChecked(w http.ResponseWriter, r *http.Request, teamID string, absent notFound) *teams.Team {
	graph, err := h.TeamService.GetTeam(teamID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "team not found", err)
		return nil
	}
	if !h.requireTeamWrite(w, r, graph.Team, absent) {
		return nil
	}
	return graph.Team
}

func (h *Handler) UpdateTeam(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	if h.teamWriteChecked(w, r, mux.Vars(r)["id"], missing("team not found")) == nil {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		EntryNodeID *string `json:"entry_node_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	team, err := h.TeamService.UpdateTeam(mux.Vars(r)["id"], req.Name, req.Description, req.EntryNodeID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(team)
}

func (h *Handler) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	if h.teamWriteChecked(w, r, mux.Vars(r)["id"], missing("team not found")) == nil {
		return
	}
	if err := h.TeamService.DeleteTeam(mux.Vars(r)["id"]); err != nil {
		respondInternal(w, r, "failed to delete crew", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CloneTeam(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	source := h.teamWriteChecked(w, r, mux.Vars(r)["id"], missing("team not found"))
	if source == nil {
		return
	}
	var req struct {
		Name      string  `json:"name"`
		ProjectID *string `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// The copy stays in the source's workspace and is checked where it lands
	// as CreateTeam checks a new crew: a pin must name a project of that
	// workspace, which the caller edits; no pin needs workspace admin rights.
	if req.ProjectID != nil && *req.ProjectID != "" {
		if !h.requireProjectVisible(w, r, *req.ProjectID, crewPinNotFound) {
			return
		}
		project, err := h.ProjectService.GetProject(*req.ProjectID)
		if err != nil || project == nil {
			crewPinNotFound.write(w)
			return
		}
		if project.OrgID != source.OrgID {
			writeJSONError(w, http.StatusBadRequest, "project does not belong to this workspace")
			return
		}
		if !h.requireProjectRole(w, r, *req.ProjectID, members.RoleEditor) {
			return
		}
	} else if !h.requireOrgRole(w, r, source.OrgID, orgs.RoleAdmin) {
		return
	}
	team, err := h.TeamService.CloneTeam(mux.Vars(r)["id"], req.Name, req.ProjectID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(team)
}

// ExportCrew returns a crew as a portable, org-independent JSON document whose
// nodes reference agents by slug. Any member of the crew's workspace may
// export. Human nodes are omitted (their identities aren't portable).
func (h *Handler) ExportCrew(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	graph, err := h.TeamService.GetTeam(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "crew not found", err)
		return
	}
	if !h.requireOrgRoleFor(w, r, graph.Team.OrgID, orgs.RoleMember, missing("crew not found")) {
		return
	}
	portable, err := crewtemplates.Serialize(graph, h.AgentService)
	if err != nil {
		respondInternal(w, r, "failed to export crew", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", crewExportFilename(graph.Team.Name)))
	json.NewEncoder(w).Encode(portable)
}

// ImportCrew creates a crew in the active workspace from a portable crew
// document, resolving each node's agent slug to this workspace's agents.
// Missing slugs are skipped with a warning (returned in the response) rather
// than failing the import. Authz mirrors CreateTeam: a project-pinned import
// (?project_id=) needs project editor rights, a workspace-wide one needs
// workspace admin rights.
func (h *Handler) ImportCrew(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	orgID := ActiveOrg(r)
	projectID := optionalProjectID(r)
	if projectID != nil {
		if !h.requireProjectVisible(w, r, *projectID, crewPinNotFound) {
			return
		}
		project, err := h.ProjectService.GetProject(*projectID)
		if err != nil || project == nil {
			crewPinNotFound.write(w)
			return
		}
		if project.OrgID != orgID {
			writeJSONError(w, http.StatusBadRequest, "project does not belong to this workspace")
			return
		}
		if !h.requireProjectRole(w, r, *projectID, members.RoleEditor) {
			return
		}
	} else if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}

	var doc crewtemplates.PortableCrew
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	result, err := crewtemplates.Import(&doc, orgID, projectID, h.AgentService, h.TeamService)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(result)
}

// ListCrewTemplates returns the built-in crew presets. Each carries its full
// portable document so the client can hand a chosen preset straight to the
// import endpoint.
func (h *Handler) ListCrewTemplates(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	json.NewEncoder(w).Encode(crewtemplates.BuiltinCrewTemplates())
}

// crewPinNotFound answers a crew pin, in a create's body or an import's
// query, naming a project no row has, or one the caller cannot reach at all.
var crewPinNotFound = notFound{http.StatusBadRequest, "project not found"}

// optionalProjectID reads an optional ?project_id= pin from the request.
func optionalProjectID(r *http.Request) *string {
	v := strings.TrimSpace(r.URL.Query().Get("project_id"))
	if v == "" {
		return nil
	}
	return &v
}

// crewExportFilename derives a safe download name from a crew name.
func crewExportFilename(name string) string {
	var b strings.Builder
	for _, ch := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		case ch == ' ' || ch == '-' || ch == '_':
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "crew"
	}
	return slug + ".crew.json"
}

func (h *Handler) AddTeamNode(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	if h.teamWriteChecked(w, r, mux.Vars(r)["id"], missing("team not found")) == nil {
		return
	}
	var req struct {
		NodeType   string                 `json:"node_type"`
		AgentID    string                 `json:"agent_id"`
		UserID     string                 `json:"user_id"`
		Label      string                 `json:"label"`
		Department string                 `json:"department"`
		Position   map[string]interface{} `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	node, err := h.TeamService.AddNode(mux.Vars(r)["id"], teams.NodeSpec{
		NodeType:   req.NodeType,
		AgentID:    req.AgentID,
		UserID:     req.UserID,
		Label:      req.Label,
		Department: req.Department,
		Position:   req.Position,
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(node)
}

func (h *Handler) UpdateTeamNode(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	node, err := h.TeamService.GetNode(mux.Vars(r)["id"])
	if err != nil || node == nil {
		writeJSONError(w, http.StatusNotFound, "team node not found")
		return
	}
	if h.teamWriteChecked(w, r, node.TeamID, missing("team node not found")) == nil {
		return
	}
	var req struct {
		Label      *string                `json:"label"`
		AgentID    *string                `json:"agent_id"`
		UserID     *string                `json:"user_id"`
		Department *string                `json:"department"`
		Position   map[string]interface{} `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.TeamService.UpdateNode(mux.Vars(r)["id"], req.Label, req.AgentID, req.UserID, req.Department, req.Position)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) RemoveTeamNode(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	node, err := h.TeamService.GetNode(mux.Vars(r)["id"])
	if err != nil || node == nil {
		writeJSONError(w, http.StatusNotFound, "team node not found")
		return
	}
	if h.teamWriteChecked(w, r, node.TeamID, missing("team node not found")) == nil {
		return
	}
	if err := h.TeamService.RemoveNode(mux.Vars(r)["id"]); err != nil {
		respondInternal(w, r, "failed to remove crew node", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddTeamEdge(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	if h.teamWriteChecked(w, r, mux.Vars(r)["id"], missing("team not found")) == nil {
		return
	}
	var req struct {
		FromNodeID string                 `json:"from_node_id"`
		ToNodeID   string                 `json:"to_node_id"`
		EdgeType   string                 `json:"edge_type"`
		Config     map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	edge, err := h.TeamService.AddEdge(mux.Vars(r)["id"], req.FromNodeID, req.ToNodeID, req.EdgeType, req.Config)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(edge)
}

func (h *Handler) UpdateTeamEdge(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	edge, err := h.TeamService.GetEdge(mux.Vars(r)["id"])
	if err != nil || edge == nil {
		writeJSONError(w, http.StatusNotFound, "team edge not found")
		return
	}
	if h.teamWriteChecked(w, r, edge.TeamID, missing("team edge not found")) == nil {
		return
	}
	var req struct {
		Config map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.TeamService.UpdateEdge(mux.Vars(r)["id"], req.Config)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	json.NewEncoder(w).Encode(updated)
}

func (h *Handler) RemoveTeamEdge(w http.ResponseWriter, r *http.Request) {
	if !requireUser(w, r) {
		return
	}
	edge, err := h.TeamService.GetEdge(mux.Vars(r)["id"])
	if err != nil || edge == nil {
		writeJSONError(w, http.StatusNotFound, "team edge not found")
		return
	}
	if h.teamWriteChecked(w, r, edge.TeamID, missing("team edge not found")) == nil {
		return
	}
	if err := h.TeamService.RemoveEdge(mux.Vars(r)["id"]); err != nil {
		respondInternal(w, r, "failed to remove crew edge", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// LaunchTeamRun starts a run at the team's entry node.
func (h *Handler) LaunchTeamRun(w http.ResponseWriter, r *http.Request) {
	if !h.requireNoProposalRunLaunch(w, r) {
		return
	}
	graph, err := h.TeamService.GetTeam(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, r, http.StatusNotFound, "team not found", err)
		return
	}
	// The checks below tell of the crew, so a caller who may not know of it
	// hears first what a crew no row has answers (I3).
	if !h.requireTeamVisible(w, r, graph.Team, missing("team not found")) {
		return
	}
	if graph.Team.EntryNodeID == nil {
		writeJSONError(w, http.StatusBadRequest, "team has no entry node")
		return
	}
	var entryAgentID string
	for _, node := range graph.Nodes {
		if node.ID == *graph.Team.EntryNodeID {
			entryAgentID = node.AgentID
		}
	}
	if entryAgentID == "" {
		writeJSONError(w, http.StatusBadRequest, "entry node not found in team")
		return
	}
	var req struct {
		ProjectID string `json:"project_id"`
		Prompt    string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Project-pinned crews launch with project editor rights on the pin;
	// otherwise the request's project scope needs editor rights; a launch
	// with no project scope at all needs workspace admin rights.
	pin := h.teamPin(graph.Team)
	if pin != "" {
		if !h.requireProjectRole(w, r, pin, members.RoleEditor) {
			return
		}
	} else if req.ProjectID == "" {
		launchOrg := graph.Team.OrgID
		if launchOrg == "" {
			launchOrg = ActiveOrg(r)
		}
		if !h.requireOrgRole(w, r, launchOrg, orgs.RoleAdmin) {
			return
		}
	}
	if req.ProjectID != "" && !h.requireProjectRole(w, r, req.ProjectID, members.RoleEditor) {
		return
	}
	// A launch that names no project runs in the pinned crew's own, so the
	// run is scoped to it and gets the board's tracking card there.
	if req.ProjectID == "" {
		req.ProjectID = pin
	}
	// A crew's run stays in the crew's workspace, whoever edits the project.
	if req.ProjectID != "" && graph.Team.OrgID != "" {
		if project, err := h.ProjectService.GetProject(req.ProjectID); err != nil || project == nil || project.OrgID != graph.Team.OrgID {
			writeJSONError(w, http.StatusBadRequest, "project does not belong to this workspace")
			return
		}
	}
	// Crew runs belong to the crew's org, falling back to the project's org
	// then the caller's active workspace.
	orgID := graph.Team.OrgID
	if orgID == "" && req.ProjectID != "" {
		if project, err := h.ProjectService.GetProject(req.ProjectID); err == nil && project != nil {
			orgID = project.OrgID
		}
	}
	if orgID == "" {
		orgID = ActiveOrg(r)
	}
	teamID := graph.Team.ID
	launch := agentruns.LaunchRequest{
		OrgID:      orgID,
		AgentID:    entryAgentID,
		TeamID:     &teamID,
		TeamNodeID: graph.Team.EntryNodeID,
		Prompt:     req.Prompt,
		LaunchedBy: CurrentUserID(r),
	}
	if req.ProjectID != "" {
		launch.ProjectID = &req.ProjectID
	}
	run, err := h.launchRun(r, launch)
	if err != nil {
		writeLaunchError(w, r, launchErrs400, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(run)
}
