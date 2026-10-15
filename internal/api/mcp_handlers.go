package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"github.com/openv/requirements-platform/internal/domain/connectedapps"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/domain/users"
	"github.com/openv/requirements-platform/internal/mcp"
)

// The remote MCP endpoint (REQ-44): the tools openv-mcp gives an agent,
// served over MCP's Streamable HTTP transport to an AI client a member has
// connected with OAuth (oauth_handlers.go). It is stateless: each POST
// carries one JSON-RPC message or a batch and is answered in JSON, with no
// session and no server-sent stream.
//
// A tool calls the REST API as it does from openv-mcp, but its request
// never leaves the process: it goes through the server's own router as the
// member, in the workspace the grant names, so every guard, plan gate and
// audit record applies to it unchanged, and the project and workspace
// guards refuse any other workspace (authz.go). A read-only grant is
// offered only the tools that read, and its requests are refused anything
// but a GET besides.

// registerMCPRoutes wires the endpoint. Its tool calls go through the
// router cmd/server hands SetMCPRouter once every route is registered.
func (h *Handler) registerMCPRoutes(router *mux.Router) {
	router.HandleFunc("/mcp", h.MCP).Methods("POST")
	// No server-initiated stream and no session to end.
	router.HandleFunc("/mcp", h.MCPNoStream).Methods("GET", "DELETE")
}

// SetMCPRouter wires the router the endpoint's tool calls go through
// (wiring-time only). Without it the endpoint answers 503.
func (h *Handler) SetMCPRouter(router http.Handler) { h.mcpRouter = router }

// MCPNoStream answers the methods the endpoint does not offer.
func (h *Handler) MCPNoStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", "POST")
	writeJSONError(w, http.StatusMethodNotAllowed, "this endpoint takes POST only: it opens no stream and keeps no session")
}

// MCP answers one POST of JSON-RPC.
func (h *Handler) MCP(w http.ResponseWriter, r *http.Request) {
	if h.connectedAppsOff(w) {
		return
	}
	if h.mcpRouter == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "the MCP endpoint is not wired")
		return
	}
	challenge := `Bearer realm="OpenV", resource_metadata="` + h.oauthIssuer() + `/.well-known/oauth-protected-resource"`
	token := ""
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		token = strings.TrimPrefix(auth, "Bearer ")
	}
	if token == "" {
		w.Header().Set("WWW-Authenticate", challenge)
		writeJSONError(w, http.StatusUnauthorized, "authentication required: connect this app to OpenV")
		return
	}
	grant, err := h.ConnectedApps.Authenticate(token)
	if err != nil {
		w.Header().Set("WWW-Authenticate", challenge+`, error="invalid_token"`)
		writeJSONError(w, http.StatusUnauthorized, "the access token is not valid: it expired or was revoked")
		return
	}
	member, err := h.UserService.GetByID(grant.UserID)
	if err != nil || member == nil {
		w.Header().Set("WWW-Authenticate", challenge+`, error="invalid_token"`)
		writeJSONError(w, http.StatusUnauthorized, "the access token is not valid: its member no longer exists")
		return
	}
	// The tool calls run with the grant's workspace active, which the
	// session middleware would have checked the member still belongs to;
	// a member who has left it since the consent reaches nothing there.
	if ok, err := h.OrgService.IsMember(grant.OrgID, grant.UserID); err != nil || !ok {
		writeJSONError(w, http.StatusForbidden, "you are no longer a member of the workspace this app was connected to")
		return
	}
	if !h.memberFeatureEnabled(grant.OrgID, &grant.UserID, release.FeatureRemoteMCP) {
		writeJSONError(w, http.StatusForbidden, "connected apps are not yet available on this workspace's release channel")
		return
	}
	if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !supportedMCPVersion(v) {
		writeJSONError(w, http.StatusBadRequest, "unsupported MCP-Protocol-Version "+v)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "could not read the request body")
		return
	}

	// The member as the tool calls see them: never a platform admin, so a
	// connected app holds no power beyond the member's own in the workspace.
	acting := *member
	acting.IsAdmin = false
	client := mcp.NewClientWithTransport("http://openv.internal", "", &memberTransport{
		h: h, outer: r, user: &acting, grant: grant,
	})
	version := ""
	if cur := h.ReleaseService; cur != nil {
		if rel := cur.Current(); rel != nil {
			version = rel.Version
		}
	}
	out := mcp.HandleHTTP(client, mcp.MemberTools(mcp.Tools(), !grant.CanWrite()), body, "openv", version)
	if out == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

func supportedMCPVersion(v string) bool {
	for _, s := range mcp.ProtocolVersions {
		if s == v {
			return true
		}
	}
	return false
}

// memberTransport sends a tool's API request through the router as the
// grant's member, in the grant's workspace.
type memberTransport struct {
	h     *Handler
	outer *http.Request
	user  *users.User
	grant *connectedapps.Grant
}

func (t *memberTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := &bufferedResponse{header: http.Header{}, status: http.StatusOK}
	if !t.grant.CanWrite() && req.Method != http.MethodGet && req.Method != http.MethodHead {
		rec.header.Set("Content-Type", "application/json")
		rec.status = http.StatusForbidden
		rec.body.WriteString(`{"error":"this app was connected to read only"}`)
		return rec.response(req), nil
	}
	ctx := context.WithValue(t.outer.Context(), ctxUser, t.user)
	ctx = context.WithValue(ctx, ctxActiveOrg, t.grant.OrgID)
	ctx = context.WithValue(ctx, ctxConnectedApp, t.grant)
	inner := req.Clone(ctx)
	inner.RequestURI = req.URL.RequestURI()
	inner.RemoteAddr = t.outer.RemoteAddr
	inner.Header.Del("Authorization")
	t.h.mcpRouter.ServeHTTP(rec, inner)
	return rec.response(req), nil
}

// bufferedResponse is the ResponseWriter a tool's in-process request is
// answered into.
type bufferedResponse struct {
	header      http.Header
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) WriteHeader(status int) {
	if !b.wroteHeader {
		b.status, b.wroteHeader = status, true
	}
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	b.wroteHeader = true
	return b.body.Write(p)
}

func (b *bufferedResponse) response(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: b.status,
		Header:     b.header,
		Body:       io.NopCloser(bytes.NewReader(b.body.Bytes())),
		Request:    req,
	}
}
