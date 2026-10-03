package api

import "github.com/gorilla/mux"

// registerOrgRoutes registers the workspace surface: one sub-registrar per
// area, each in its area file. gorilla/mux serves the first registered
// route that matches, so the order of the calls is the contract, and
// testdata/route_handlers.txt pins it. That is why a workspace's quality
// rule set, whose handlers are in quality_rules_handlers.go, is registered
// between the personal runner keys and the Agent Connector, and the
// connector before the hosted runner, where the workspace routes always
// had them.
func (h *Handler) registerOrgRoutes(router *mux.Router) {
	h.registerOrgCoreRoutes(router)
	h.registerOrgLogoRoutes(router)
	h.registerOrgMemberRoutes(router)
	h.registerOrgTeamRoutes(router)
	h.registerWorkerKeyRoutes(router)
	h.registerRunnerKeyRoutes(router)
	h.registerWorkspaceQualityRuleRoutes(router)
	h.registerConnectorRoutes(router)
	h.registerHostedRunnerRoutes(router)
	h.registerWorkerStatusRoutes(router)
	h.registerProjectTeamAccessRoutes(router)
}
