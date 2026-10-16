package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client calls the OpenV API as the agent (run-token auth).
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient creates an API client for the MCP tools.
func NewClient(baseURL, runToken string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   runToken,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// NewClientWithTransport creates a client whose calls go through rt rather
// than the network: the remote MCP endpoint (REQ-44) answers a tool call by
// sending the tool's API request through the server's own router, as the
// member, so every authorization rule applies to it unchanged. token may be
// empty when rt authenticates the call itself.
func NewClientWithTransport(baseURL, token string, rt http.RoundTripper) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second, Transport: rt},
	}
}

// request performs an API call and returns the response body and status.
func (c *Client) request(method, path string, query url.Values, body interface{}) (string, int, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return "", 0, err
		}
		reader = bytes.NewReader(buf)
	}
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(method, u, reader)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}
	text := strings.TrimSpace(string(data))
	if resp.StatusCode >= 400 {
		return text, resp.StatusCode, fmt.Errorf("API %d: %s", resp.StatusCode, text)
	}
	return text, resp.StatusCode, nil
}
