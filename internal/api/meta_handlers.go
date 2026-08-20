package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
)

func (h *Handler) registerMetaRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/meta/artifact-types", h.MetaArtifactTypes).Methods("GET")
	router.HandleFunc("/api/v1/meta/link-types", h.MetaLinkTypes).Methods("GET")
}

// MetaArtifactTypes serves the artifact type catalog.
func (h *Handler) MetaArtifactTypes(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(artifacts.TypeCatalog())
}

// linkTypeRuleDTO mirrors the frontend's LinkTypeRule shape.
type linkTypeRuleDTO struct {
	Type             string   `json:"type"`
	Label            string   `json:"label"`
	InverseLabel     string   `json:"inverseLabel"`
	AllowedFromTypes []string `json:"allowedFromTypes"`
	AllowedToTypes   []string `json:"allowedToTypes"`
	Description      string   `json:"description"`
}

// MetaLinkTypes serves the link type rules.
func (h *Handler) MetaLinkTypes(w http.ResponseWriter, r *http.Request) {
	rules := links.GetLinkTypeRules()
	out := make([]linkTypeRuleDTO, 0, len(rules))
	for _, rule := range rules {
		out = append(out, linkTypeRuleDTO{
			Type:             rule.Type,
			Label:            rule.Label,
			InverseLabel:     rule.InverseLabel,
			AllowedFromTypes: rule.AllowedFromTypes,
			AllowedToTypes:   rule.AllowedToTypes,
			Description:      rule.Description,
		})
	}
	json.NewEncoder(w).Encode(out)
}
