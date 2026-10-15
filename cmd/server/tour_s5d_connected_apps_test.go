//go:build unix

package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/connectedapps"
)

// TestTourS5dConnectedApps is the S5d tour's connected-apps area: the remote
// MCP endpoint and its OAuth (REQ-44). Its golden is
// testdata/tour/s5d/connected_apps.json.
//
// The area walks what an AI client and a member do, in order: the client
// finds the authorization server from the endpoint's 401 and the two
// metadata documents; registers itself (and is refused a redirect URI it may
// not use); sends the member's browser to /oauth/authorize, which refuses a
// redirect URI the client did not register outright, sends a request
// without PKCE back to the client with an error, and sends a good one on to
// the app's consent page; the member reads the consent (the app's name and
// where it returns to), declines once (access_denied goes back with the
// state), then approves read only for their own workspace; the client's
// first code is spent by a wrong verifier, and the second buys a token
// pair. With the read-only token the client initializes (the revision it
// asked for), acknowledges with a notification (202), lists the tools (the
// read-only ones), lists the member's projects, is refused a project of a
// workspace the grant does not name as if it did not exist, and finds no
// tool that writes. It refreshes (the old refresh token is spent), the
// member approves read and write (the same grant takes the new scope), and
// the new token creates an artifact, which the workspace's events record as
// the member's. The member lists their connected apps, the token is no key
// to the rest of the API; a colleague connects the app to the owner's shared
// workspace, and once removed from that workspace their app is refused
// there (403) though its grant stands; a request in an MCP revision the endpoint does
// not speak is refused (400), and the member revokes the app, after which its
// token is refused; the revocation endpoint answers the same for a token
// that is gone. Last, the endpoint's other methods (405).
//
// Nondeterminism: the client id, the codes and the tokens, registered from
// the answers; client_id_issued_at, a Unix time, kept as <issued-at>.
func TestTourS5dConnectedApps(t *testing.T) {
	runTourArea(t, tourArea{
		slice: "s5d",
		key:   "connected_apps",
		about: "Connected apps (REQ-44): the remote MCP endpoint's OAuth discovery, client registration, the " +
			"consent, codes with PKCE, token refresh and revocation, and tools/call as the member, held to the " +
			"granted workspace and scope.",
		run: connectedAppsTour,
		accounts: []tourAccount{{name: "colleague", display: "Tour Colleague", about: "a member of the owner's " +
			"shared workspace, who connects the app there and is then removed from the workspace"}},
	})
}

// The PKCE verifier the tour's client holds, and its S256 challenge.
const connectedAppsVerifier = "tour-verifier-0123456789-abcdefghijklmnopqrstuvwxyz"

var connectedAppsChallenge = connectedapps.S256(connectedAppsVerifier)

const connectedAppsRedirect = "https://client.example/oauth/callback"

// connectedAppsForm sends a form-encoded body, filled, as OAuth's token and
// revocation endpoints take it.
func connectedAppsForm(s string) tourOpt {
	return func(tr *tour, r *tourReq) {
		r.body, r.sendBody = []byte(tr.fill(s)), true
		r.header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
}

// connectedAppsRPC is a JSON-RPC body for /mcp.
func connectedAppsRPC(id int, method, params string) tourOpt {
	if params == "" {
		return jsonBody(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q}`, id, method))
	}
	return jsonBody(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, id, method, params))
}

// connectedAppsCall is a tools/call body.
func connectedAppsCall(id int, tool, args string) tourOpt {
	return connectedAppsRPC(id, "tools/call", fmt.Sprintf(`{"name":%q,"arguments":%s}`, tool, args))
}

func connectedAppsTour(tr *tour) {
	o, anon := tr.owner, tr.anon
	tr.remember("verifier", connectedAppsVerifier)
	tr.remember("challenge", connectedAppsChallenge)
	tr.pattern(`"client_id_issued_at":[0-9]+`, `"client_id_issued_at":"<issued-at>"`,
		"the Unix time the client was registered")

	tr.setup("the member's project, in their own workspace", o, "POST /api/v1/projects",
		jsonBody(`{"name":"Tour connected"}`)).capture("project", "/id")
	other := tr.sharedWorkspace("other", "Tour Elsewhere")
	tr.setup("a project of another workspace of the member's", o, "POST /api/v1/projects", actingIn(other),
		jsonBody(`{"name":"Tour elsewhere"}`)).capture("project.other", "/id")
	// Back in the member's own workspace, the one the app is granted, whose
	// events the steps read.
	tr.actIn(o, o.home)

	// Discovery.
	tr.step("the endpoint with no token: 401 and the challenge that names the resource metadata", anon,
		"POST /mcp", connectedAppsRPC(1, "initialize", `{"protocolVersion":"2025-06-18"}`))
	tr.step("the protected resource metadata", anon, "GET /.well-known/oauth-protected-resource")
	tr.step("the same at the path-suffixed address", anon, "GET /.well-known/oauth-protected-resource/mcp")
	tr.step("the authorization server metadata", anon, "GET /.well-known/oauth-authorization-server")

	// Registration.
	tr.step("register with a redirect URI on another host over plain http: refused", anon, "POST /oauth/register",
		jsonBody(`{"client_name":"Tour Client","redirect_uris":["http://client.example/cb"]}`))
	tr.step("register a client: public, no secret", anon, "POST /oauth/register",
		jsonBody(`{"client_name":"Tour Client","redirect_uris":["`+connectedAppsRedirect+`"],"token_endpoint_auth_method":"client_secret_basic"}`),
		note("it asked for a secret and is registered public all the same, as the answer says")).
		capture("client", "/client_id")

	// Authorization, then the member's consent.
	authorize := func(title, q string, opts ...tourOpt) *tourResult {
		return tr.step(title, anon, "GET /oauth/authorize", append([]tourOpt{query(q)}, opts...)...)
	}
	authorize("authorize with a redirect URI the client did not register: 400, and no redirect",
		"response_type=code&client_id={{client}}&redirect_uri=https%3A%2F%2Fevil.example%2Fcb&code_challenge={{challenge}}&code_challenge_method=S256&state=s1")
	authorize("authorize without PKCE: back to the client with invalid_request and the state",
		"response_type=code&client_id={{client}}&redirect_uri="+strings.ReplaceAll(connectedAppsRedirect, "/", "%2F")+"&state=s2")
	authorize("authorize: on to the app's consent page with the request",
		"response_type=code&client_id={{client}}&redirect_uri="+strings.ReplaceAll(connectedAppsRedirect, "/", "%2F")+"&code_challenge={{challenge}}&code_challenge_method=S256&state=s3&scope=openv%3Aread")

	tr.step("the consent page reads the app", o, "GET /api/v1/oauth/consent",
		query("client_id={{client}}&redirect_uri="+strings.ReplaceAll(connectedAppsRedirect, "/", "%2F")))
	tr.step("the consent with no session", anon, "GET /api/v1/oauth/consent",
		query("client_id={{client}}&redirect_uri="+strings.ReplaceAll(connectedAppsRedirect, "/", "%2F")))
	consentAs := func(title string, a *tourActor, orgID, scope string, approve bool, opts ...tourOpt) *tourResult {
		return tr.step(title, a, "POST /api/v1/oauth/consent", append([]tourOpt{jsonBody(fmt.Sprintf(
			`{"client_id":"{{client}}","redirect_uri":%q,"code_challenge":"{{challenge}}","code_challenge_method":"S256","state":"s3","scope":%q,"org_id":%q,"approve":%t}`,
			connectedAppsRedirect, scope, orgID, approve))}, opts...)...)
	}
	consent := func(title, orgID, scope string, approve bool, opts ...tourOpt) *tourResult {
		return consentAs(title, o, orgID, scope, approve, opts...)
	}
	consent("decline: back to the client with access_denied and the state", "{{owner.workspace}}", "openv:read", false)
	code := consent("approve read only for the member's workspace: back to the client with a code", "{{owner.workspace}}", "openv:read", true)
	code.captureMatch("code.spent", "/redirect_to", `[?&]code=(ovac_[0-9a-f]{64})`)
	token := func(title, form string, opts ...tourOpt) *tourResult {
		return tr.step(title, anon, "POST /oauth/token", append([]tourOpt{connectedAppsForm(form)}, opts...)...)
	}
	token("spend the code with the wrong verifier: invalid_grant",
		"grant_type=authorization_code&code={{code.spent}}&client_id={{client}}&redirect_uri="+connectedAppsRedirect+"&code_verifier=tour-wrong-verifier-0123456789-abcdefghijklmnopqr")
	token("the same code again, with the right verifier: spent by the first attempt",
		"grant_type=authorization_code&code={{code.spent}}&client_id={{client}}&redirect_uri="+connectedAppsRedirect+"&code_verifier={{verifier}}")
	consent("approve read only again", "{{owner.workspace}}", "openv:read", true).
		captureMatch("code.read", "/redirect_to", `[?&]code=(ovac_[0-9a-f]{64})`)
	pair := token("spend the code: an hour's access token, a refresh token and the scope",
		"grant_type=authorization_code&code={{code.read}}&client_id={{client}}&redirect_uri="+connectedAppsRedirect+"&code_verifier={{verifier}}")
	readToken := pair.value("/access_token")
	pair.capture("refresh.read", "/refresh_token")
	reader := tr.bearerActor("reader", readToken, "the connected app holding a read-only token for the "+
		"member's own workspace")

	// The endpoint, read only.
	mcp := func(title string, a *tourActor, opts ...tourOpt) *tourResult {
		return tr.step(title, a, "POST /mcp", opts...)
	}
	mcp("initialize: the revision the client asked for", reader, connectedAppsRPC(1, "initialize", `{"protocolVersion":"2025-03-26"}`))
	mcp("the client's initialized notification: 202, no body", reader,
		jsonBody(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	mcp("tools/list: the read-only tools", reader, connectedAppsRPC(2, "tools/list", ""))
	mcp("list_projects: the member's projects in the granted workspace", reader, connectedAppsCall(3, "list_projects", `{}`))
	mcp("get_project_map of a project in another of the member's workspaces: as if it did not exist", reader,
		connectedAppsCall(4, "get_project_map", `{"project_id":"{{project.other}}"}`))
	mcp("create_artifact on a read-only grant: no such tool", reader,
		connectedAppsCall(5, "create_artifact", `{"project_id":"{{project}}","type":"requirement","title":"x"}`))

	// Refresh, then read and write.
	token("refresh: a new pair",
		"grant_type=refresh_token&refresh_token={{refresh.read}}&client_id={{client}}").
		capture("access.refreshed", "/access_token")
	token("the old refresh token again: spent",
		"grant_type=refresh_token&refresh_token={{refresh.read}}&client_id={{client}}")
	token("a client no longer registered: 401 invalid_client, so the app registers again",
		"grant_type=refresh_token&refresh_token=ovrt_unknown&client_id=00000000-0000-4000-8000-000000000000")
	consent("approve read and write: the same grant takes the scope", "{{owner.workspace}}", "", true).
		captureMatch("code.write", "/redirect_to", `[?&]code=(ovac_[0-9a-f]{64})`)
	writeToken := token("spend it",
		"grant_type=authorization_code&code={{code.write}}&client_id={{client}}&redirect_uri="+connectedAppsRedirect+"&code_verifier={{verifier}}").
		value("/access_token")
	writer := tr.bearerActor("writer", writeToken, "the connected app holding a read-and-write token for "+
		"the member's own workspace")
	mcp("create_artifact: made as the member", writer,
		connectedAppsCall(6, "create_artifact", `{"project_id":"{{project}}","type":"requirement","title":"Written by a connected app"}`))
	tr.step("the member's connected apps: one grant, read and write", o, "GET /api/v1/me/connected-apps").
		capture("grant", "/0/id")
	tr.step("the access token is no key to the rest of the API", writer, "GET /api/v1/projects")

	// A member who leaves the workspace an app acts in takes the app's
	// reach there with them.
	c := tr.actor("colleague")
	tr.join(c, "{{other}}", "member")
	consentAs("the colleague approves read only for the shared workspace", c, "{{other}}", "openv:read", true).
		captureMatch("code.colleague", "/redirect_to", `[?&]code=(ovac_[0-9a-f]{64})`)
	colleagueApp := tr.bearerActor("colleague.app", token("the colleague's app spends its code",
		"grant_type=authorization_code&code={{code.colleague}}&client_id={{client}}&redirect_uri="+connectedAppsRedirect+"&code_verifier={{verifier}}").
		value("/access_token"), "the connected app the colleague approved for the shared workspace")
	mcp("the colleague's app works in the shared workspace: no project there is the colleague's, so the list is empty", colleagueApp,
		connectedAppsCall(9, "list_projects", `{}`))
	tr.setup("the owner removes the colleague from the shared workspace", o, "DELETE /api/v1/orgs/{id}/members/{userId}",
		at("id", "{{other}}", "userId", "{{colleague}}"), actingIn("{{other}}"), expect(204))
	mcp("the colleague's app once they have left the workspace: 403, nothing there", colleagueApp,
		connectedAppsCall(10, "list_projects", `{}`))

	mcp("an MCP-Protocol-Version the endpoint does not speak", writer, connectedAppsRPC(8, "ping", ""),
		withHeader("MCP-Protocol-Version", "1999-01-01"))
	mcp("initialize carrying a revision the endpoint does not speak: answered with the newest it does", writer,
		connectedAppsRPC(11, "initialize", `{"protocolVersion":"2099-01-01"}`), withHeader("MCP-Protocol-Version", "2099-01-01"))

	// Revocation.
	tr.step("revoke the app", o, "DELETE /api/v1/me/connected-apps/{id}", at("id", "{{grant}}"))
	mcp("the revoked app's token: 401 invalid_token", writer, connectedAppsRPC(7, "ping", ""))
	tr.step("revoke it again: gone", o, "DELETE /api/v1/me/connected-apps/{id}", at("id", "{{grant}}"))
	tr.step("the revocation endpoint, for a token that is gone: 200 all the same", anon, "POST /oauth/revoke",
		connectedAppsForm("token={{access.refreshed}}&client_id={{client}}"))

	// What the endpoint does not offer.
	tr.step("GET /mcp: no stream", anon, "GET /mcp")
	tr.step("DELETE /mcp: no session", anon, "DELETE /mcp")
}
