package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/quality"
	"github.com/openv/requirements-platform/internal/domain/settings"
)

// Quality rule sets: which normative vocabulary a project's requirements are
// written in ("shall" or RFC 2119 must/should/may) and how loudly each lint
// rule speaks. A workspace sets the house style; a project overrides only what
// it must. The rules are advisory — they change what the linter reports, never
// whether a write is allowed.

// qualityRulesCatalog is the vocabulary the UI needs to render the editor
// without hard-coding it: the conventions and rules the server understands.
type qualityRulesCatalog struct {
	Conventions []string          `json:"conventions"`
	Rules       []string          `json:"rules"`
	Severities  []string          `json:"severities"`
	Defaults    quality.RuleSet   `json:"defaults"`
	Labels      map[string]string `json:"labels"`
}

func qualityCatalog() qualityRulesCatalog {
	return qualityRulesCatalog{
		Conventions: quality.Conventions,
		Rules:       quality.Rules,
		Severities:  []string{quality.SeverityError, quality.SeverityWarning, quality.SeverityInfo, quality.SeverityOff},
		Defaults:    quality.DefaultRuleSet(),
		Labels:      quality.Labels(),
	}
}

// qualityRulesResponse is what both levels return: the resolved rules, the
// overrides that produced them, and the catalog the editor renders from.
type qualityRulesResponse struct {
	*settings.QualityRules
	Catalog qualityRulesCatalog `json:"catalog"`
}

func writeQualityRules(w http.ResponseWriter, rules *settings.QualityRules) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(qualityRulesResponse{QualityRules: rules, Catalog: qualityCatalog()})
}

// decodeRuleSet reads a rule set from the request body. An empty body or an
// empty object clears the level's override, which is how a project goes back
// to inheriting its workspace's house style.
func decodeRuleSet(w http.ResponseWriter, r *http.Request) (quality.RuleSet, bool) {
	var rs quality.RuleSet
	if err := json.NewDecoder(r.Body).Decode(&rs); err != nil && err != io.EOF {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return quality.RuleSet{}, false
	}
	return rs, true
}

// respondRulesError maps a settings failure to its status: a rule set naming
// an unknown convention, rule or severity is the caller's mistake.
func (h *Handler) respondRulesError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, settings.ErrInvalidRules) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondInternal(w, r, "failed to load quality rules", err)
}

// GetProjectQualityRules returns the rules a project's requirements are linted
// against, plus both levels' overrides. Viewer role, like the lint report.
func (h *Handler) GetProjectQualityRules(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleViewer) {
		return
	}
	rules, err := h.settingsService.ProjectQualityRules(h.orgIDForProject(projectID), projectID)
	if err != nil {
		h.respondRulesError(w, r, err)
		return
	}
	writeQualityRules(w, rules)
}

// UpdateProjectQualityRules stores a project's override. Editor role: the
// people who write the requirements choose how they are judged.
func (h *Handler) UpdateProjectQualityRules(w http.ResponseWriter, r *http.Request) {
	projectID := mux.Vars(r)["id"]
	if !h.requireProjectRole(w, r, projectID, members.RoleEditor) {
		return
	}
	rs, ok := decodeRuleSet(w, r)
	if !ok {
		return
	}
	rules, err := h.settingsService.SetProjectQualityRules(h.orgIDForProject(projectID), projectID, rs)
	if err != nil {
		h.respondRulesError(w, r, err)
		return
	}
	writeQualityRules(w, rules)
}

// GetWorkspaceQualityRules returns a workspace's house style. Any member may
// read it; only admins change it.
func (h *Handler) GetWorkspaceQualityRules(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	rules, err := h.settingsService.WorkspaceQualityRules(orgID)
	if err != nil {
		h.respondRulesError(w, r, err)
		return
	}
	writeQualityRules(w, rules)
}

// UpdateWorkspaceQualityRules stores a workspace's house style, which every
// project inherits until it overrides it.
func (h *Handler) UpdateWorkspaceQualityRules(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleAdmin) {
		return
	}
	rs, ok := decodeRuleSet(w, r)
	if !ok {
		return
	}
	rules, err := h.settingsService.SetWorkspaceQualityRules(orgID, rs)
	if err != nil {
		h.respondRulesError(w, r, err)
		return
	}
	writeQualityRules(w, rules)
}

// orgIDForProject resolves a project's workspace, returning "" when it cannot
// be loaded — the settings service then resolves the project level alone
// rather than failing a read that is only advisory.
func (h *Handler) orgIDForProject(projectID string) string {
	project, err := h.projectService.GetProject(projectID)
	if err != nil || project == nil {
		return ""
	}
	return project.OrgID
}

// qualityRuleSetFor resolves the rule set the lint endpoints judge a project
// against.
func (h *Handler) qualityRuleSetFor(projectID string) quality.RuleSet {
	if h.settingsService == nil {
		return quality.DefaultRuleSet()
	}
	return h.settingsService.EffectiveRuleSet(h.orgIDForProject(projectID), projectID)
}
