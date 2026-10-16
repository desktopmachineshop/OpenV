package providers

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Login request statuses.
const (
	LoginPending      = "pending"       // waiting for a worker to pick it up
	LoginClaimed      = "claimed"       // worker is starting the CLI login flow
	LoginURLReady     = "url_ready"     // auth URL available; user should open it
	LoginAwaitingCode = "awaiting_code" // CLI expects a paste-back code
	LoginCompleted    = "completed"
	LoginFailed       = "failed"
	LoginCancelled    = "cancelled"
)

// activeLoginStatuses are non-terminal.
var activeLoginStatuses = map[string]bool{
	LoginPending:      true,
	LoginClaimed:      true,
	LoginURLReady:     true,
	LoginAwaitingCode: true,
}

// Paste kinds: what the worker is waiting for the member to paste back.
// The two CLI flows ask for different things, and only the worker knows
// which — the UI uses this to pick a phone keyboard and label the field.
const (
	// PasteKindCode is a short authorization code (a paste-back CLI flow).
	PasteKindCode = "code"
	// PasteKindURL is the whole redirected address (a loopback CLI flow the
	// member's browser cannot reach).
	PasteKindURL = "url"
)

// LoginStaleAfter is how long an active request may go without progress
// before StartLogin abandons it and creates a fresh one (covers dead workers).
const LoginStaleAfter = 15 * time.Minute

// Login targets: which machine should execute the CLI sign-in.
const (
	// LoginTargetWorkspace runs on any of the workspace's shared workers
	// (admin-managed hosts). Historical default.
	LoginTargetWorkspace = "workspace"
	// LoginTargetUser runs only on the requesting user's personal runner —
	// the credential lands on their own machine.
	LoginTargetUser = "user"
)

// Login purposes: what a brokered sign-in is for.
const (
	// LoginPurposeSignIn signs the CLI in on the runner, which keeps the
	// credentials it gets; OpenV never sees them. The zero value.
	LoginPurposeSignIn = ""
	// LoginPurposeToken has a cloud runner mint the member a long-lived
	// Claude subscription token (`claude setup-token`) and hand it to the
	// server, which seals it as the member's saved token. Only Claude Code,
	// only a user-targeted request, and only a runner that says it can
	// capture the token (a headless one) claims it.
	LoginPurposeToken = "token"
)

// LoginRequest brokers a CLI provider sign-in between the UI and the host
// worker. The OAuth code passes through briefly; for an ordinary sign-in the
// CLI stores the actual credentials on the host — OpenV never sees them. A
// token-purpose request is the exception: its token reaches the server once,
// to be sealed as the member's saved token, and is never stored here.
type LoginRequest struct {
	ID       string `json:"id"`
	OrgID    string `json:"org_id"`
	Provider string `json:"provider"`
	// Target is where the sign-in executes: "workspace" (any shared worker)
	// or "user" (only the requester's personal runner).
	Target string `json:"target"`
	// Purpose is "" for a sign-in, or "token" when the runner mints the
	// member a saved subscription token instead.
	Purpose string `json:"purpose,omitempty"`
	Status  string `json:"status"`
	AuthURL string `json:"auth_url"`
	Code    string `json:"code,omitempty"`
	Detail  string `json:"detail"`
	// PasteKind is what the member must paste back ("code" or "url"), as
	// reported by the worker. Empty until a flow asks for a paste — and
	// always empty from a worker older than this field.
	PasteKind   string    `json:"paste_kind,omitempty"`
	RequestedBy *string   `json:"requested_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Active reports whether the request is still in flight.
func (l *LoginRequest) Active() bool {
	return activeLoginStatuses[l.Status]
}

// Sanitized returns a copy safe for the requesting user's UI (no code echo).
func (l *LoginRequest) Sanitized() *LoginRequest {
	copied := *l
	copied.Code = ""
	return &copied
}

// LoginRepository defines persistence for login requests.
// Find methods return (nil, nil) when no row matches.
type LoginRepository interface {
	SaveLogin(l *LoginRequest) error
	UpdateLogin(l *LoginRequest) error
	FindLoginByID(id string) (*LoginRequest, error)
	// FindActiveLogin returns the most recent in-flight request for a
	// provider and purpose, scoped by target: user-targeted requests match
	// per requester (userID), workspace-targeted requests match org-wide.
	FindActiveLogin(orgID, provider, target, purpose string, userID *string) (*LoginRequest, error)
	// ClaimPendingLogin atomically claims the org's oldest pending request a
	// worker may execute: workspace-targeted requests for any worker, plus
	// user-targeted requests when workerUserID matches the requester
	// (personal runners). Token-purpose requests are claimed only when
	// tokenLogins is set: a runner that cannot capture the token would run
	// an ordinary sign-in in its place.
	ClaimPendingLogin(orgID, workerUserID string, tokenLogins bool) (*LoginRequest, error)
}

// LoginService defines the login broker logic.
type LoginService interface {
	// StartLogin creates (or returns the existing active) login request for
	// a CLI provider in an org. target picks the executing machine:
	// "workspace" (shared workers) or "user" (the requester's own runner).
	StartLogin(orgID, provider, target string, requestedBy *string) (*LoginRequest, error)
	// StartTokenLogin creates (or returns the existing active) request for
	// the member's own runner to mint them a Claude subscription token.
	StartTokenLogin(orgID, requestedBy string) (*LoginRequest, error)
	// CompleteTokenLogin finishes a token-purpose request once its token is
	// saved. A request that is not an active token request is refused.
	CompleteTokenLogin(id string) (*LoginRequest, error)
	Get(id string) (*LoginRequest, error)
	// SubmitCode records the user's pasted authorization code.
	SubmitCode(id, code string) (*LoginRequest, error)
	Cancel(id string) (*LoginRequest, error)
	// Claim hands the org's oldest matching pending request to a worker (nil
	// when none). workerUserID is the personal runner's owner ("" for
	// workspace workers); tokenLogins is whether the worker can mint a
	// token (see ClaimPendingLogin).
	Claim(orgID, workerUserID string, tokenLogins bool) (*LoginRequest, error)
	// Progress records worker-side state updates. pasteKind is "code" or
	// "url" when the worker is asking for a paste, and "" otherwise (which
	// leaves any kind already recorded in place). A cancelled or completed
	// request is returned as it is: a late update changes neither.
	Progress(id, status, authURL, detail, pasteKind string) (*LoginRequest, error)
}

// DefaultLoginService implements LoginService.
type DefaultLoginService struct {
	repo LoginRepository
}

// NewLoginService creates the login broker service.
func NewLoginService(repo LoginRepository) *DefaultLoginService {
	return &DefaultLoginService{repo: repo}
}

func cliProvider(name string) bool {
	switch name {
	case ProviderClaudeCode, ProviderCodexCLI, ProviderGeminiCLI:
		return true
	}
	return false
}

// StartLogin creates a login request, reusing a fresh active one if present.
func (s *DefaultLoginService) StartLogin(orgID, provider, target string, requestedBy *string) (*LoginRequest, error) {
	return s.start(orgID, provider, target, LoginPurposeSignIn, requestedBy)
}

// StartTokenLogin creates a token-purpose request for the member's own
// runner, reusing a fresh active one if present.
func (s *DefaultLoginService) StartTokenLogin(orgID, requestedBy string) (*LoginRequest, error) {
	if requestedBy == "" {
		return nil, errors.New("a token request needs a requesting user")
	}
	return s.start(orgID, ProviderClaudeCode, LoginTargetUser, LoginPurposeToken, &requestedBy)
}

func (s *DefaultLoginService) start(orgID, provider, target, purpose string, requestedBy *string) (*LoginRequest, error) {
	if orgID == "" {
		return nil, errors.New("organization id is required")
	}
	if !cliProvider(provider) {
		return nil, fmt.Errorf("provider %q does not use CLI subscription login", provider)
	}
	if target == "" {
		target = LoginTargetWorkspace
	}
	if target != LoginTargetWorkspace && target != LoginTargetUser {
		return nil, fmt.Errorf("invalid login target %q", target)
	}
	if target == LoginTargetUser && requestedBy == nil {
		return nil, errors.New("a user-targeted login needs a requesting user")
	}

	existing, err := s.repo.FindActiveLogin(orgID, provider, target, purpose, requestedBy)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		// updated_at is a TIMESTAMP read back as UTC. Every write here stamps
		// it in UTC, as the claim's NOW() does: a local stamp read back five
		// hours old west of UTC, so a fresh request was abandoned and
		// replaced, and east of UTC a stale one was handed out again for two
		// hours more (#379 bug 162).
		if time.Since(existing.UpdatedAt) < LoginStaleAfter {
			return existing, nil
		}
		existing.Status = LoginFailed
		existing.Detail = "abandoned: no progress from worker"
		existing.UpdatedAt = time.Now().UTC()
		if err := s.repo.UpdateLogin(existing); err != nil {
			return nil, err
		}
	}

	detail := "Waiting for the worker (agentd) to pick this up. Make sure agentd is running on the host."
	if target == LoginTargetUser {
		detail = "Waiting for your personal runner to pick this up. Make sure your Agent Connector (or agentd) is running."
	}
	if purpose == LoginPurposeToken {
		detail = "Waiting for your cloud runner to pick this up. Start one under Cloud runner if none is running."
	}
	now := time.Now().UTC() // TIMESTAMP columns hold UTC wall clocks (#379 bug 162)
	login := &LoginRequest{
		ID:          uuid.New().String(),
		OrgID:       orgID,
		Provider:    provider,
		Target:      target,
		Purpose:     purpose,
		Status:      LoginPending,
		Detail:      detail,
		RequestedBy: requestedBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.SaveLogin(login); err != nil {
		return nil, err
	}
	return login, nil
}

// Get returns a login request by id.
func (s *DefaultLoginService) Get(id string) (*LoginRequest, error) {
	login, err := s.repo.FindLoginByID(id)
	if err != nil {
		return nil, err
	}
	if login == nil {
		return nil, errors.New("login request not found")
	}
	return login, nil
}

// SubmitCode stores the pasted authorization code for the worker to consume.
func (s *DefaultLoginService) SubmitCode(id, code string) (*LoginRequest, error) {
	login, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if !login.Active() {
		return nil, fmt.Errorf("login request is already %s", login.Status)
	}
	login.Code = code
	login.UpdatedAt = time.Now().UTC() // a TIMESTAMP holding a UTC wall clock (#379 bug 162)
	if err := s.repo.UpdateLogin(login); err != nil {
		return nil, err
	}
	return login, nil
}

// Cancel marks a login request cancelled.
func (s *DefaultLoginService) Cancel(id string) (*LoginRequest, error) {
	login, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if login.Active() {
		login.Status = LoginCancelled
		login.Detail = "Cancelled by user."
		login.UpdatedAt = time.Now().UTC()
		if err := s.repo.UpdateLogin(login); err != nil {
			return nil, err
		}
	}
	return login, nil
}

// Claim hands the org's oldest matching pending request to a worker.
func (s *DefaultLoginService) Claim(orgID, workerUserID string, tokenLogins bool) (*LoginRequest, error) {
	return s.repo.ClaimPendingLogin(orgID, workerUserID, tokenLogins)
}

// ErrNotTokenLogin refuses a token for a request that is not an active
// token-purpose one.
var ErrNotTokenLogin = errors.New("this sign-in request does not mint a token")

// CompleteTokenLogin marks an active token-purpose request completed.
func (s *DefaultLoginService) CompleteTokenLogin(id string) (*LoginRequest, error) {
	login, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if login.Purpose != LoginPurposeToken || !login.Active() {
		return nil, ErrNotTokenLogin
	}
	login.Status = LoginCompleted
	login.Detail = "Token saved. Your runners now start Claude signed in."
	login.Code = ""
	login.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateLogin(login); err != nil {
		return nil, err
	}
	return login, nil
}

// Progress applies a worker-side status update.
func (s *DefaultLoginService) Progress(id, status, authURL, detail, pasteKind string) (*LoginRequest, error) {
	login, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	// A user cancellation wins over late worker updates, and a completed
	// sign-in stays completed: the CLI is signed in, whatever is reported
	// after that.
	if login.Status == LoginCancelled || login.Status == LoginCompleted {
		return login, nil
	}
	switch status {
	case LoginClaimed, LoginURLReady, LoginAwaitingCode, LoginCompleted, LoginFailed:
	default:
		return nil, fmt.Errorf("invalid login status %q", status)
	}
	login.Status = status
	if authURL != "" {
		login.AuthURL = authURL
	}
	if detail != "" {
		login.Detail = detail
	}
	if pasteKind != "" {
		login.PasteKind = pasteKind
	}
	login.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateLogin(login); err != nil {
		return nil, err
	}
	return login, nil
}
