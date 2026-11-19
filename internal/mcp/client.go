package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
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
	return c.do(req)
}

// do sends req and answers the response body, trimmed, and its status; a
// status of 400 or more is also an error carrying the body.
func (c *Client) do(req *http.Request) (string, int, error) {
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

// slowTimeout bounds a call the server answers only after a provider has
// done its work, such as generating an image (generate_project_cover).
const slowTimeout = 150 * time.Second

// requestSlow is request with slowTimeout in place of the client's own.
func (c *Client) requestSlow(method, path string, body interface{}) (string, int, error) {
	slow := *c
	h := *c.http
	h.Timeout = slowTimeout
	slow.http = &h
	return slow.request(method, path, nil, body)
}

// uploadBoundary is the multipart boundary uploadFile writes: fixed, so a
// recorded request reads the same every time (S7).
const uploadBoundary = "openv-mcp-upload"

// uploadFile POSTs one file as the multipart field "file", the shape the
// API's upload handlers read, and returns the response as request does.
func (c *Client) uploadFile(path, filename, contentType string, data []byte) (string, int, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.SetBoundary(uploadBoundary); err != nil {
		return "", 0, err
	}
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	hdr.Set("Content-Type", contentType)
	part, err := mw.CreatePart(hdr)
	if err != nil {
		return "", 0, err
	}
	if _, err := part.Write(data); err != nil {
		return "", 0, err
	}
	if err := mw.Close(); err != nil {
		return "", 0, err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, &buf)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return c.do(req)
}
