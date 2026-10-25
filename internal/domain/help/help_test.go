package help

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// secret is the fake embed secret both sides of the tests sign with.
var secret = strings.Repeat("k", 32)

// fakeOpenD answers the embed help API for the lines in published, checks
// every token, and records entitlement syncs.
type fakeOpenD struct {
	t         *testing.T
	url       string
	published map[string]bool // lines with docs
	mu        sync.Mutex
	synced    []string        // org slugs upserted
	known     map[string]bool // org slugs OpenD has a reader for
	calls     int
}

func newFake(t *testing.T, lines ...string) (*fakeOpenD, *httptest.Server) {
	f := &fakeOpenD{t: t, published: map[string]bool{}, known: map[string]bool{}}
	for _, l := range lines {
		f.published[l] = true
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return f, srv
}

func (f *fakeOpenD) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/admin/api/entitlements/sync" {
		if r.Header.Get("Authorization") != "Bearer admin" {
			w.WriteHeader(401)
			return
		}
		var body struct {
			Orgs []struct {
				Slug         string `json:"slug"`
				Entitlements struct {
					Products []string `json:"products"`
					Version  string   `json:"version"`
				} `json:"entitlements"`
				Readers []string `json:"readers"`
			} `json:"orgs"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, o := range body.Orgs {
			f.synced = append(f.synced, o.Slug)
			f.known[o.Slug] = true
		}
		w.WriteHeader(200)
		return
	}
	f.calls++
	claims := f.verify(r.Header.Get("Authorization"))
	if claims == nil {
		w.WriteHeader(401)
		return
	}
	org := claims["org"].(string)
	if !f.known[org] {
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"error":"no access"}`))
		return
	}
	// openv-0-23-business -> 0.23
	parts := strings.Split(org, "-")
	line := parts[1] + "." + parts[2]
	if !f.published[line] {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"Nothing is published for release ` + line + ` yet."}`))
		return
	}
	switch {
	case r.URL.Path == "/api/v1/help/context" && r.URL.Query().Get("key") == "nothing":
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"no help for this context"}`))
	case r.URL.Path == "/api/v1/help/context", strings.HasPrefix(r.URL.Path, "/api/v1/help/topics/"):
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key": r.URL.Query().Get("key"), "via": "key", "fragment": nil,
			"topicId": "topics/vv/test-runs", "title": "Test runs " + line, "shortdesc": "Runs.",
			"html": `<article><p onclick="x()">See <a href="` + f.url + `/topics/vv/gaps.html#g">gaps</a>` +
				`<script>alert(1)</script><img src="https://evil.example/x.png"></p></article>`,
			"related": []map[string]string{{"topicId": "topics/vv/gaps", "title": "Gaps"}},
			"fullUrl": f.url + "/topics/vv/test-runs.html",
		})
	case r.URL.Path == "/api/v1/help/search":
		_ = json.NewEncoder(w).Encode(map[string]any{"query": r.URL.Query().Get("q"),
			"hits": []map[string]any{{"topicId": "topics/vv/gaps", "title": "Gaps", "snippet": "…gaps…", "score": 1}}})
	default:
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}
}

func (f *fakeOpenD) verify(auth string) map[string]any {
	tok, ok := strings.CutPrefix(auth, "Bearer ")
	parts := strings.Split(tok, ".")
	if !ok || len(parts) != 3 {
		return nil
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) != parts[2] {
		return nil
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c map[string]any
	_ = json.Unmarshal(raw, &c)
	if c["aud"] != f.url || c["exp"].(float64)-c["iat"].(float64) > 600 {
		return nil
	}
	if c["email"] != c["org"].(string)+"@help.openv.app" {
		return nil
	}
	return c
}

func service(f *fakeOpenD, admin string) *Service {
	return NewService(Config{URL: f.url + "/", Secret: secret, AdminToken: admin}, nil)
}

func TestNewServiceIsOffWithoutURL(t *testing.T) {
	if NewService(Config{Secret: secret}, nil) != nil {
		t.Fatal("help must be off without the docs URL")
	}
	if NewService(Config{URL: "https://docs"}, nil) == nil {
		t.Fatal("the docs URL alone turns public help on")
	}
}

// publicOpenD answers the help API anonymously, as a public OpenD site
// does: a version it offers is served from its own line, any other from
// the default, and the line served is named in OpenD-Release-Line.
func publicOpenD(t *testing.T, lines ...string) (*httptest.Server, *[]string) {
	var seen []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.RequestURI())
		mu.Unlock()
		if r.Header.Get("Authorization") != "" {
			t.Errorf("public read sent credentials: %s", r.Header.Get("Authorization"))
		}
		line := lines[0]
		for _, l := range lines {
			if l == r.URL.Query().Get("version") {
				line = l
			}
		}
		w.Header().Set("OpenD-Release-Line", line)
		switch r.URL.Path {
		case "/api/v1/help/context":
			_ = json.NewEncoder(w).Encode(map[string]any{"topicId": "topics/vv/test-runs", "title": "Test runs " + line,
				"html": `<p>Runs<script>x()</script></p>`, "fullUrl": "https://docs/topics/vv/test-runs.html"})
		case "/api/v1/help/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"query": r.URL.Query().Get("q"), "hits": []any{}})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestPublicModeAsksForTheLineAndReportsTheOneServed(t *testing.T) {
	srv, seen := publicOpenD(t, "0.23", "0.22")
	s := NewService(Config{URL: srv.URL}, nil)
	for _, c := range []struct{ asked, served string }{{"0.22", "0.22"}, {"0.24", "0.23"}} {
		topic, err := s.Context(context.Background(), Profile{Line: c.asked, Plan: "free"}, "vv.runs", "")
		if err != nil {
			t.Fatal(err)
		}
		if topic.Line != c.served || topic.Title != "Test runs "+c.served || strings.Contains(topic.HTML, "<script") {
			t.Fatalf("asked %s: topic = %+v", c.asked, topic)
		}
	}
	if _, err := s.Context(context.Background(), Profile{Line: "0.22", Plan: "business"}, "vv.runs", ""); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 2 || !strings.Contains((*seen)[0], "version=0.22") {
		t.Fatalf("requests = %v; want one per line, cached across plans", *seen)
	}
	if _, err := s.Search(context.Background(), Profile{Line: "0.23"}, "gaps"); err != nil {
		t.Fatal(err)
	}
	if last := (*seen)[len(*seen)-1]; !strings.Contains(last, "q=gaps") || !strings.Contains(last, "&version=0.23") {
		t.Fatalf("search request = %s", last)
	}
}

func TestContextProvisionsTheProfileAndSanitizes(t *testing.T) {
	f, _ := newFake(t, "0.23")
	s := service(f, "admin")
	topic, err := s.Context(context.Background(), Profile{Line: "0.23", Plan: "business"}, "vv.runs", "")
	if err != nil {
		t.Fatal(err)
	}
	if topic.Title != "Test runs 0.23" || topic.Line != "0.23" || topic.TopicID != "topics/vv/test-runs" {
		t.Fatalf("topic = %+v", topic)
	}
	if len(f.synced) != 1 || f.synced[0] != "openv-0-23-business" {
		t.Fatalf("synced = %v", f.synced)
	}
	for _, bad := range []string{"<script", "onclick", "evil.example"} {
		if strings.Contains(topic.HTML, bad) {
			t.Errorf("html keeps %q: %s", bad, topic.HTML)
		}
	}
	if !strings.Contains(topic.HTML, `data-help-topic="topics/vv/gaps"`) || !strings.Contains(topic.HTML, `data-help-fragment="g"`) {
		t.Errorf("docs link not marked for the drawer: %s", topic.HTML)
	}
	if topic.FullURL != f.url+"/topics/vv/test-runs.html" {
		t.Errorf("full url = %q", topic.FullURL)
	}
}

func TestFallsBackToTheNewestPublishedLine(t *testing.T) {
	f, _ := newFake(t, "0.21")
	s := service(f, "admin")
	topic, err := s.Context(context.Background(), Profile{Line: "0.23", Plan: "free"}, "vv", "")
	if err != nil {
		t.Fatal(err)
	}
	if topic.Line != "0.21" {
		t.Fatalf("line = %q, want the newest published, 0.21", topic.Line)
	}
	// The choice is remembered: the next request asks 0.21 straight away.
	before := f.calls
	if _, err := s.Search(context.Background(), Profile{Line: "0.23", Plan: "free"}, "gaps"); err != nil {
		t.Fatal(err)
	}
	if f.calls-before != 1 {
		t.Fatalf("search took %d calls, want 1", f.calls-before)
	}
}

func TestNoHelpForAContextIsNotFound(t *testing.T) {
	f, _ := newFake(t, "0.23")
	_, err := service(f, "admin").Context(context.Background(), Profile{Line: "0.23", Plan: "free"}, "nothing", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestNothingPublishedAtAllIsNotFound(t *testing.T) {
	f, _ := newFake(t)
	_, err := service(f, "admin").Context(context.Background(), Profile{Line: "0.3", Plan: "free"}, "vv", "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAnswersAreCachedPerProfile(t *testing.T) {
	f, _ := newFake(t, "0.23")
	s := service(f, "admin")
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := s.TopicByID(ctx, Profile{Line: "0.23", Plan: "free"}, "topics/vv/test-runs"); err != nil {
			t.Fatal(err)
		}
	}
	if f.calls != 1 {
		t.Fatalf("calls = %d, want 1", f.calls)
	}
	if _, err := s.TopicByID(ctx, Profile{Line: "0.23", Plan: "business"}, "topics/vv/test-runs"); err != nil {
		t.Fatal(err)
	}
	if f.calls != 2 {
		t.Fatalf("another profile shared the cache: calls = %d", f.calls)
	}
}

func TestWithoutAnAdminTokenAnUnknownProfileIsUnavailable(t *testing.T) {
	f, _ := newFake(t, "0.23")
	_, err := service(f, "").Context(context.Background(), Profile{Line: "0.23", Plan: "free"}, "vv", "")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestTopicIDsAreChecked(t *testing.T) {
	f, _ := newFake(t, "0.23")
	for _, id := range []string{"", "../admin", "/etc", "topics/a?b", "topics/%2e%2e"} {
		if _, err := service(f, "admin").TopicByID(context.Background(), Profile{Line: "0.23", Plan: "free"}, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: err = %v, want ErrNotFound", id, err)
		}
	}
}

func TestUnreachableDocsAreUnavailable(t *testing.T) {
	s := NewService(Config{URL: "http://127.0.0.1:1", Secret: secret}, nil)
	if _, err := s.Search(context.Background(), Profile{Line: "0.23", Plan: "free"}, "x"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestLineAndPlanFamily(t *testing.T) {
	for in, want := range map[string]string{"0.23.1": "0.23", "v1.2.0": "1.2", "0.23": "0.23", "x": "", "1": "", "a.b.c": ""} {
		if got := Line(in); got != want {
			t.Errorf("Line(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"single": "free", "free": "free", "business_lite": "lite", "team": "business",
		"open_source": "business", "enterprise": "enterprise", "self_host": "self-host", "": "free"} {
		if got := PlanFamily(in); got != want {
			t.Errorf("PlanFamily(%q) = %q, want %q", in, got, want)
		}
	}
	if previousLine("0.1") != "0.0" || previousLine("0.0") != "" || previousLine("x") != "" {
		t.Error("previousLine")
	}
}

func TestSanitize(t *testing.T) {
	docs := "https://docs.openv.app"
	cases := map[string]string{
		`<p>a &amp; <b>b</b></p>`:                                   `<p>a &amp; <b>b</b></p>`,
		`<p><a href="javascript:alert(1)">x</a></p>`:                `<p><a>x</a></p>`,
		`<p><a href="#s">x</a></p>`:                                 `<p><a href="#s">x</a></p>`,
		`<p><a href="https://example.com">x</a></p>`:                `<p><a href="https://example.com" target="_blank" rel="noopener noreferrer">x</a></p>`,
		`<div><style>p{}</style><iframe src="x"></iframe>t</div>`:   `<div>t</div>`,
		`<p class="a&quot;b" id="ok" style="color:red">x</p>`:       `<p id="ok">x</p>`,
		`<img src="https://docs.openv.app/_assets/a.png" alt="A">`:  `<img src="https://docs.openv.app/_assets/a.png" alt="A">`,
		`<p><img src="data:image/png;base64,AA" onerror="x()"></p>`: `<p></p>`,
		`<custom-el><p>kept</p></custom-el>`:                        `<p>kept</p>`,
		`<svg><script>alert(1)</script></svg><p>z</p>`:              `<p>z</p>`,
	}
	for in, want := range cases {
		if got := Sanitize(in, docs); got != want {
			t.Errorf("Sanitize(%s)\n got %s\nwant %s", in, got, want)
		}
	}
}
