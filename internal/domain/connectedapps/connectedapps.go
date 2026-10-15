// Package connectedapps is how a member attaches an AI client to a workspace
// over the remote MCP endpoint (REQ-44): the OAuth 2.1 profile the MCP
// authorization spec names, with dynamic client registration, PKCE, a
// consent the member gives per client and per workspace, short-lived access
// tokens and rotating refresh tokens.
//
// A client registers itself and holds no secret. A member's consent is a
// grant: one client, one workspace, read only or read and write. Every token
// belongs to a grant, so revoking the grant ends every token the client
// holds. Only hashes of codes and tokens are stored.
package connectedapps

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openv/requirements-platform/internal/domain/tokens"
)

// Scopes. A grant holds one or both; read and write is what a member gives
// unless they choose read only.
const (
	ScopeRead  = "openv:read"
	ScopeWrite = "openv:write"
)

// Lifetimes. An access token lives an hour, so a revoked grant stops a
// client within the hour even where a token was copied before the revoke
// removed it; a refresh token lives thirty days and is replaced each time it
// is used; a code lives ten minutes and is spent once.
const (
	AccessTTL  = time.Hour
	RefreshTTL = 30 * 24 * time.Hour
	CodeTTL    = 10 * time.Minute
)

// Token prefixes, so a bearer token is recognisable as an access token
// before any lookup, and a leaked one is easy to search for.
const (
	AccessPrefix  = "ovat_"
	RefreshPrefix = "ovrt_"
	codePrefix    = "ovac_"
)

// Registration bounds.
const (
	maxClientName   = 100
	maxRedirectURIs = 5
	maxRedirectURI  = 500
)

// Errors. The OAuth handlers map them to the error codes RFC 6749 names.
var (
	// ErrInvalidClient: no client is registered under the id.
	ErrInvalidClient = errors.New("unknown client")
	// ErrInvalidRedirect: the redirect URI is not one the client registered,
	// or registration offered one that may not be used.
	ErrInvalidRedirect = errors.New("redirect_uri is not registered for this client")
	// ErrInvalidRequest: a required parameter is missing or malformed.
	ErrInvalidRequest = errors.New("invalid request")
	// ErrInvalidGrant: the code or refresh token is unknown, spent, expired,
	// issued to another client, or its grant was revoked; or the PKCE
	// verifier does not match.
	ErrInvalidGrant = errors.New("invalid grant")
	// ErrInvalidScope: the scope names something other than openv:read and
	// openv:write.
	ErrInvalidScope = errors.New("invalid scope")
	// ErrInvalidToken: an access token that is unknown, expired or revoked.
	ErrInvalidToken = errors.New("invalid token")
	// ErrNotFound: no grant of the caller's has the id.
	ErrNotFound = errors.New("connected app not found")
)

// Client is a registered AI client. It has no secret: a public client
// proves itself with PKCE on every authorization.
type Client struct {
	ID           string    `json:"client_id"`
	Name         string    `json:"client_name"`
	RedirectURIs []string  `json:"redirect_uris"`
	CreatedAt    time.Time `json:"created_at"`
}

// Grant is a member's consent: client ClientID may act as UserID in
// workspace OrgID with Scope.
type Grant struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	OrgID      string     `json:"org_id"`
	OrgName    string     `json:"org_name,omitempty"`
	ClientID   string     `json:"client_id"`
	ClientName string     `json:"client_name"`
	Scope      string     `json:"scope"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"-"`
}

// CanWrite reports whether the grant allows tools that change the project.
func (g *Grant) CanWrite() bool { return hasScope(g.Scope, ScopeWrite) }

// Code is a stored authorization code: its hash, the grant it was issued
// for, and what the token request must repeat.
type Code struct {
	Hash          string
	GrantID       string
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	ExpiresAt     time.Time
}

// Token is a stored access and refresh token pair.
type Token struct {
	ID               string
	GrantID          string
	AccessHash       string
	RefreshHash      string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
	CreatedAt        time.Time
}

// Tokens is what a token request answers with, in plaintext, once.
type Tokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// Repository is the storage port, implemented in internal/persistence.
type Repository interface {
	CreateClient(c *Client) error
	// GetClient returns nil, nil when no client has the id.
	GetClient(id string) (*Client, error)
	// SaveGrant records consent: the member's live grant for this client in
	// this workspace takes the scope, or a new grant is made.
	SaveGrant(userID, orgID, clientID, scope string, now time.Time) (*Grant, error)
	// GetGrant returns a live grant, nil, nil when none (revoked included).
	GetGrant(id string) (*Grant, error)
	// ListGrants returns the member's live grants, newest first, with the
	// client's and the workspace's names.
	ListGrants(userID string) ([]Grant, error)
	// RevokeGrant marks the grant revoked and deletes its codes and tokens.
	RevokeGrant(id string, now time.Time) error
	CreateCode(c *Code) error
	// ConsumeCode deletes and returns the code, nil, nil when none.
	ConsumeCode(hash string) (*Code, error)
	CreateToken(t *Token) error
	// GrantForAccess returns the live grant whose unexpired access token
	// has the hash, nil, nil when none, and notes the grant was used.
	GrantForAccess(hash string, now time.Time) (*Grant, error)
	// ConsumeRefresh deletes the token pair whose unexpired refresh token
	// has the hash and returns it, nil, nil when none.
	ConsumeRefresh(hash string, now time.Time) (*Token, error)
	// DeleteToken deletes the pair one of whose tokens has the hash, if it
	// belongs to a grant of clientID.
	DeleteToken(hash, clientID string) error
}

// Service is the connected-apps logic the API calls.
type Service interface {
	Register(name string, redirectURIs []string) (*Client, error)
	// Authorizing checks an authorization request before the member is
	// asked: the client exists and the redirect URI is its own.
	Authorizing(clientID, redirectURI string) (*Client, error)
	// Approve records the member's consent and returns a code for the
	// client's redirect.
	Approve(userID, orgID, clientID, redirectURI, codeChallenge, scope string) (string, error)
	Exchange(code, clientID, redirectURI, verifier string) (*Tokens, error)
	Refresh(refreshToken, clientID string) (*Tokens, error)
	Revoke(token, clientID string) error
	Authenticate(accessToken string) (*Grant, error)
	List(userID string) ([]Grant, error)
	RevokeForUser(userID, grantID string) error
}

// DefaultService implements Service over a Repository.
type DefaultService struct {
	repo Repository
	now  func() time.Time
}

// NewService builds the service.
func NewService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo, now: time.Now}
}

// Register records a client from a dynamic registration request (RFC 7591).
func (s *DefaultService) Register(name string, redirectURIs []string) (*Client, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "MCP client"
	}
	if len(name) > maxClientName {
		name = name[:maxClientName]
	}
	if len(redirectURIs) == 0 || len(redirectURIs) > maxRedirectURIs {
		return nil, ErrInvalidRedirect
	}
	for _, u := range redirectURIs {
		if !AllowedRedirect(u) {
			return nil, ErrInvalidRedirect
		}
	}
	c := &Client{ID: uuid.NewString(), Name: name, RedirectURIs: redirectURIs, CreatedAt: s.now().UTC()}
	if err := s.repo.CreateClient(c); err != nil {
		return nil, err
	}
	return c, nil
}

// AllowedRedirect reports whether a client may register uri: an absolute
// https URL, an http URL on the loopback interface (a desktop client
// listening locally), or a private-use scheme a native app owns. Schemes a
// browser would run or read locally are refused, as is a fragment.
func AllowedRedirect(uri string) bool {
	if uri == "" || len(uri) > maxRedirectURI {
		return false
	}
	u, err := url.Parse(uri)
	if err != nil || u.Scheme == "" || u.Fragment != "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return u.Host != ""
	case "http":
		host := u.Hostname()
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	case "javascript", "data", "file", "vbscript", "about", "blob", "ftp", "ws", "wss":
		return false
	}
	// A private-use scheme (RFC 8252 7.1): cursor://, vscode://.
	return true
}

// Authorizing checks a client and its redirect URI.
func (s *DefaultService) Authorizing(clientID, redirectURI string) (*Client, error) {
	c, err := s.client(clientID)
	if err != nil {
		return nil, err
	}
	for _, u := range c.RedirectURIs {
		if u == redirectURI {
			return c, nil
		}
	}
	return nil, ErrInvalidRedirect
}

func (s *DefaultService) client(clientID string) (*Client, error) {
	if _, err := uuid.Parse(clientID); err != nil {
		return nil, ErrInvalidClient
	}
	c, err := s.repo.GetClient(clientID)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, ErrInvalidClient
	}
	return c, nil
}

// NormalizeScope turns a requested scope into what a grant holds: the
// scopes named, in a fixed order, read always included (a client that may
// write may read). Empty means read and write.
func NormalizeScope(scope string) (string, error) {
	fields := strings.Fields(scope)
	if len(fields) == 0 {
		return ScopeRead + " " + ScopeWrite, nil
	}
	write := false
	for _, f := range fields {
		switch f {
		case ScopeRead:
		case ScopeWrite:
			write = true
		default:
			return "", ErrInvalidScope
		}
	}
	if write {
		return ScopeRead + " " + ScopeWrite, nil
	}
	return ScopeRead, nil
}

func hasScope(scope, want string) bool {
	for _, f := range strings.Fields(scope) {
		if f == want {
			return true
		}
	}
	return false
}

// Approve records consent and mints a code. The caller has checked that
// userID may act in orgID; the code challenge is the S256 one the client
// sent.
func (s *DefaultService) Approve(userID, orgID, clientID, redirectURI, codeChallenge, scope string) (string, error) {
	if userID == "" || orgID == "" || !validChallenge(codeChallenge) {
		return "", ErrInvalidRequest
	}
	scope, err := NormalizeScope(scope)
	if err != nil {
		return "", err
	}
	if _, err := s.Authorizing(clientID, redirectURI); err != nil {
		return "", err
	}
	now := s.now().UTC()
	g, err := s.repo.SaveGrant(userID, orgID, clientID, scope, now)
	if err != nil {
		return "", err
	}
	plain, err := tokens.NewToken()
	if err != nil {
		return "", err
	}
	code := codePrefix + plain
	if err := s.repo.CreateCode(&Code{
		Hash:          tokens.HashToken(code),
		GrantID:       g.ID,
		ClientID:      clientID,
		RedirectURI:   redirectURI,
		CodeChallenge: codeChallenge,
		ExpiresAt:     now.Add(CodeTTL),
	}); err != nil {
		return "", err
	}
	return code, nil
}

// validChallenge holds for an S256 challenge: 43 characters of base64url.
func validChallenge(c string) bool {
	if len(c) != 43 {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(c)
	return err == nil
}

// S256 is the PKCE transform of a verifier (RFC 7636 4.2).
func S256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Exchange spends a code for the first token pair.
func (s *DefaultService) Exchange(code, clientID, redirectURI, verifier string) (*Tokens, error) {
	if code == "" || clientID == "" || len(verifier) < 43 || len(verifier) > 128 {
		return nil, ErrInvalidRequest
	}
	c, err := s.repo.ConsumeCode(tokens.HashToken(code))
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if c == nil || now.After(c.ExpiresAt) || c.ClientID != clientID || c.RedirectURI != redirectURI || S256(verifier) != c.CodeChallenge {
		return nil, ErrInvalidGrant
	}
	g, err := s.repo.GetGrant(c.GrantID)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, ErrInvalidGrant
	}
	return s.issue(g, now)
}

// Refresh replaces a token pair with a new one. The old refresh token is
// spent whether or not the new pair reaches the client.
func (s *DefaultService) Refresh(refreshToken, clientID string) (*Tokens, error) {
	if !strings.HasPrefix(refreshToken, RefreshPrefix) || clientID == "" {
		return nil, ErrInvalidGrant
	}
	now := s.now().UTC()
	t, err := s.repo.ConsumeRefresh(tokens.HashToken(refreshToken), now)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrInvalidGrant
	}
	g, err := s.repo.GetGrant(t.GrantID)
	if err != nil {
		return nil, err
	}
	if g == nil || g.ClientID != clientID {
		return nil, ErrInvalidGrant
	}
	return s.issue(g, now)
}

func (s *DefaultService) issue(g *Grant, now time.Time) (*Tokens, error) {
	access, err := tokens.NewToken()
	if err != nil {
		return nil, err
	}
	refresh, err := tokens.NewToken()
	if err != nil {
		return nil, err
	}
	access, refresh = AccessPrefix+access, RefreshPrefix+refresh
	if err := s.repo.CreateToken(&Token{
		ID:               uuid.NewString(),
		GrantID:          g.ID,
		AccessHash:       tokens.HashToken(access),
		RefreshHash:      tokens.HashToken(refresh),
		AccessExpiresAt:  now.Add(AccessTTL),
		RefreshExpiresAt: now.Add(RefreshTTL),
		CreatedAt:        now,
	}); err != nil {
		return nil, err
	}
	return &Tokens{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int(AccessTTL / time.Second),
		RefreshToken: refresh,
		Scope:        g.Scope,
	}, nil
}

// Revoke ends the token pair one of whose tokens is token (RFC 7009). An
// unknown token is not an error: revocation answers the same either way.
func (s *DefaultService) Revoke(token, clientID string) error {
	if token == "" {
		return nil
	}
	return s.repo.DeleteToken(tokens.HashToken(token), clientID)
}

// Authenticate resolves an access token to its live grant.
func (s *DefaultService) Authenticate(accessToken string) (*Grant, error) {
	if !strings.HasPrefix(accessToken, AccessPrefix) {
		return nil, ErrInvalidToken
	}
	g, err := s.repo.GrantForAccess(tokens.HashToken(accessToken), s.now().UTC())
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, ErrInvalidToken
	}
	return g, nil
}

// List returns the member's connected apps.
func (s *DefaultService) List(userID string) ([]Grant, error) {
	gs, err := s.repo.ListGrants(userID)
	if err != nil {
		return nil, err
	}
	if gs == nil {
		gs = []Grant{}
	}
	return gs, nil
}

// RevokeForUser revokes one of the member's grants; another member's grant
// is one that does not exist.
func (s *DefaultService) RevokeForUser(userID, grantID string) error {
	if _, err := uuid.Parse(grantID); err != nil {
		return ErrNotFound
	}
	g, err := s.repo.GetGrant(grantID)
	if err != nil {
		return err
	}
	if g == nil || g.UserID != userID {
		return ErrNotFound
	}
	return s.repo.RevokeGrant(grantID, s.now().UTC())
}
