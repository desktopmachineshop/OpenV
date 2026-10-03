package api

import (
	"github.com/gorilla/mux"
)

func (h *Handler) registerAgentRoutes(router *mux.Router) {
	// Agent definitions (file-backed).
	router.HandleFunc("/api/v1/agents", h.ListAgents).Methods("GET")
	router.HandleFunc("/api/v1/agents", h.CreateAgent).Methods("POST")
	router.HandleFunc("/api/v1/agents/sync", h.SyncAgents).Methods("POST")
	router.HandleFunc("/api/v1/agents/{slug}", h.GetAgent).Methods("GET")
	router.HandleFunc("/api/v1/agents/{slug}", h.UpdateAgent).Methods("PUT")
	router.HandleFunc("/api/v1/agents/{slug}", h.DeleteAgent).Methods("DELETE")
	router.HandleFunc("/api/v1/agents/{slug}/raw", h.GetAgentRaw).Methods("GET")
	router.HandleFunc("/api/v1/agents/{slug}/raw", h.SaveAgentRaw).Methods("PUT")
	router.HandleFunc("/api/v1/agents/{slug}/runs", h.LaunchAgentRun).Methods("POST")
	router.HandleFunc("/api/v1/projects/{id}/draft-test-cases", h.DraftTestCases).Methods("POST")

	// Runs.
	router.HandleFunc("/api/v1/agent-runs", h.ListAgentRuns).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/claim", h.ClaimAgentRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/delegate", h.DelegateRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/delegate/{id}", h.DelegateStatus).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/{id}", h.GetAgentRun).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/{id}/tree", h.GetAgentRunTree).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/{id}/logs", h.GetAgentRunLogs).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/{id}/logs", h.AppendAgentRunLogs).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/{id}/stream", h.StreamAgentRun).Methods("GET")
	router.HandleFunc("/api/v1/agent-runs/{id}/cancel", h.alwaysWritable(h.CancelAgentRun)).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/{id}/retry", h.RetryAgentRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/{id}/start", h.StartAgentRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/{id}/release", h.ReleaseAgentRun).Methods("POST")
	router.HandleFunc("/api/v1/agent-runs/{id}/finish", h.FinishAgentRun).Methods("POST")

	// Automations.
	router.HandleFunc("/api/v1/automations", h.ListAutomations).Methods("GET")
	router.HandleFunc("/api/v1/automations", h.CreateAutomation).Methods("POST")
	router.HandleFunc("/api/v1/automations/{id}", h.GetAutomation).Methods("GET")
	router.HandleFunc("/api/v1/automations/{id}", h.UpdateAutomation).Methods("PUT")
	router.HandleFunc("/api/v1/automations/{id}", h.DeleteAutomation).Methods("DELETE")
	router.HandleFunc("/api/v1/automations/{id}/run-now", h.RunAutomationNow).Methods("POST")

	// Proposals.
	router.HandleFunc("/api/v1/proposals", h.ListProposals).Methods("GET")
	router.HandleFunc("/api/v1/proposals/bulk", h.BulkReviewProposals).Methods("POST")
	router.HandleFunc("/api/v1/proposals/{id}/approve", h.ApproveProposal).Methods("POST")
	router.HandleFunc("/api/v1/proposals/{id}/reject", h.RejectProposal).Methods("POST")

	// Repo connections.
	router.HandleFunc("/api/v1/projects/{id}/repo-connections", h.ListRepoConnections).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/repo-connections", h.CreateRepoConnection).Methods("POST")
	router.HandleFunc("/api/v1/repo-connections/{id}", h.UpdateRepoConnection).Methods("PUT")
	router.HandleFunc("/api/v1/repo-connections/{id}", h.DeleteRepoConnection).Methods("DELETE")
	router.HandleFunc("/api/v1/repo-connections/{id}/my-path", h.SetMyRepoPath).Methods("PUT")

	// Provider settings.
	router.HandleFunc("/api/v1/provider-settings", h.ListProviderSettings).Methods("GET")
	router.HandleFunc("/api/v1/provider-settings", h.UpsertProviderSetting).Methods("PUT")
	router.HandleFunc("/api/v1/provider-settings/detect", h.RecordProviderDetection).Methods("POST")

	// Provider CLI login broker.
	router.HandleFunc("/api/v1/provider-logins", h.StartProviderLogin).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/claim", h.ClaimProviderLogin).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/{id}", h.GetProviderLogin).Methods("GET")
	router.HandleFunc("/api/v1/provider-logins/{id}/code", h.SubmitProviderLoginCode).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/{id}/cancel", h.CancelProviderLogin).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/{id}/progress", h.ProgressProviderLogin).Methods("POST")
	router.HandleFunc("/api/v1/provider-logins/{id}/full", h.GetProviderLoginFull).Methods("GET")

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

	// Domain event audit.
	router.HandleFunc("/api/v1/events", h.ListDomainEvents).Methods("GET")
}

// --- Agent definitions ---

// --- Runs ---

// --- Worker endpoints ---

// --- Automations ---

// --- Proposals ---

// --- Repo connections ---

// --- Provider settings ---

// --- Provider CLI login broker ---

// --- Teams ---
