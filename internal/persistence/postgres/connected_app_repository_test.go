package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/connectedapps"
)

// The connected-apps service over the real tables (REQ-44): consent, the
// code and both tokens round-trip; a code and a refresh token are spent by
// their first use; revoking a grant, deleting its member or its workspace
// takes every token with it; and one live grant stands per member, client
// and workspace however often the member consents.
func TestConnectedAppsRoundTrip(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewConnectedAppRepository(db)
	svc := connectedapps.NewService(repo)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	userID, orgID := uuid.NewString(), uuid.NewString()
	_, err := db.Exec(`INSERT INTO users (id, email, name, auth_provider, created_at, updated_at)
		VALUES ($1, $2, 'Dana', 'password', NOW(), NOW())`, userID, userID+"@example.com")
	must(err)
	_, err = db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Acme', $2)`, orgID, "acme-"+orgID[:8])
	must(err)

	client, err := svc.Register("Claude", []string{"https://claude.ai/api/mcp/auth_callback", "http://127.0.0.1:9000/cb"})
	must(err)
	got, err := repo.GetClient(client.ID)
	must(err)
	if got == nil || got.Name != "Claude" || len(got.RedirectURIs) != 2 || got.RedirectURIs[1] != "http://127.0.0.1:9000/cb" {
		t.Fatalf("GetClient = %+v", got)
	}
	if none, err := repo.GetClient(uuid.NewString()); err != nil || none != nil {
		t.Fatalf("unknown client = %+v, %v", none, err)
	}

	const verifier = "a-verifier-of-at-least-forty-three-characters-long-xyz"
	approve := func(scope string) string {
		t.Helper()
		code, err := svc.Approve(userID, []string{orgID}, client.ID, client.RedirectURIs[0], connectedapps.S256(verifier), scope)
		must(err)
		return code
	}

	code := approve("openv:read")
	tok, err := svc.Exchange(code, client.ID, client.RedirectURIs[0], verifier)
	must(err)
	if _, err := svc.Exchange(code, client.ID, client.RedirectURIs[0], verifier); err == nil {
		t.Fatal("a code was spent twice")
	}
	g, err := svc.Authenticate(tok.AccessToken)
	must(err)
	if g.UserID != userID || g.OrgID != orgID || g.OrgName != "Acme" || g.ClientName != "Claude" || g.Scope != "openv:read" {
		t.Fatalf("grant = %+v", g)
	}
	if len(g.Workspaces) != 1 || g.Workspaces[0] != (connectedapps.Workspace{ID: orgID, Name: "Acme"}) {
		t.Fatalf("grant workspaces = %+v, want Acme alone", g.Workspaces)
	}

	// A second workspace, given and then the first taken away: the same
	// token reaches the new set at once.
	otherOrg := uuid.NewString()
	_, err = db.Exec(`INSERT INTO organizations (id, name, slug) VALUES ($1, 'Beta', $2)`, otherOrg, "beta-"+otherOrg[:8])
	must(err)
	if _, err := svc.SetWorkspacesForUser(userID, g.ID, []string{orgID, otherOrg}); err != nil {
		t.Fatal(err)
	}
	g, err = svc.Authenticate(tok.AccessToken)
	must(err)
	if !g.Allows(orgID) || !g.Allows(otherOrg) || len(g.Workspaces) != 2 || g.Workspaces[1].Name != "Beta" {
		t.Fatalf("after adding Beta: %+v", g.Workspaces)
	}
	if _, err := svc.SetWorkspacesForUser(userID, g.ID, []string{otherOrg}); err != nil {
		t.Fatal(err)
	}
	g, err = svc.Authenticate(tok.AccessToken)
	must(err)
	if g.Allows(orgID) || !g.Allows(otherOrg) || g.OrgID != orgID {
		t.Fatalf("after taking Acme away: org %s, workspaces %+v; want Beta alone, keyed on Acme still", g.OrgID, g.Workspaces)
	}
	if _, err := svc.SetWorkspacesForUser(userID, g.ID, []string{orgID}); err != nil {
		t.Fatal(err)
	}
	var lastUsed *time.Time
	must(db.QueryRow(`SELECT last_used_at FROM connected_apps WHERE id = $1`, g.ID).Scan(&lastUsed))
	if lastUsed == nil {
		t.Fatal("using a token did not note the grant was used")
	}

	// Consenting again, now for read and write, keeps the one grant.
	approve("")
	list, err := svc.List(userID)
	must(err)
	if len(list) != 1 || list[0].ID != g.ID || list[0].Scope != "openv:read openv:write" {
		t.Fatalf("List = %+v", list)
	}

	next, err := svc.Refresh(tok.RefreshToken, client.ID)
	must(err)
	if _, err := svc.Refresh(tok.RefreshToken, client.ID); err == nil {
		t.Fatal("a refresh token was spent twice")
	}
	if _, err := svc.Authenticate(tok.AccessToken); err == nil {
		t.Fatal("the access token of a spent pair still works")
	}
	if _, err := svc.Authenticate(next.AccessToken); err != nil {
		t.Fatalf("new access token: %v", err)
	}

	// Revocation by the token itself ends just that pair.
	third, err := svc.Refresh(next.RefreshToken, client.ID)
	must(err)
	must(svc.Revoke(third.AccessToken, uuid.NewString())) // another client's revoke does nothing
	if _, err := svc.Authenticate(third.AccessToken); err != nil {
		t.Fatalf("another client revoked the token: %v", err)
	}
	must(svc.Revoke(third.AccessToken, client.ID))
	if _, err := svc.Authenticate(third.AccessToken); err == nil {
		t.Fatal("a revoked token still works")
	}

	// Revoking the grant ends its tokens and its unspent codes.
	pending := approve("")
	live, err := svc.Exchange(approve(""), client.ID, client.RedirectURIs[0], verifier)
	must(err)
	must(svc.RevokeForUser(userID, g.ID))
	if _, err := svc.Authenticate(live.AccessToken); err == nil {
		t.Fatal("a token outlived its grant's revoke")
	}
	if _, err := svc.Exchange(pending, client.ID, client.RedirectURIs[0], verifier); err == nil {
		t.Fatal("a code outlived its grant's revoke")
	}
	if list, _ := svc.List(userID); len(list) != 0 {
		t.Fatalf("revoked grant still listed: %+v", list)
	}
	var revoked int
	must(db.QueryRow(`SELECT COUNT(*) FROM connected_apps WHERE id = $1 AND revoked_at IS NOT NULL`, g.ID).Scan(&revoked))
	if revoked != 1 {
		t.Fatal("the revoked grant was not kept as the record of the consent")
	}

	// Consent after a revoke is a new grant; deleting the workspace takes it
	// and its tokens.
	fresh, err := svc.Exchange(approve(""), client.ID, client.RedirectURIs[0], verifier)
	must(err)
	_, err = db.Exec(`DELETE FROM organizations WHERE id = $1`, orgID)
	must(err)
	if _, err := svc.Authenticate(fresh.AccessToken); err == nil {
		t.Fatal("a token outlived its workspace")
	}
	var tokens int
	must(db.QueryRow(`SELECT COUNT(*) FROM oauth_tokens t JOIN connected_apps g ON g.id = t.grant_id WHERE g.user_id = $1`, userID).Scan(&tokens))
	if tokens != 0 {
		t.Fatalf("%d tokens left behind", tokens)
	}
}
