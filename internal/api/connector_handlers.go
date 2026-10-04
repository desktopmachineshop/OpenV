package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// registerConnectorRoutes wires Agent Connector pairing and download: the
// browser issues a one-time code, and the local connector exchanges it on a
// public route, where the code is the credential.
func (h *Handler) registerConnectorRoutes(router *mux.Router) {
	router.HandleFunc("/api/v1/orgs/{id}/connector-pairing", h.CreateConnectorPairing).Methods("POST")
	router.HandleFunc("/api/v1/public/connector/pair", h.ExchangeConnectorPairing).Methods("POST")
	router.HandleFunc("/api/v1/public/connector/download", h.DownloadConnector).Methods("GET", "HEAD")
}

// CreateConnectorPairing issues a one-time pairing code plus the deep link
// the browser uses to hand it to the local connector.
func (h *Handler) CreateConnectorPairing(w http.ResponseWriter, r *http.Request) {
	orgID := mux.Vars(r)["id"]
	if !h.requireOrgRole(w, r, orgID, orgs.RoleMember) {
		return
	}
	user := CurrentUser(r)
	code, expires, err := h.WorkerKeyService.CreatePairing(orgID, user.ID)
	if err != nil {
		respondInternal(w, r, "failed to create pairing code", err)
		return
	}
	apiURL := h.PublicAPIURL
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code":       code,
		"expires_at": expires,
		"api_url":    apiURL,
		"deep_link":  "openv-connector://pair?code=" + code + "&api=" + url.QueryEscape(apiURL),
		"start_link": "openv-connector://start",
		// One-link flow: the connector starts with its existing pairing for
		// this workspace and only spends the code when it has none, so
		// opening never rotates a working key.
		"open_link": "openv-connector://open?org=" + orgID + "&code=" + code + "&api=" + url.QueryEscape(apiURL),
	})
}

// ExchangeConnectorPairing lets the local connector trade a code for the
// member's personal runner key (rotates any previous one).
func (h *Handler) ExchangeConnectorPairing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeJSONError(w, http.StatusBadRequest, "pairing code is required")
		return
	}
	key, plaintext, err := h.WorkerKeyService.ExchangePairing(req.Code, "")
	if err != nil {
		writeJSONError(w, http.StatusForbidden, err.Error())
		return
	}
	orgName := ""
	if org, err := h.OrgService.Get(key.OrgID); err == nil {
		orgName = org.Name
	}
	userName := ""
	if key.UserID != nil {
		if u, err := h.UserService.GetByID(*key.UserID); err == nil && u != nil {
			userName = u.Name
		}
	}
	apiURL := h.PublicAPIURL
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"worker_key": plaintext,
		"api_url":    apiURL,
		"org_id":     key.OrgID,
		"org_name":   orgName,
		"user_name":  userName,
	})
}

// DownloadConnector serves a prebuilt Agent Connector when present:
// preferably the single self-contained executable (agentd and openv-mcp
// embedded — see cmd/openv-connector payload_embed.go), falling back to the
// legacy zip bundle for dist directories built before the single-file era.
func (h *Handler) DownloadConnector(w http.ResponseWriter, r *http.Request) {
	osName := r.URL.Query().Get("os")
	if osName == "" {
		osName = "windows"
	}
	// Each allowed os names its files by constant, so no part of the query
	// reaches a path.
	var single, serveAs, zipName string
	switch osName {
	case "windows":
		single, serveAs, zipName = "openv-connector-windows.exe", "openv-connector.exe", "openv-connector-windows.zip"
	case "linux":
		single, serveAs, zipName = "openv-connector-linux", "openv-connector", "openv-connector-linux.zip"
	case "darwin":
		single, serveAs, zipName = "openv-connector-darwin", "openv-connector", "openv-connector-darwin.zip"
	default:
		writeJSONError(w, http.StatusBadRequest, "unknown os")
		return
	}
	if h.ConnectorDistDir == "" {
		writeJSONError(w, http.StatusNotFound, "connector downloads are not configured on this deployment")
		return
	}

	if path := filepath.Join(h.ConnectorDistDir, single); fileExists(path) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename="+serveAs)
		http.ServeFile(w, r, path)
		return
	}

	if path := filepath.Join(h.ConnectorDistDir, zipName); fileExists(path) {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename="+zipName)
		http.ServeFile(w, r, path)
		return
	}
	writeJSONError(w, http.StatusNotFound, "connector download not available for "+osName+" — build it with `make connector-dist`")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
