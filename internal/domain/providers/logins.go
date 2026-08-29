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

// LoginRequest brokers a CLI provider sign-in between the UI and the host
// worker. The OAuth code passes through briefly; the CLI stores the actual
// credentials on the host — OpenV never sees tokens.
type LoginRequest struct {
	ID       string `json:"id"`
	OrgID    string `json:"org_id"`
	Provider string `json:"provider"`
	// Target is where the sign-in executes: "workspace" (any shared worker)
	// or "user" (only the requester's personal runner).
	Target      string    `json:"target"`
	Status      string    `json:"status"`
	AuthURL     string    `json:"auth_url"`
	Code        string    `json:"code,omitempty"`
	Detail      string    `json:"detail"`
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
	// provider, scoped by target: user-targeted requests match per requester
	// (userID), workspace-targeted requests match org-wide.
	FindActiveLogin(orgID, provider, target string, userID *string) (*LoginRequest, error)
	// ClaimPendingLogin atomically claims the org's oldest pending request a
	// worker may execute: workspace-targeted requests for any worker, plus
	// user-targeted requests when workerUserID matches the requester
	// (personal runners).
	ClaimPendingLogin(orgID, workerUserID string) (*LoginRequest, error)
}

// LoginService defines the login broker logic.
type LoginService interface {
	// StartLogin creates (or returns the existing active) login request for
	// a CLI provider in an org. target picks the executing machine:
	// "workspace" (shared workers) or "user" (the requester's own runner).
	StartLogin(orgID, provider, target string, requestedBy *string) (*LoginRequest, error)
	Get(id string) (*LoginRequest, error)
	// SubmitCode records the user's pasted authorization code.
	SubmitCode(id, code string) (*LoginRequest, error)
	Cancel(id string) (*LoginRequest, error)
	// Claim hands the org's oldest matching pending request to a worker (nil
	// when none). workerUserID is the personal runner's owner ("" for
	// workspace workers).
	Claim(orgID, workerUserID string) (*LoginRequest, error)
	// Progress records worker-side state updates.
	Progress(id, status, authURL, detail string) (*LoginRequest, error)
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

	existing, err := s.repo.FindActiveLogin(orgID, provider, target, requestedBy)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if time.Since(existing.UpdatedAt) < LoginStaleAfter {
			return existing, nil
		}
		existing.Status = LoginFailed
		existing.Detail = "abandoned: no progress from worker"
		existing.UpdatedAt = time.Now()
		if err := s.repo.UpdateLogin(existing); err != nil {
			return nil, err
		}
	}

	detail := "Waiting for the worker (agentd) to pick this up. Make sure agentd is running on the host."
	if target == LoginTargetUser {
		detail = "Waiting for your personal runner to pick this up. Make sure your Agent Connector (or agentd) is running."
	}
	now := time.Now()
	login := &LoginRequest{
		ID:          uuid.New().String(),
		OrgID:       orgID,
		Provider:    provider,
		Target:      target,
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
	login.UpdatedAt = time.Now()
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
		login.UpdatedAt = time.Now()
		if err := s.repo.UpdateLogin(login); err != nil {
			return nil, err
		}
	}
	return login, nil
}

// Claim hands the org's oldest matching pending request to a worker.
func (s *DefaultLoginService) Claim(orgID, workerUserID string) (*LoginRequest, error) {
	return s.repo.ClaimPendingLogin(orgID, workerUserID)
}

// Progress applies a worker-side status update.
func (s *DefaultLoginService) Progress(id, status, authURL, detail string) (*LoginRequest, error) {
	login, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	// A user cancellation wins over late worker updates.
	if login.Status == LoginCancelled {
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
	login.UpdatedAt = time.Now()
	if err := s.repo.UpdateLogin(login); err != nil {
		return nil, err
	}
	return login, nil
}
