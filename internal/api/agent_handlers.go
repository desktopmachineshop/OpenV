package api

import "github.com/gorilla/mux"

// registerAgentRoutes registers the agent surface: one sub-registrar per
// area, each in its area file. gorilla/mux serves the first registered
// route that matches, so the order of the calls is the contract, and
// testdata/route_handlers.txt pins it. That is why the run registrars
// interleave with the worker protocol's, and why the worker's dispatch
// routes, delegation among them, come before a run's {id}/... routes.
func (h *Handler) registerAgentRoutes(router *mux.Router) {
	h.registerAgentDefinitionRoutes(router)
	h.registerAgentRunLaunchRoutes(router)
	h.registerWorkerDispatchRoutes(router)
	h.registerAgentRunReadRoutes(router)
	h.registerWorkerLogRoutes(router)
	h.registerAgentRunControlRoutes(router)
	h.registerWorkerLifecycleRoutes(router)
	h.registerAutomationRoutes(router)
	h.registerProposalRoutes(router)
	h.registerRepoConnectionRoutes(router)
	h.registerProviderSettingsRoutes(router)
	h.registerProviderLoginRoutes(router)
	h.registerCrewRoutes(router)
	h.registerDomainEventRoutes(router)
}
