package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/repoconns"
)

// Client is the worker's typed HTTP client for the OpenV API.
type Client struct {
	baseURL   string
	workerKey string
	http      *http.Client
	logHTTP   *http.Client
}

// NewClient creates a worker API client.
func NewClient(baseURL, workerKey string) *Client {
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		workerKey: workerKey,
		http:      &http.Client{Timeout: 30 * time.Second},
		logHTTP:   &http.Client{Timeout: 15 * time.Second},
	}
}

// RunAuth is the credential mode the API resolved for a claimed run.
type RunAuth struct {
	// Mode is "user-account" (use the host's CLI sign-in, the default) or
	// "api-key" (inject the key named by APIKeyEnv from the host env).
	Mode      string `json:"mode"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
}

// ClaimResponse is the payload returned when a run is claimed.
type ClaimResponse struct {
	Run      *agentruns.Run `json:"run"`
	Agent    *agents.Agent  `json:"agent"`
	RunToken string         `json:"run_token"`
	Auth     *RunAuth       `json:"auth,omitempty"`
}

func (c *Client) do(client *http.Client, method, path string, body interface{}) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.workerKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return client.Do(req)
}

func httpError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("api returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
}

// transientRetries bounds how many extra attempts a transient failure (a
// network error or a 5xx response) gets before the call gives up. Applied to
// the calls whose loss corrupts run state — Start, PushLogs, and above all
// Finish, where a dropped terminal report would strand the run until the
// reaper.
const transientRetries = 3

// retryBackoff is the pause before the n-th retry (n starts at 1). Short and
// linear: the goal is to ride out a brief blip or an API restart, not to wait
// out a real outage.
func retryBackoff(n int) time.Duration { return time.Duration(n) * 250 * time.Millisecond }

// doWithRetry issues the request and retries it on transient failures. A
// network error or a 5xx is retried (the body is drained and closed first); a
// non-5xx response — including 4xx like a 409 already-finished conflict — is
// returned to the caller as-is, retries or not. On exhaustion the last error
// is returned so the caller reports a real failure rather than a nil response.
func (c *Client) doWithRetry(client *http.Client, method, path string, body interface{}) (*http.Response, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			time.Sleep(retryBackoff(attempt))
		}
		resp, err := c.do(client, method, path, body)
		if err != nil {
			lastErr = err
		} else if resp.StatusCode >= 500 {
			lastErr = httpError(resp) // reads the body...
			resp.Body.Close()         // ...so close it before the next attempt
		} else {
			return resp, nil
		}
		if attempt >= transientRetries {
			return nil, lastErr
		}
	}
}

// Claim asks the queue for a run. Returns (nil, nil) when the queue is empty.
func (c *Client) Claim(workerID string, providers []string, minPriority int, hosted bool) (*ClaimResponse, error) {
	resp, err := c.do(c.http, "POST", "/api/v1/agent-runs/claim", map[string]interface{}{
		"worker_id":    workerID,
		"providers":    providers,
		"min_priority": minPriority,
		"hosted":       hosted,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpError(resp)
	}
	var claim ClaimResponse
	if err := json.NewDecoder(resp.Body).Decode(&claim); err != nil {
		return nil, err
	}
	return &claim, nil
}

// Start transitions a claimed run to running.
func (c *Client) Start(runID string) error {
	resp, err := c.doWithRetry(c.http, "POST", "/api/v1/agent-runs/"+runID+"/start", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return httpError(resp)
	}
	return nil
}

// PushLogs appends a log batch (also serves as heartbeat) and reports back
// whether cancellation was requested.
func (c *Client) PushLogs(runID string, entries []agentruns.LogEntry) (bool, string, error) {
	if entries == nil {
		entries = []agentruns.LogEntry{}
	}
	resp, err := c.doWithRetry(c.logHTTP, "POST", "/api/v1/agent-runs/"+runID+"/logs", entries)
	if err != nil {
		return false, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, "", httpError(resp)
	}
	var out struct {
		CancelRequested bool   `json:"cancel_requested"`
		Status          string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, "", err
	}
	return out.CancelRequested, out.Status, nil
}

// Finish reports a run's terminal state. Retried on transient failures so a
// blip talking to the API doesn't drop the run's terminal report.
func (c *Client) Finish(runID string, req agentruns.FinishRequest) error {
	resp, err := c.doWithRetry(c.http, "POST", "/api/v1/agent-runs/"+runID+"/finish", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return httpError(resp)
	}
	return nil
}

// Release hands a claimed/running run back to the queue (best effort) so it
// can be reclaimed — used when the worker is shutting down mid-run rather than
// failing the run.
func (c *Client) Release(runID, workerID string) error {
	resp, err := c.do(c.http, "POST", "/api/v1/agent-runs/"+runID+"/release", map[string]string{
		"worker_id": workerID,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return httpError(resp)
	}
	return nil
}

// ReportDetection uploads provider availability results.
func (c *Client) ReportDetection(report map[string]map[string]interface{}) error {
	resp, err := c.do(c.http, "POST", "/api/v1/provider-settings/detect", report)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return httpError(resp)
	}
	return nil
}

// ClaimLogin asks for a pending provider sign-in request (nil when none).
func (c *Client) ClaimLogin() (*providers.LoginRequest, error) {
	resp, err := c.do(c.http, "POST", "/api/v1/provider-logins/claim", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpError(resp)
	}
	var login providers.LoginRequest
	if err := json.NewDecoder(resp.Body).Decode(&login); err != nil {
		return nil, err
	}
	return &login, nil
}

// LoginProgress reports sign-in progress back to the API.
func (c *Client) LoginProgress(id, status, authURL, detail string) error {
	resp, err := c.do(c.http, "POST", "/api/v1/provider-logins/"+id+"/progress", map[string]string{
		"status":   status,
		"auth_url": authURL,
		"detail":   detail,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return httpError(resp)
	}
	return nil
}

// GetLoginFull fetches a sign-in request including any pasted code.
func (c *Client) GetLoginFull(id string) (*providers.LoginRequest, error) {
	resp, err := c.do(c.http, "GET", "/api/v1/provider-logins/"+id+"/full", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpError(resp)
	}
	var login providers.LoginRequest
	if err := json.NewDecoder(resp.Body).Decode(&login); err != nil {
		return nil, err
	}
	return &login, nil
}

// ListRepoConnections returns a project's repo connections.
func (c *Client) ListRepoConnections(projectID string) ([]*repoconns.RepoConnection, error) {
	resp, err := c.do(c.http, "GET", "/api/v1/projects/"+projectID+"/repo-connections", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpError(resp)
	}
	var conns []*repoconns.RepoConnection
	if err := json.NewDecoder(resp.Body).Decode(&conns); err != nil {
		return nil, err
	}
	return conns, nil
}

// --- Transient runner pool ---
//
// These calls are made with the deployment's pool key, before any workspace
// is in the picture. Once a lease arrives the node builds a second client
// around the lease's own session key and uses that for everything else.

// PoolNode is a registered pool node as the API sees it.
type PoolNode struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Pool   string `json:"pool"`
	Status string `json:"status"`
}

// PoolAssignment is the lease a node has been handed. WorkerKey is present
// only on the heartbeat that picks the lease up.
type PoolAssignment struct {
	SessionID string    `json:"session_id"`
	OrgID     string    `json:"org_id"`
	UserID    string    `json:"user_id"`
	UserName  string    `json:"user_name"`
	WorkerKey string    `json:"worker_key"`
	ExpiresAt time.Time `json:"expires_at"`
}

// RegisterPoolNode announces this process as available to lease.
func (c *Client) RegisterPoolNode(name, pool string, providers []string) (*PoolNode, error) {
	resp, err := c.doWithRetry(c.http, "POST", "/api/v1/runner-pool/nodes", map[string]interface{}{
		"name":      name,
		"pool":      pool,
		"providers": providers,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, httpError(resp)
	}
	node := new(PoolNode)
	if err := json.NewDecoder(resp.Body).Decode(node); err != nil {
		return nil, err
	}
	return node, nil
}

// ErrPoolNodeUnknown means the API has no record of this node, so it must
// register again (a reset database, or a purged pool).
var ErrPoolNodeUnknown = errors.New("pool node is not registered")

// PoolHeartbeat records a beat and returns the node's lease, or nil.
func (c *Client) PoolHeartbeat(nodeID string) (*PoolAssignment, error) {
	resp, err := c.do(c.http, "POST", "/api/v1/runner-pool/nodes/"+nodeID+"/heartbeat", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrPoolNodeUnknown
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpError(resp)
	}
	var body struct {
		Assignment *PoolAssignment `json:"assignment"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Assignment, nil
}

// ReleasePoolNode reports that the node has wiped a lease's state.
func (c *Client) ReleasePoolNode(nodeID, sessionID string) error {
	resp, err := c.doWithRetry(c.http, "POST", "/api/v1/runner-pool/nodes/"+nodeID+"/release", map[string]interface{}{
		"session_id": sessionID,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return httpError(resp)
	}
	return nil
}
