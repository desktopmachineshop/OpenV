package api

import (
	"net/http"

	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/release"
)

// Agent providers that ship behind a release feature (REQ-137). A
// stable-channel workspace is not offered such a provider, and cannot choose
// one, until the stable release it has turned on carries the feature: the
// provider settings list leaves it out, and a provider setting or an agent
// definition that names it is refused with featureGateMessage. An agent
// already on the provider keeps it: an edit that leaves the provider as it is
// goes through, and runs are never gated, so a colleague's agent made while
// previewing the next stable release keeps working.
var gatedProviders = map[string]string{
	providers.ProviderAntigravityCLI: release.FeatureAntigravity,
}

// providerOffered reports whether the caller may choose provider in workspace
// orgID: a provider no feature gates always, a gated one once its feature is
// on for the caller there.
func (h *Handler) providerOffered(r *http.Request, orgID, provider string) bool {
	key, gated := gatedProviders[provider]
	return !gated || h.featureEnabled(r, orgID, key)
}

// agentProviderAllowed reports whether an agent definition saved under slug
// in the active workspace may name provider: one the workspace is offered,
// or the provider the agent already has.
func (h *Handler) agentProviderAllowed(r *http.Request, slug, provider string) bool {
	orgID := ActiveOrg(r)
	if h.providerOffered(r, orgID, provider) {
		return true
	}
	existing, err := h.AgentService.GetBySlug(orgID, slug)
	return err == nil && existing != nil && existing.Provider == provider
}
