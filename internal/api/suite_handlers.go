package api

import "github.com/gorilla/mux"

// registerSuiteRoutes registers the requirements suite's surface: one
// sub-registrar per area, each in its area file. gorilla/mux serves the
// first registered route that matches, so the order of the calls is the
// contract, and testdata/route_handlers.txt pins it. That is why a
// project's quality rule set, whose handlers are in
// quality_rules_handlers.go, is registered between the quality linting and
// the reference parties, where the suite's routes always had it.
func (h *Handler) registerSuiteRoutes(router *mux.Router) {
	h.registerProductProfileRoutes(router)
	h.registerVVRoutes(router)
	h.registerQualityLintRoutes(router)
	h.registerProjectQualityRuleRoutes(router)
	h.registerReferencePartyRoutes(router)
	h.registerWorkItemRoutes(router)
	h.registerGuidedSessionRoutes(router)
	h.registerGuidedCopilotRoutes(router)
	h.registerInterviewRoutes(router)
	h.registerPublicInterviewRoutes(router)
}
