package api

import (
	"github.com/gorilla/mux"
)

func (h *Handler) registerOrgRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs", h.ListOrgs).Methods("GET")
	router.HandleFunc("/api/v1/orgs", h.CreateOrg).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}", h.GetOrg).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}", h.UpdateOrg).Methods("PUT")
	router.HandleFunc("/api/v1/orgs/{id}", h.alwaysWritable(h.DeleteOrg)).Methods("DELETE")
	router.HandleFunc("/api/v1/orgs/{id}/plan", h.SetOrgPlan).Methods("PUT")
	router.HandleFunc("/api/v1/orgs/{id}/restore", h.RestoreOrg).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/activate", h.alwaysWritable(h.ActivateOrg)).Methods("POST")

	// Workspace logo: any member may fetch it (it is shown in the app and on
	// download cover pages); admins upload and remove it.
	router.HandleFunc("/api/v1/orgs/{id}/logo", h.GetOrgLogo).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/logo", h.UploadOrgLogo).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/logo", h.DeleteOrgLogo).Methods("DELETE")

	router.HandleFunc("/api/v1/orgs/{id}/limits", h.GetOrgLimits).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/members", h.ListOrgMembers).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/members", h.AddOrgMember).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/members/{userId}", h.UpdateOrgMember).Methods("PUT")
	router.HandleFunc("/api/v1/orgs/{id}/members/{userId}", h.alwaysWritable(h.RemoveOrgMember)).Methods("DELETE")

	router.HandleFunc("/api/v1/orgs/{id}/teams", h.ListOrgTeams).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/teams", h.CreateOrgTeam).Methods("POST")
	router.HandleFunc("/api/v1/org-teams/{id}", h.UpdateOrgTeam).Methods("PUT")
	router.HandleFunc("/api/v1/org-teams/{id}", h.DeleteOrgTeam).Methods("DELETE")
	router.HandleFunc("/api/v1/org-teams/{id}/members/{userId}", h.AddOrgTeamMember).Methods("POST")
	router.HandleFunc("/api/v1/org-teams/{id}/members/{userId}", h.RemoveOrgTeamMember).Methods("DELETE")

	router.HandleFunc("/api/v1/orgs/{id}/worker-keys", h.ListWorkerKeys).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/worker-keys", h.CreateWorkerKey).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/worker-keys/{keyId}", h.alwaysWritable(h.RevokeWorkerKey)).Methods("DELETE")

	// Personal runner keys: every member manages their own.
	router.HandleFunc("/api/v1/orgs/{id}/my-runner-key", h.GetMyRunnerKey).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/my-runner-key", h.CreateMyRunnerKey).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/my-runner-key", h.alwaysWritable(h.RevokeMyRunnerKey)).Methods("DELETE")

	// Requirement quality house style: readable by any member, set by admins,
	// inherited by every project in the workspace.
	router.HandleFunc("/api/v1/orgs/{id}/quality-rules", h.GetWorkspaceQualityRules).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/quality-rules", h.UpdateWorkspaceQualityRules).Methods("PUT")

	// Agent Connector pairing: browser issues a one-time code; the local
	// connector exchanges it (public route — the code is the credential).
	router.HandleFunc("/api/v1/orgs/{id}/connector-pairing", h.CreateConnectorPairing).Methods("POST")
	router.HandleFunc("/api/v1/public/connector/pair", h.ExchangeConnectorPairing).Methods("POST")
	router.HandleFunc("/api/v1/public/connector/download", h.DownloadConnector).Methods("GET", "HEAD")

	// Hosted runner: one platform-managed container per workspace (admin).
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner", h.GetHostedRunner).Methods("GET")
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner", h.CreateHostedRunner).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner/start", h.StartHostedRunner).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner/stop", h.StopHostedRunner).Methods("POST")
	router.HandleFunc("/api/v1/orgs/{id}/hosted-runner", h.DeleteHostedRunner).Methods("DELETE")

	// Worker status: runner fleet + queue depth for the workspace (member).
	router.HandleFunc("/api/v1/orgs/{id}/worker-status", h.GetWorkerStatus).Methods("GET")

	// Usage rollup: run counts, tokens and cost by agent and by day (member).
	router.HandleFunc("/api/v1/orgs/{id}/usage", h.GetOrgUsage).Methods("GET")

	router.HandleFunc("/api/v1/projects/{id}/team-access", h.ListProjectTeamAccess).Methods("GET")
	router.HandleFunc("/api/v1/projects/{id}/team-access", h.GrantProjectTeamAccess).Methods("PUT")
	router.HandleFunc("/api/v1/projects/{id}/team-access/{teamId}", h.RevokeProjectTeamAccess).Methods("DELETE")
}

// --- Workspaces ---

// --- Workspace logo ---

// --- Workspace members ---

// --- People-teams ---

// --- Worker keys ---

// --- Personal runner keys ---

// --- Hosted runner ---

// --- Worker status ---

// --- Agent Connector pairing ---

// --- Project team grants ---
