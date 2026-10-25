// Package help serves the user manual's context help from OpenD, the docs
// server that publishes manual/ (docs/plans/opend-help.md).
//
// Two ways to read it. With only the docs site's address it reads the
// site's public view, which a public OpenD site answers without a token:
// every workspace reads the manual anonymous visitors see, for the version
// it asks for, and nothing has to be shared with the docs site. With the
// site's embed secret as well, it reads per profile, as below, so the
// manual can also carry content gated by plan.
//
// OpenV's API calls OpenD's embed JSON help API server to server and hands
// the browser sanitized topics, so the app's CSP never admits the docs
// origin and the embed secret never leaves the server. OpenD decides what
// a reader may see from their organization's entitlements, and what a
// workspace may see here is decided by its profile alone: the docs line of
// the release it is on, and its plan family. Each profile is one OpenD
// organization with one service reader, which this package creates through
// OpenD's entitlement-sync API the first time the profile asks for help,
// and signs embed tokens for.
package help

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// App is the help-contexts app name the manual's topics declare
// (manual/help-contexts.yaml).
const App = "openv-web"

// ErrNotFound is returned when the docs have nothing for a request: no
// topic for the context, no such topic, or no docs line published at all.
var ErrNotFound = errors.New("help: not found")

// ErrUnavailable is returned when OpenD cannot be reached or refuses the
// request; the app then falls back to the help it ships.
var ErrUnavailable = errors.New("help: docs unavailable")

// Config is where OpenD is and how to sign in to it.
type Config struct {
	// URL is the docs site's PUBLIC_URL, without a trailing slash. It is
	// also the audience of every embed token.
	URL string
	// Secret is the docs site's OPEND_EMBED_SECRET. Without it the service
	// reads the site's public view.
	Secret string
	// AdminToken is the docs site's OPEND_ADMIN_TOKEN, used only to upsert
	// a profile's organization and reader.
	AdminToken string
	// ReaderDomain is the email domain of the service readers.
	ReaderDomain string
}

// Profile is what decides which docs a workspace reads.
type Profile struct {
	Line string // docs line, major.minor: "0.23"
	Plan string // plan family: free, lite, business, enterprise, self-host
}

func (p Profile) slug() string {
	return "openv-" + strings.ReplaceAll(p.Line, ".", "-") + "-" + p.Plan
}

// Line is the docs line of a release version: "0.23.1" -> "0.23".
func Line(version string) string {
	parts := strings.SplitN(strings.TrimPrefix(version, "v"), ".", 3)
	if len(parts) < 2 {
		return ""
	}
	for _, p := range parts[:2] {
		if _, err := strconv.Atoi(p); err != nil {
			return ""
		}
	}
	return parts[0] + "." + parts[1]
}

// previousLine is the line before line within its major ("0.23" -> "0.22"),
// or "" for the first.
func previousLine(line string) string {
	major, minor, ok := strings.Cut(line, ".")
	n, err := strconv.Atoi(minor)
	if !ok || err != nil || n == 0 {
		return ""
	}
	return major + "." + strconv.Itoa(n-1)
}

// PlanFamily maps an OpenV plan to the manual's `product` values
// (manual/opend.config.yaml).
func PlanFamily(plan string) string {
	switch plan {
	case "business_lite":
		return "lite"
	case "business", "team", "open_source":
		return "business"
	case "enterprise":
		return "enterprise"
	case "self_host":
		return "self-host"
	default:
		return "free"
	}
}

// Related is a topic near another in the manual's navigation.
type Related struct {
	TopicID string `json:"topic_id"`
	Title   string `json:"title"`
}

// Topic is one topic of the manual, as the drawer shows it.
type Topic struct {
	// Key and Via say how a context resolved: the key itself, a parent, or
	// the app's default. Empty for a topic asked for by id.
	Key       string    `json:"key,omitempty"`
	Via       string    `json:"via,omitempty"`
	Fragment  string    `json:"fragment,omitempty"`
	TopicID   string    `json:"topic_id"`
	Title     string    `json:"title"`
	Shortdesc string    `json:"shortdesc"`
	HTML      string    `json:"html"`
	Related   []Related `json:"related"`
	FullURL   string    `json:"full_url"`
	// Line is the docs line served, which is older than the one asked for
	// while a new line's docs are still building.
	Line string `json:"line"`
}

// Hit is one search result.
type Hit struct {
	TopicID string `json:"topic_id"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// Results is a search's answer.
type Results struct {
	Query string `json:"query"`
	Hits  []Hit  `json:"hits"`
	Line  string `json:"line"`
}

// Service fetches help from OpenD for a profile.
type Service struct {
	cfg    Config
	client *http.Client
	now    func() time.Time

	mu      sync.Mutex
	ensured map[string]bool       // profile slugs upserted on OpenD
	lines   map[string]lineChoice // requested line -> line served
	cache   map[string]cacheEntry // request -> answer
}

type lineChoice struct {
	line    string
	expires time.Time
}

type cacheEntry struct {
	body    []byte
	expires time.Time
}

const (
	cacheTTL    = 5 * time.Minute
	cacheMax    = 2000
	tokenTTL    = 5 * time.Minute
	fallbackMax = 12 // older lines tried when a line is not published
	responseMax = 4 << 20
)

// NewService returns a service for cfg, or nil when cfg names no docs site:
// help is then off and the app keeps the help it ships.
func NewService(cfg Config, client *http.Client) *Service {
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	if cfg.URL == "" {
		return nil
	}
	if cfg.ReaderDomain == "" {
		cfg.ReaderDomain = "help.openv.app"
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Service{
		cfg:     cfg,
		client:  client,
		now:     time.Now,
		ensured: map[string]bool{},
		lines:   map[string]lineChoice{},
		cache:   map[string]cacheEntry{},
	}
}

// Context is the topic for a context key, or for an app path when key is
// empty.
func (s *Service) Context(ctx context.Context, p Profile, key, path string) (*Topic, error) {
	q := url.Values{"app": {App}}
	if key != "" {
		q.Set("key", key)
	} else {
		q.Set("path", path)
	}
	var t Topic
	line, err := s.get(ctx, p, "/api/v1/help/context?"+q.Encode(), &t)
	if err != nil {
		return nil, err
	}
	return s.finish(&t, line), nil
}

// TopicByID is one topic by its page id, such as "topics/vv/test-runs".
func (s *Service) TopicByID(ctx context.Context, p Profile, id string) (*Topic, error) {
	if !validTopicID(id) {
		return nil, ErrNotFound
	}
	var t Topic
	line, err := s.get(ctx, p, "/api/v1/help/topics/"+id, &t)
	if err != nil {
		return nil, err
	}
	return s.finish(&t, line), nil
}

// Search searches the topics the profile may read.
func (s *Service) Search(ctx context.Context, p Profile, query string) (*Results, error) {
	var r struct {
		Query string `json:"query"`
		Hits  []struct {
			TopicID string `json:"topicId"`
			Title   string `json:"title"`
			Snippet string `json:"snippet"`
		} `json:"hits"`
	}
	line, err := s.get(ctx, p, "/api/v1/help/search?"+url.Values{"q": {query}}.Encode(), &r)
	if err != nil {
		return nil, err
	}
	out := &Results{Query: r.Query, Hits: []Hit{}, Line: line}
	for _, h := range r.Hits {
		out.Hits = append(out.Hits, Hit{TopicID: h.TopicID, Title: h.Title, Snippet: h.Snippet})
	}
	return out, nil
}

func validTopicID(id string) bool {
	if id == "" || len(id) > 300 || strings.Contains(id, "..") || strings.HasPrefix(id, "/") {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/-_.", r)) {
			return false
		}
	}
	return true
}

// openDTopic is the JSON OpenD sends for a topic.
type openDTopic struct {
	Key       string  `json:"key"`
	Via       string  `json:"via"`
	Fragment  *string `json:"fragment"`
	TopicID   string  `json:"topicId"`
	Title     string  `json:"title"`
	Shortdesc string  `json:"shortdesc"`
	HTML      string  `json:"html"`
	Related   []struct {
		TopicID string `json:"topicId"`
		Title   string `json:"title"`
	} `json:"related"`
	FullURL string `json:"fullUrl"`
}

func (t *Topic) UnmarshalJSON(b []byte) error {
	var o openDTopic
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	*t = Topic{Key: o.Key, Via: o.Via, TopicID: o.TopicID, Title: o.Title, Shortdesc: o.Shortdesc,
		HTML: o.HTML, FullURL: o.FullURL, Related: []Related{}}
	if o.Fragment != nil {
		t.Fragment = *o.Fragment
	}
	for _, r := range o.Related {
		t.Related = append(t.Related, Related{TopicID: r.TopicID, Title: r.Title})
	}
	return nil
}

// finish sanitizes a topic for the app and records the line it came from.
func (s *Service) finish(t *Topic, line string) *Topic {
	t.HTML = Sanitize(t.HTML, s.cfg.URL)
	if !strings.HasPrefix(t.FullURL, s.cfg.URL+"/") {
		t.FullURL = ""
	}
	t.Line = line
	return t
}

// get fetches path for profile p into out, falling back to older lines while
// the profile's own is not published, and returns the line that answered.
func (s *Service) get(ctx context.Context, p Profile, path string, out any) (string, error) {
	if s.cfg.Secret == "" {
		return s.getPublic(ctx, p.Line, path, out)
	}
	line := s.servedLine(p.Line)
	for i := 0; line != "" && i <= fallbackMax; i++ {
		body, status, err := s.fetch(ctx, Profile{Line: line, Plan: p.Plan}, path)
		switch {
		case err != nil:
			return "", err
		case status == http.StatusOK:
			if line != p.Line {
				s.rememberLine(p.Line, line)
			}
			if err := json.Unmarshal(body, out); err != nil {
				return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
			}
			return line, nil
		case status == http.StatusNotFound && !lineMissing(body):
			return "", ErrNotFound
		case status == http.StatusNotFound:
			line = previousLine(line)
		default:
			return "", fmt.Errorf("%w: %s answered %d", ErrUnavailable, path, status)
		}
	}
	return "", ErrNotFound
}

// getPublic fetches path from the site's public view at version line. The
// site answers a version it does not offer with its default, and names the
// line it answered from in OpenD-Release-Line.
func (s *Service) getPublic(ctx context.Context, line, path string, out any) (string, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	path += sep + url.Values{"version": {line}}.Encode()
	key := "public " + path
	body, served, ok := s.cachedPublic(key)
	if !ok {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.URL+path, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := s.client.Do(req)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		defer resp.Body.Close()
		body, err = io.ReadAll(io.LimitReader(resp.Body, responseMax))
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		switch {
		case resp.StatusCode == http.StatusNotFound:
			return "", ErrNotFound
		case resp.StatusCode != http.StatusOK:
			return "", fmt.Errorf("%w: %s answered %d", ErrUnavailable, path, resp.StatusCode)
		}
		served = resp.Header.Get("OpenD-Release-Line")
		if served == "" {
			served = line
		}
		s.store(key, append([]byte(served+"\n"), body...))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return served, nil
}

// cachedPublic is a cached public answer: the line it came from, then its body.
func (s *Service) cachedPublic(key string) (body []byte, line string, ok bool) {
	raw, ok := s.cached(key)
	if !ok {
		return nil, "", false
	}
	first, rest, found := strings.Cut(string(raw), "\n")
	if !found {
		return nil, "", false
	}
	return []byte(rest), first, true
}

// lineMissing reports whether a 404 from OpenD means the reader's release
// line has nothing published, rather than that this one thing is missing.
func lineMissing(body []byte) bool {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	return e.Error != "no help for this context" && e.Error != "not found"
}

func (s *Service) servedLine(line string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.lines[line]; ok && s.now().Before(c.expires) {
		return c.line
	}
	return line
}

func (s *Service) rememberLine(asked, served string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines[asked] = lineChoice{line: served, expires: s.now().Add(cacheTTL)}
}

// fetch answers path for p, from the cache or from OpenD. A refusal of the
// profile's reader (403) upserts the profile and tries once more.
func (s *Service) fetch(ctx context.Context, p Profile, path string) ([]byte, int, error) {
	key := p.slug() + " " + path
	if body, ok := s.cached(key); ok {
		return body, http.StatusOK, nil
	}
	if err := s.ensure(ctx, p, false); err != nil {
		return nil, 0, err
	}
	body, status, err := s.call(ctx, p, path)
	if err == nil && status == http.StatusForbidden {
		if err := s.ensure(ctx, p, true); err != nil {
			return nil, 0, err
		}
		body, status, err = s.call(ctx, p, path)
	}
	if err == nil && status == http.StatusOK {
		s.store(key, body)
	}
	return body, status, err
}

func (s *Service) call(ctx context.Context, p Profile, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.URL+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token(p))
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, responseMax))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return body, resp.StatusCode, nil
}

// ensure upserts the profile's organization and service reader on OpenD,
// once per process unless force.
func (s *Service) ensure(ctx context.Context, p Profile, force bool) error {
	slug := p.slug()
	s.mu.Lock()
	done := s.ensured[slug]
	s.mu.Unlock()
	if done && !force {
		return nil
	}
	if s.cfg.AdminToken == "" {
		// Profiles are then provisioned on OpenD by its operator.
		return nil
	}
	snapshot := map[string]any{
		"mode": "merge",
		"orgs": []map[string]any{{
			"slug": slug,
			"name": "OpenV " + p.Line + " " + p.Plan,
			"entitlements": map[string]any{
				"products": []string{p.Plan},
				"features": []string{},
				"version":  p.Line,
			},
			"readers": []string{s.readerEmail(p)},
		}},
	}
	body, _ := json.Marshal(snapshot)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.cfg.URL+"/admin/api/entitlements/sync", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.AdminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: entitlement sync answered %d", ErrUnavailable, resp.StatusCode)
	}
	s.mu.Lock()
	s.ensured[slug] = true
	s.mu.Unlock()
	return nil
}

func (s *Service) readerEmail(p Profile) string {
	return p.slug() + "@" + s.cfg.ReaderDomain
}

// token is an OpenD embed token for the profile's service reader: HS256
// over {email, org, aud, iat, exp} (OpenD's "Sign embed tokens" topic).
func (s *Service) token(p Profile) string {
	b64 := base64.RawURLEncoding.EncodeToString
	now := s.now().Unix()
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"email": s.readerEmail(p),
		"org":   p.slug(),
		"aud":   s.cfg.URL,
		"iat":   now,
		"exp":   now + int64(tokenTTL/time.Second),
	})
	signing := b64(header) + "." + b64(claims)
	mac := hmac.New(sha256.New, []byte(s.cfg.Secret))
	mac.Write([]byte(signing))
	return signing + "." + b64(mac.Sum(nil))
}

func (s *Service) cached(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.cache[key]
	if !ok || !s.now().Before(e.expires) {
		return nil, false
	}
	return e.body, true
}

func (s *Service) store(key string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= cacheMax {
		now := s.now()
		for k, e := range s.cache {
			if !now.Before(e.expires) {
				delete(s.cache, k)
			}
		}
		if len(s.cache) >= cacheMax {
			s.cache = map[string]cacheEntry{}
		}
	}
	s.cache[key] = cacheEntry{body: body, expires: s.now().Add(cacheTTL)}
}
