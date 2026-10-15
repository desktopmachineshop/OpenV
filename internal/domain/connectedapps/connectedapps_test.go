package connectedapps

import (
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// memRepo is an in-memory Repository with the semantics the Postgres one
// has: one live grant per member, client and workspace; codes and refresh
// tokens spent on use; a revoked grant takes its tokens with it.
type memRepo struct {
	clients map[string]*Client
	grants  map[string]*Grant
	codes   map[string]*Code
	tokens  map[string]*Token
}

func newMemRepo() *memRepo {
	return &memRepo{clients: map[string]*Client{}, grants: map[string]*Grant{}, codes: map[string]*Code{}, tokens: map[string]*Token{}}
}

func (m *memRepo) CreateClient(c *Client) error { m.clients[c.ID] = c; return nil }
func (m *memRepo) GetClient(id string) (*Client, error) {
	return m.clients[id], nil
}
func (m *memRepo) SaveGrant(userID, orgID, clientID, scope string, now time.Time) (*Grant, error) {
	for _, g := range m.grants {
		if g.RevokedAt == nil && g.UserID == userID && g.OrgID == orgID && g.ClientID == clientID {
			g.Scope = scope
			return g, nil
		}
	}
	g := &Grant{ID: uuid.NewString(), UserID: userID, OrgID: orgID, ClientID: clientID, ClientName: m.clients[clientID].Name, Scope: scope, CreatedAt: now}
	m.grants[g.ID] = g
	return g, nil
}
func (m *memRepo) GetGrant(id string) (*Grant, error) {
	g := m.grants[id]
	if g == nil || g.RevokedAt != nil {
		return nil, nil
	}
	return g, nil
}
func (m *memRepo) ListGrants(userID string) ([]Grant, error) {
	var out []Grant
	for _, g := range m.grants {
		if g.UserID == userID && g.RevokedAt == nil {
			out = append(out, *g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}
func (m *memRepo) RevokeGrant(id string, now time.Time) error {
	if g := m.grants[id]; g != nil {
		g.RevokedAt = &now
	}
	for h, c := range m.codes {
		if c.GrantID == id {
			delete(m.codes, h)
		}
	}
	for h, t := range m.tokens {
		if t.GrantID == id {
			delete(m.tokens, h)
		}
	}
	return nil
}
func (m *memRepo) CreateCode(c *Code) error { m.codes[c.Hash] = c; return nil }
func (m *memRepo) ConsumeCode(hash string) (*Code, error) {
	c := m.codes[hash]
	delete(m.codes, hash)
	return c, nil
}
func (m *memRepo) CreateToken(t *Token) error { m.tokens[t.ID] = t; return nil }
func (m *memRepo) GrantForAccess(hash string, now time.Time) (*Grant, error) {
	for _, t := range m.tokens {
		if t.AccessHash == hash && now.Before(t.AccessExpiresAt) {
			return m.GetGrant(t.GrantID)
		}
	}
	return nil, nil
}
func (m *memRepo) ConsumeRefresh(hash string, now time.Time) (*Token, error) {
	for id, t := range m.tokens {
		if t.RefreshHash == hash && now.Before(t.RefreshExpiresAt) {
			delete(m.tokens, id)
			return t, nil
		}
	}
	return nil, nil
}
func (m *memRepo) DeleteToken(hash, clientID string) error {
	for id, t := range m.tokens {
		if (t.AccessHash == hash || t.RefreshHash == hash) && m.grants[t.GrantID].ClientID == clientID {
			delete(m.tokens, id)
		}
	}
	return nil
}

const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk-and-some-more"

func fixture(t *testing.T) (*DefaultService, *memRepo, *Client, *time.Time) {
	t.Helper()
	repo := newMemRepo()
	s := NewService(repo)
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	c, err := s.Register("Claude", []string{"https://claude.ai/api/mcp/auth_callback"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return s, repo, c, &now
}

func approve(t *testing.T, s *DefaultService, c *Client, scope string) string {
	t.Helper()
	code, err := s.Approve("user-1", "org-1", c.ID, c.RedirectURIs[0], S256(verifier), scope)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	return code
}

func TestRegistrationAcceptsWhatAClientMayUseAndNothingElse(t *testing.T) {
	for _, ok := range []string{
		"https://claude.ai/api/mcp/auth_callback",
		"http://127.0.0.1:33418/callback",
		"http://localhost:6274/oauth/callback",
		"cursor://anysphere.cursor-retrieval/oauth/user-openv/callback",
	} {
		if !AllowedRedirect(ok) {
			t.Errorf("%s refused", ok)
		}
	}
	for _, bad := range []string{
		"", "/relative", "http://example.com/callback", "https://example.com/cb#frag",
		"javascript:alert(1)", "data:text/html,x", "file:///etc/passwd", "https://" + strings.Repeat("a", 600),
	} {
		if AllowedRedirect(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
	s, _, _, _ := fixture(t)
	if _, err := s.Register("x", nil); !errors.Is(err, ErrInvalidRedirect) {
		t.Errorf("no redirect URIs: err = %v", err)
	}
	if _, err := s.Register("x", []string{"https://ok.example/cb", "http://evil.example/cb"}); !errors.Is(err, ErrInvalidRedirect) {
		t.Errorf("one bad redirect URI: err = %v", err)
	}
	c, err := s.Register("  ", []string{"https://ok.example/cb"})
	if err != nil || c.Name != "MCP client" {
		t.Errorf("blank name: %+v, %v", c, err)
	}
}

func TestAuthorizingChecksTheClientAndItsOwnRedirect(t *testing.T) {
	s, _, c, _ := fixture(t)
	if _, err := s.Authorizing(c.ID, c.RedirectURIs[0]); err != nil {
		t.Fatalf("own redirect: %v", err)
	}
	if _, err := s.Authorizing(c.ID, "https://claude.ai/other"); !errors.Is(err, ErrInvalidRedirect) {
		t.Errorf("other redirect: %v", err)
	}
	if _, err := s.Authorizing(uuid.NewString(), c.RedirectURIs[0]); !errors.Is(err, ErrInvalidClient) {
		t.Errorf("unknown client: %v", err)
	}
	if _, err := s.Authorizing("not-a-uuid", c.RedirectURIs[0]); !errors.Is(err, ErrInvalidClient) {
		t.Errorf("malformed client id: %v", err)
	}
}

func TestALoopbackRedirectMatchesOnAnyPort(t *testing.T) {
	s, _, _, _ := fixture(t)
	c, err := s.Register("Claude Code", []string{"http://localhost:3118/callback", "http://127.0.0.1/callback"})
	if err != nil {
		t.Fatal(err)
	}
	for _, ok := range []string{"http://localhost:3118/callback", "http://localhost:51234/callback", "http://127.0.0.1:40001/callback"} {
		if _, err := s.Authorizing(c.ID, ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"http://localhost:3118/other", "http://127.0.0.1:3118/callback?x=1", "http://[::1]:3118/callback",
		"https://localhost:3118/callback", "http://localhost.evil.example:3118/callback",
	} {
		if _, err := s.Authorizing(c.ID, bad); !errors.Is(err, ErrInvalidRedirect) {
			t.Errorf("%s: %v", bad, err)
		}
	}
	// Only a loopback redirect ignores the port.
	web, _ := s.Register("Web", []string{"https://app.example:8443/cb"})
	if _, err := s.Authorizing(web.ID, "https://app.example:9443/cb"); !errors.Is(err, ErrInvalidRedirect) {
		t.Errorf("https on another port: %v", err)
	}
}

func TestATokenRequestFromAnUnknownClientSaysSo(t *testing.T) {
	s, _, c, _ := fixture(t)
	code := approve(t, s, c, "")
	if _, err := s.Exchange(code, uuid.NewString(), c.RedirectURIs[0], verifier); !errors.Is(err, ErrInvalidClient) {
		t.Errorf("exchange: %v", err)
	}
	tok, err := s.Exchange(code, c.ID, c.RedirectURIs[0], verifier)
	if err != nil {
		t.Fatalf("the code survives a request from an unknown client: %v", err)
	}
	if _, err := s.Refresh(tok.RefreshToken, uuid.NewString()); !errors.Is(err, ErrInvalidClient) {
		t.Errorf("refresh: %v", err)
	}
}

func TestScopeIsReadOrReadAndWrite(t *testing.T) {
	cases := map[string]string{
		"":                        "openv:read openv:write",
		"openv:read":              "openv:read",
		"openv:write":             "openv:read openv:write",
		"openv:write openv:read":  "openv:read openv:write",
		"openv:read  openv:read ": "openv:read",
	}
	for in, want := range cases {
		if got, err := NormalizeScope(in); err != nil || got != want {
			t.Errorf("NormalizeScope(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := NormalizeScope("admin"); !errors.Is(err, ErrInvalidScope) {
		t.Errorf("unknown scope: %v", err)
	}
}

func TestACodeBuysOneTokenPairForTheRightVerifier(t *testing.T) {
	s, _, c, _ := fixture(t)
	code := approve(t, s, c, "openv:read")

	// Wrong verifier: refused, and the code is spent by the attempt.
	if _, err := s.Exchange(code, c.ID, c.RedirectURIs[0], strings.Repeat("x", 43)); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("wrong verifier: %v", err)
	}
	if _, err := s.Exchange(code, c.ID, c.RedirectURIs[0], verifier); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("a code is spent by the first attempt: %v", err)
	}

	code = approve(t, s, c, "openv:read")
	tok, err := s.Exchange(code, c.ID, c.RedirectURIs[0], verifier)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if !strings.HasPrefix(tok.AccessToken, AccessPrefix) || !strings.HasPrefix(tok.RefreshToken, RefreshPrefix) || tok.TokenType != "Bearer" || tok.ExpiresIn != 3600 || tok.Scope != "openv:read" {
		t.Fatalf("tokens = %+v", tok)
	}
	g, err := s.Authenticate(tok.AccessToken)
	if err != nil || g.UserID != "user-1" || g.OrgID != "org-1" || g.CanWrite() {
		t.Fatalf("Authenticate = %+v, %v", g, err)
	}
	if _, err := s.Exchange(code, c.ID, c.RedirectURIs[0], verifier); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("code reused: %v", err)
	}
}

func TestACodeIsBoundToItsClientRedirectAndLifetime(t *testing.T) {
	s, _, c, now := fixture(t)
	other, _ := s.Register("Other", []string{"https://other.example/cb"})
	for name, try := range map[string]func(code string) error{
		"another client": func(code string) error {
			_, err := s.Exchange(code, other.ID, c.RedirectURIs[0], verifier)
			return err
		},
		"another redirect": func(code string) error {
			_, err := s.Exchange(code, c.ID, "https://claude.ai/elsewhere", verifier)
			return err
		},
		"expired": func(code string) error {
			*now = now.Add(CodeTTL + time.Second)
			defer func() { *now = now.Add(-CodeTTL - time.Second) }()
			_, err := s.Exchange(code, c.ID, c.RedirectURIs[0], verifier)
			return err
		},
	} {
		code := approve(t, s, c, "")
		if err := try(code); !errors.Is(err, ErrInvalidGrant) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := s.Approve("user-1", "org-1", c.ID, c.RedirectURIs[0], "plain-challenge", ""); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("a challenge that is not S256: %v", err)
	}
}

func TestARefreshTokenIsSpentAndReplaced(t *testing.T) {
	s, _, c, now := fixture(t)
	tok, err := s.Exchange(approve(t, s, c, ""), c.ID, c.RedirectURIs[0], verifier)
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * AccessTTL)
	if _, err := s.Authenticate(tok.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("an access token outlives its hour: %v", err)
	}
	next, err := s.Refresh(tok.RefreshToken, c.ID)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if next.RefreshToken == tok.RefreshToken || next.Scope != "openv:read openv:write" {
		t.Fatalf("refresh = %+v", next)
	}
	if _, err := s.Refresh(tok.RefreshToken, c.ID); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("a spent refresh token works again: %v", err)
	}
	if g, err := s.Authenticate(next.AccessToken); err != nil || !g.CanWrite() {
		t.Fatalf("new access token: %+v, %v", g, err)
	}
	other, _ := s.Register("Other", []string{"https://other.example/cb"})
	if _, err := s.Refresh(next.RefreshToken, other.ID); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("another client's refresh: %v", err)
	}
}

func TestRevokingAGrantEndsItsTokens(t *testing.T) {
	s, _, c, _ := fixture(t)
	tok, _ := s.Exchange(approve(t, s, c, ""), c.ID, c.RedirectURIs[0], verifier)
	list, _ := s.List("user-1")
	if len(list) != 1 || list[0].ClientName != "Claude" {
		t.Fatalf("List = %+v", list)
	}
	if err := s.RevokeForUser("user-2", list[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another member revokes: %v", err)
	}
	if err := s.RevokeForUser("user-1", list[0].ID); err != nil {
		t.Fatalf("RevokeForUser: %v", err)
	}
	if _, err := s.Authenticate(tok.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("access token after revoke: %v", err)
	}
	if _, err := s.Refresh(tok.RefreshToken, c.ID); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("refresh after revoke: %v", err)
	}
	if list, _ := s.List("user-1"); len(list) != 0 {
		t.Fatalf("revoked grant still listed: %+v", list)
	}
}

func TestConsentingAgainUpdatesTheOneGrant(t *testing.T) {
	s, _, c, _ := fixture(t)
	approve(t, s, c, "openv:read")
	approve(t, s, c, "")
	list, _ := s.List("user-1")
	if len(list) != 1 || list[0].Scope != "openv:read openv:write" {
		t.Fatalf("List = %+v", list)
	}
}

func TestRevokeEndsATokenPairAndIgnoresTheUnknown(t *testing.T) {
	s, _, c, _ := fixture(t)
	tok, _ := s.Exchange(approve(t, s, c, ""), c.ID, c.RedirectURIs[0], verifier)
	if err := s.Revoke("nonsense", c.ID); err != nil {
		t.Fatalf("unknown token: %v", err)
	}
	if err := s.Revoke(tok.RefreshToken, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(tok.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("access token survives its pair's revoke: %v", err)
	}
	if _, err := s.Authenticate("Bearer-ish"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("a token without the prefix: %v", err)
	}
}
