package runner

import (
	"bytes"
	"encoding/json"
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
	resp, err := c.do(c.http, "POST", "/api/v1/agent-runs/"+runID+"/start", nil)
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
	resp, err := c.do(c.logHTTP, "POST", "/api/v1/agent-runs/"+runID+"/logs", entries)
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

// Finish reports a run's terminal state.
func (c *Client) Finish(runID string, req agentruns.FinishRequest) error {
	resp, err := c.do(c.http, "POST", "/api/v1/agent-runs/"+runID+"/finish", req)
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
