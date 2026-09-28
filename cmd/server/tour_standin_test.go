//go:build unix

package main

import (
	"bufio"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The API tour's stand-ins (refactor plan §6.4 S5c). Some answers exist only
// after the server has talked to a provider: a checkout's URL and the return
// URLs it hands the payment provider (M14's "S5c billing return URL", X12's
// seat count), a Google or OIDC sign-on's session. The recording proxy
// refuses every request before a byte leaves the machine, and the server
// dials those providers at fixed https origins (Stripe's base URL, Google's
// endpoints) that no variable moves. So an area may declare stand-ins
// (tourArea.standIns): for each one's host, the proxy accepts the CONNECT
// instead of refusing it, and the stand-in answers what comes through the
// tunnel over TLS, with a certificate for that host from a certificate
// authority the test makes once per process, which the server trusts through
// SSL_CERT_FILE (the golden shows <the tour's test certificate authority>).
// Go reads SSL_CERT_FILE on Linux and the BSDs, not on macOS, so an area with
// stand-ins skips on darwin; CI runs Linux. Nothing reaches the real
// provider: the stand-in is the test's own code, answering from a script.
//
// Each answer carries Connection: close, so each request the server sends
// takes a CONNECT of its own and outbound_requests counts them exactly. Each
// request is recorded (method, URL, query, headers, a form's fields, sorted,
// normalised) with the status the stand-in answered, under the step during
// which it came (stand_in_requests: a provider call inside a handler happens
// before the handler answers); one that came during a setup, a probe or the
// server's own background work (a reconcile at boot) is listed at the end
// (stand_in_requests_outside_steps) with the exchange it came during. A step
// that makes provider calls should be sent once (a GET is otherwise sent
// twice).
//
// The scripts: newTourStripe (the nine calls of internal/billing/stripe, for
// the price map an area sets), newTourGoogle (the token and userinfo
// endpoints of Google's sign-on, per authorization code the area issues;
// also a code whose access token is answered already expired, expiredToken)
// and newTourIdP (an OIDC identity provider: discovery, JWKS, and a token
// endpoint signing an RS256 id_token with the claims the area issues per
// code; also, for the callback's refusals, a discovery that fails for a
// while, and codes whose token answer is wrong: no id_token, one signed by
// a key its JWKS does not publish, one for another client). An area may
// write its own with newTourStandIn.

// tourStandIn answers one host's requests for an area.
type tourStandIn struct {
	host   string // "api.stripe.com:443"
	about  string
	answer func(req *tourStandInRequest) tourStandInAnswer
	tls    *tls.Config
	log    *tourStandInLog
}

// tourStandInRequest is a request a stand-in received.
type tourStandInRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Form   url.Values // an application/x-www-form-urlencoded body's fields
	Body   []byte
}

// tourStandInAnswer is what a stand-in answers.
type tourStandInAnswer struct {
	status      int
	contentType string
	body        []byte
}

// standInJSON is an answer of JSON.
func standInJSON(status int, v any) tourStandInAnswer {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return tourStandInAnswer{status: status, contentType: "application/json", body: b}
}

// newTourStandIn is a stand-in for host ("name:port") answering with a
// script of the area's own.
func newTourStandIn(host, about string, answer func(req *tourStandInRequest) tourStandInAnswer) *tourStandIn {
	return &tourStandIn{host: host, about: about, answer: answer}
}

// tourStandInLog is every request an area's stand-ins received.
type tourStandInLog struct {
	mu       sync.Mutex
	requests []tourStandInEntry
	failures []string
}

type tourStandInEntry struct {
	at     time.Time
	host   string
	req    *tourStandInRequest
	status int
}

// tourStandInRecord is one request a stand-in received, as the golden shows
// it.
type tourStandInRecord struct {
	During  string   `json:"during,omitempty"` // outside a step: the exchange it came during
	Request string   `json:"request"`
	Query   []string `json:"query,omitempty"`
	Headers []string `json:"headers,omitempty"`
	Form    []string `json:"form,omitempty"`
	Body    string   `json:"body,omitempty"`
	Status  int      `json:"answered"`
}

// tourCA is the certificate authority the stand-ins' certificates chain to,
// made once per test process.
var tourCA struct {
	once sync.Once
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
	err  error
}

func tourTestCA(t *testing.T) {
	t.Helper()
	tourCA.once.Do(func() {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			tourCA.err = err
			return
		}
		tmpl := &x509.Certificate{
			SerialNumber:          big.NewInt(1),
			Subject:               pkix.Name{CommonName: "OpenV API tour test CA"},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().Add(24 * time.Hour),
			KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
			BasicConstraintsValid: true,
			IsCA:                  true,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			tourCA.err = err
			return
		}
		tourCA.cert, _ = x509.ParseCertificate(der)
		tourCA.key = key
		tourCA.pem = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	})
	if tourCA.err != nil {
		t.Fatalf("make the tour's test certificate authority: %v", tourCA.err)
	}
}

// tourLeaf is a certificate for host, signed by the tour's CA.
func tourLeaf(t *testing.T, host string) tls.Certificate {
	t.Helper()
	tourTestCA(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tourCA.cert, &key.PublicKey, tourCA.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// prepareStandIns writes the tour's CA where SSL_CERT_FILE names it and
// returns the proxy's tunnels, one per stand-in.
func (tr *tour) prepareStandIns(env map[string]string) (map[string]string, map[string]func(net.Conn)) {
	t := tr.t
	t.Helper()
	if len(tr.area.standIns) == 0 {
		return env, nil
	}
	if runtime.GOOS == "darwin" {
		t.Skip("the tour's stand-ins need the server to trust a test certificate authority through SSL_CERT_FILE, " +
			"which Go does not read on macOS; CI runs them on Linux")
	}
	tourTestCA(t)
	path := filepath.Join(t.TempDir(), "tour-ca.pem")
	if err := os.WriteFile(path, tourCA.pem, 0o644); err != nil {
		t.Fatal(err)
	}
	env["SSL_CERT_FILE"] = path
	tr.shown["SSL_CERT_FILE"] = "<the tour's test certificate authority>"
	log := &tourStandInLog{}
	tunnels := map[string]func(net.Conn){}
	for _, s := range tr.area.standIns {
		name, _, err := net.SplitHostPort(s.host)
		if err != nil || tunnels[s.host] != nil {
			t.Fatalf("standIns: %q is not a host:port, or is named twice", s.host)
		}
		s.tls = &tls.Config{Certificates: []tls.Certificate{tourLeaf(t, name)}, NextProtos: []string{"http/1.1"}}
		s.log = log
		tunnels[s.host] = s.serve
	}
	return env, tunnels
}

// serve answers one request that came through a tunnel, and closes it.
func (s *tourStandIn) serve(conn net.Conn) {
	tc := tls.Server(conn, s.tls)
	defer tc.Close()
	fail := func(what string, err error) {
		s.log.mu.Lock()
		s.log.failures = append(s.log.failures, fmt.Sprintf("%s: %s: %v", s.host, what, err))
		s.log.mu.Unlock()
	}
	if err := tc.Handshake(); err != nil {
		fail("the TLS handshake failed (does the server trust SSL_CERT_FILE?)", err)
		return
	}
	req, err := http.ReadRequest(bufio.NewReader(tc))
	if err != nil {
		fail("the request did not read", err)
		return
	}
	body, _ := io.ReadAll(req.Body)
	at := time.Now()
	in := &tourStandInRequest{Method: req.Method, Path: req.URL.Path, Query: req.URL.Query(), Header: req.Header, Body: body}
	if strings.HasPrefix(req.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		in.Form, _ = url.ParseQuery(string(body))
	}
	ans := s.answer(in)
	if ans.status == 0 {
		ans = standInJSON(http.StatusNotFound, map[string]string{"error": "the tour's stand-in has no answer for this"})
	}
	s.log.mu.Lock()
	s.log.requests = append(s.log.requests, tourStandInEntry{at: at, host: s.host, req: in, status: ans.status})
	s.log.mu.Unlock()
	head := fmt.Sprintf("HTTP/1.1 %d %s\r\nContent-Type: %s\r\nContent-Length: %d\r\nConnection: close\r\n\r\n",
		ans.status, http.StatusText(ans.status), ans.contentType, len(ans.body))
	_, _ = io.WriteString(tc, head)
	_, _ = tc.Write(ans.body)
}

// standInLog is the area's stand-in log, nil without stand-ins.
func (tr *tour) standInLog() *tourStandInLog {
	for _, s := range tr.area.standIns {
		if s.log != nil {
			return s.log
		}
	}
	return nil
}

// awaitStandIn waits until an area's stand-in for host has answered n
// requests: for provider calls the server makes in the background (a
// reconcile at boot), before a step relies on what they left.
func (tr *tour) awaitStandIn(host string, n int) {
	tr.t.Helper()
	log := tr.standInLog()
	if log == nil {
		tr.t.Fatalf("awaitStandIn %s: the area has no stand-ins", host)
	}
	deadline := time.Now().Add(tourAwaitWithin)
	for {
		log.mu.Lock()
		got := 0
		for _, e := range log.requests {
			if e.host == host {
				got++
			}
		}
		log.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			tr.t.Fatalf("waited %s for %d requests to the stand-in for %s; it answered %d", tourAwaitWithin, n, host, got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// tourBootSettle is how long startTour waits, once the stand-ins have
// answered the requests the server makes while it boots, for the server to
// apply the last answer before the tour's first request.
const tourBootSettle = 100 * time.Millisecond

// awaitBootStandIns waits for tourArea.bootStandIns.
func (tr *tour) awaitBootStandIns() {
	tr.t.Helper()
	if len(tr.area.bootStandIns) == 0 {
		return
	}
	for _, host := range sortedKeys(tr.area.bootStandIns) {
		tr.awaitStandIn(host, tr.area.bootStandIns[host])
	}
	time.Sleep(tourBootSettle)
}

// standInHeadersLeftOut are request headers a record leaves out: set by Go's
// transport on every request, or a length.
var standInHeadersLeftOut = map[string]bool{"Accept-Encoding": true, "Connection": true, "Content-Length": true,
	"User-Agent": true}

// record renders one request, normalised.
func (tr *tour) standInRecord(e tourStandInEntry) tourStandInRecord {
	n := tr.norm
	name, port, _ := net.SplitHostPort(e.host)
	origin := "https://" + name
	if port != "443" {
		origin += ":" + port
	}
	rec := tourStandInRecord{Request: e.req.Method + " " + origin + n.text(e.req.Path), Status: e.status}
	for _, k := range sortedKeys(e.req.Query) {
		for _, v := range e.req.Query[k] {
			rec.Query = append(rec.Query, n.text(k+"="+v))
		}
	}
	for _, k := range sortedKeys(e.req.Header) {
		if standInHeadersLeftOut[k] {
			continue
		}
		for _, v := range e.req.Header[k] {
			rec.Headers = append(rec.Headers, k+": "+tr.headerValue(k, n.text(v)))
		}
	}
	if e.req.Form != nil {
		for _, k := range sortedKeys(e.req.Form) {
			for _, v := range e.req.Form[k] {
				rec.Form = append(rec.Form, n.text(k+"="+v))
			}
		}
	} else if len(e.req.Body) > 0 {
		rec.Body = n.text(string(e.req.Body))
	}
	return rec
}

// standInWindow names the exchange a request came during.
func (tr *tour) standInWindow(e tourStandInEntry) string {
	if w := tr.clock.window(e.at); w != "" {
		return w
	}
	return "after the area's last request"
}

// renderStandInRequests renders the requests that came during one step.
func (tr *tour) renderStandInRequests(step string) []tourStandInRecord {
	log := tr.standInLog()
	if log == nil {
		return nil
	}
	log.mu.Lock()
	entries := append([]tourStandInEntry(nil), log.requests...)
	log.mu.Unlock()
	var out []tourStandInRecord
	for _, e := range entries {
		if tr.standInWindow(e) == step {
			out = append(out, tr.standInRecord(e))
		}
	}
	return out
}

// renderStandInsOutsideSteps renders the requests that came during no step,
// in the order they came, and fails the area on any the stand-ins could not
// read.
func (tr *tour) renderStandInsOutsideSteps() []tourStandInRecord {
	log := tr.standInLog()
	if log == nil {
		return nil
	}
	log.mu.Lock()
	entries := append([]tourStandInEntry(nil), log.requests...)
	failures := append([]string(nil), log.failures...)
	log.mu.Unlock()
	for _, f := range failures {
		tr.t.Errorf("a stand-in could not answer: %s", f)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].at.Before(entries[j].at) })
	var out []tourStandInRecord
	for _, e := range entries {
		w := tr.standInWindow(e)
		if strings.HasPrefix(w, "step ") {
			continue
		}
		rec := tr.standInRecord(e)
		rec.During = w
		out = append(out, rec)
	}
	return out
}

// ----------------------------------------------------------------------------
// Stripe

// tourStripe answers as Stripe's API for one price map: the calls of
// internal/billing/stripe, with fixed amounts and ids numbered in the order
// they are created. A subscription is never created, so no seat sync or
// reconcile ever has one to act on; a checkout session reads back open.
type tourStripe struct {
	mu        sync.Mutex
	intervals map[string]string // price id -> interval
	customers int
	sessions  map[string]string // checkout session id -> client_reference_id
	portals   int
}

// newTourStripe is a Stripe stand-in for the price map an area also sets as
// OPENV_STRIPE_PRICES.
func newTourStripe(priceMap string) *tourStripe {
	var entries []struct{ Price, Interval string }
	if err := json.Unmarshal([]byte(priceMap), &entries); err != nil {
		panic(fmt.Sprintf("newTourStripe: the price map does not parse: %v", err))
	}
	s := &tourStripe{intervals: map[string]string{}, sessions: map[string]string{}}
	for _, e := range entries {
		s.intervals[e.Price] = e.Interval
	}
	return s
}

// standIn is the stand-in for api.stripe.com.
func (s *tourStripe) standIn() *tourStandIn {
	return newTourStandIn("api.stripe.com:443", "Stripe's API, answered from the tour's script", s.answer)
}

func stripeError(status int, code, message string) tourStandInAnswer {
	return standInJSON(status, map[string]any{"error": map[string]string{"type": "invalid_request_error", "code": code,
		"message": message}})
}

func (s *tourStripe) answer(r *tourStandInRequest) tourStandInAnswer {
	s.mu.Lock()
	defer s.mu.Unlock()
	empty := map[string]any{"object": "list", "data": []any{}, "has_more": false}
	switch {
	case r.Method == http.MethodGet && (r.Path == "/v1/disputes" || r.Path == "/v1/subscriptions"):
		return standInJSON(http.StatusOK, empty)
	case r.Method == http.MethodGet && strings.HasPrefix(r.Path, "/v1/prices/"):
		id := strings.TrimPrefix(r.Path, "/v1/prices/")
		interval, ok := s.intervals[id]
		if !ok {
			return stripeError(http.StatusNotFound, "resource_missing", "No such price: '"+id+"'")
		}
		return standInJSON(http.StatusOK, map[string]any{"id": id, "object": "price", "currency": "gbp",
			"unit_amount": 1200, "tax_behavior": "exclusive",
			"recurring":        map[string]string{"interval": interval, "usage_type": "licensed"},
			"currency_options": map[string]any{"eur": map[string]int{"unit_amount": 1400}, "gbp": map[string]int{"unit_amount": 1200}, "usd": map[string]int{"unit_amount": 1500}}})
	case r.Method == http.MethodPost && r.Path == "/v1/customers":
		s.customers++
		return standInJSON(http.StatusOK, map[string]any{"id": fmt.Sprintf("cus_tour_%d", s.customers), "object": "customer"})
	case r.Method == http.MethodPost && r.Path == "/v1/checkout/sessions":
		id := fmt.Sprintf("cs_tour_%d", len(s.sessions)+1)
		s.sessions[id] = r.Form.Get("client_reference_id")
		return standInJSON(http.StatusOK, map[string]any{"id": id, "object": "checkout.session",
			"url": "https://checkout.stripe.com/c/pay/" + id, "client_reference_id": s.sessions[id],
			"customer": r.Form.Get("customer"), "status": "open"})
	case r.Method == http.MethodGet && strings.HasPrefix(r.Path, "/v1/checkout/sessions/"):
		id := strings.TrimPrefix(r.Path, "/v1/checkout/sessions/")
		ref, ok := s.sessions[id]
		if !ok {
			return stripeError(http.StatusNotFound, "resource_missing", "No such checkout.session: '"+id+"'")
		}
		return standInJSON(http.StatusOK, map[string]any{"id": id, "object": "checkout.session", "client_reference_id": ref,
			"status": "open"})
	case r.Method == http.MethodPost && r.Path == "/v1/billing_portal/sessions":
		s.portals++
		return standInJSON(http.StatusOK, map[string]any{"object": "billing_portal.session",
			"url": fmt.Sprintf("https://billing.stripe.com/p/session/tour_%d", s.portals)})
	}
	return stripeError(http.StatusNotFound, "resource_missing", "the tour's Stripe stand-in has no "+r.Method+" "+r.Path)
}

// ----------------------------------------------------------------------------
// Google sign-on

// tourGoogle answers Google's token and userinfo endpoints for the
// authorization codes an area issues (user).
type tourGoogle struct {
	mu      sync.Mutex
	users   map[string]map[string]any // code -> the userinfo its access token reads
	expired map[string]bool           // codes whose access token is answered already expired
}

func newTourGoogle() *tourGoogle {
	return &tourGoogle{users: map[string]map[string]any{}, expired: map[string]bool{}}
}

// user makes code one the token endpoint accepts, and info what the userinfo
// endpoint then answers.
func (g *tourGoogle) user(code string, info map[string]any) {
	g.mu.Lock()
	g.users[code] = info
	g.mu.Unlock()
}

// expiredToken makes code one the token endpoint accepts with an access
// token that expired a minute ago and no refresh token: the server's OAuth
// client then refuses to use it, so the userinfo fetch fails before any
// request is sent.
func (g *tourGoogle) expiredToken(code string) {
	g.mu.Lock()
	g.users[code] = map[string]any{}
	g.expired[code] = true
	g.mu.Unlock()
}

const tourGoogleAccessPrefix = "tour-google-access-"

// standIns are the stand-ins for Google's two hosts.
func (g *tourGoogle) standIns() []*tourStandIn {
	return []*tourStandIn{
		newTourStandIn("oauth2.googleapis.com:443", "Google's token endpoint, answered from the tour's script", g.token),
		newTourStandIn("www.googleapis.com:443", "Google's userinfo endpoint, answered from the tour's script", g.userinfo),
	}
}

func (g *tourGoogle) token(r *tourStandInRequest) tourStandInAnswer {
	g.mu.Lock()
	defer g.mu.Unlock()
	code := r.Form.Get("code")
	if r.Method != http.MethodPost || r.Path != "/token" || g.users[code] == nil {
		return standInJSON(http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": "Bad Request"})
	}
	expiresIn := 3599
	if g.expired[code] {
		expiresIn = -60
	}
	return standInJSON(http.StatusOK, map[string]any{"access_token": tourGoogleAccessPrefix + code, "token_type": "Bearer",
		"expires_in": expiresIn, "scope": "openid email profile"})
}

func (g *tourGoogle) userinfo(r *tourStandInRequest) tourStandInAnswer {
	g.mu.Lock()
	defer g.mu.Unlock()
	code, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "+tourGoogleAccessPrefix)
	if r.Method != http.MethodGet || r.Path != "/oauth2/v3/userinfo" || !ok || g.users[code] == nil {
		return standInJSON(http.StatusUnauthorized, map[string]any{"error": "invalid_request"})
	}
	return standInJSON(http.StatusOK, g.users[code])
}

// ----------------------------------------------------------------------------
// An OIDC identity provider

// tourIdP answers as an OIDC identity provider at https://<host>: discovery,
// its JWKS (a key made per area), and a token endpoint that signs an RS256
// id_token for each code an area issues, with that code's claims (the nonce
// among them, which the area captures from the login redirect). For the
// refusals a sign-on meets when the provider misbehaves, its discovery can
// fail for a number of requests (failDiscovery), and a code can be issued
// with a wrong token answer (issueWrong).
type tourIdP struct {
	host     string
	clientID string
	key      *rsa.PrivateKey
	mu       sync.Mutex
	codes    map[string]map[string]any
	wrong    map[string]tourIdPWrong // code -> how its token answer is wrong
	down     int                     // discovery requests still to answer 503
}

// tourIdPWrong is how a token answer is wrong (tourIdP.issueWrong).
type tourIdPWrong string

const (
	// tourIdPNoIDToken answers an access token and no id_token.
	tourIdPNoIDToken tourIdPWrong = "no id_token"
	// tourIdPForeignKey signs the id_token with a key of its own under the
	// JWKS key's kid, so its signature does not verify.
	tourIdPForeignKey tourIdPWrong = "an id_token signed by a key the JWKS does not publish"
	// tourIdPOtherAudience issues the id_token to another client (aud).
	tourIdPOtherAudience tourIdPWrong = "an id_token issued to another client"
)

// newTourIdP is an identity provider at https://host for a client id, the
// area's OPENV_OIDC_ISSUER and OPENV_OIDC_CLIENT_ID.
func newTourIdP(host, clientID string) *tourIdP {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return &tourIdP{host: host, clientID: clientID, key: key, codes: map[string]map[string]any{},
		wrong: map[string]tourIdPWrong{}}
}

// failDiscovery makes the next n discovery requests answer 503, as a
// provider that is down would; the server tries discovery again on each
// sign-on request until one succeeds.
func (p *tourIdP) failDiscovery(n int) {
	p.mu.Lock()
	p.down = n
	p.mu.Unlock()
}

// issueWrong is issue for a code whose token answer is wrong in the way
// named: the claims go into the id_token where there is one.
func (p *tourIdP) issueWrong(code string, how tourIdPWrong, claims map[string]any) {
	p.mu.Lock()
	p.codes[code], p.wrong[code] = claims, how
	p.mu.Unlock()
}

func (p *tourIdP) issuer() string { return "https://" + p.host }

// issue makes code one the token endpoint accepts, answering an id_token
// with these claims over iss, aud, sub, iat and exp.
func (p *tourIdP) issue(code string, claims map[string]any) {
	p.mu.Lock()
	p.codes[code] = claims
	p.mu.Unlock()
}

func (p *tourIdP) standIn() *tourStandIn {
	return newTourStandIn(p.host+":443", "an OIDC identity provider, answered from the tour's script", p.answer)
}

func (p *tourIdP) answer(r *tourStandInRequest) tourStandInAnswer {
	p.mu.Lock()
	defer p.mu.Unlock()
	iss := p.issuer()
	switch {
	case r.Method == http.MethodGet && r.Path == "/.well-known/openid-configuration" && p.down > 0:
		p.down--
		return standInJSON(http.StatusServiceUnavailable, map[string]string{"error": "temporarily_unavailable"})
	case r.Method == http.MethodGet && r.Path == "/.well-known/openid-configuration":
		return standInJSON(http.StatusOK, map[string]any{"issuer": iss, "authorization_endpoint": iss + "/authorize",
			"token_endpoint": iss + "/token", "jwks_uri": iss + "/jwks", "userinfo_endpoint": iss + "/userinfo",
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"}})
	case r.Method == http.MethodGet && r.Path == "/jwks":
		return standInJSON(http.StatusOK, map[string]any{"keys": []map[string]string{{"kty": "RSA", "alg": "RS256",
			"use": "sig", "kid": "tour-idp", "n": base64.RawURLEncoding.EncodeToString(p.key.PublicKey.N.Bytes()), "e": "AQAB"}}})
	case r.Method == http.MethodPost && r.Path == "/token":
		claims, ok := p.codes[r.Form.Get("code")]
		if !ok {
			return standInJSON(http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		}
		all := map[string]any{"iss": iss, "aud": p.clientID, "sub": "tour-subject-" + r.Form.Get("code"),
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix()}
		for k, v := range claims {
			all[k] = v
		}
		answer := map[string]any{"access_token": "tour-idp-access", "token_type": "Bearer", "expires_in": 3600}
		switch p.wrong[r.Form.Get("code")] {
		case tourIdPNoIDToken:
		case tourIdPForeignKey:
			foreign, err := rsa.GenerateKey(rand.Reader, 2048)
			if err != nil {
				panic(err)
			}
			answer["id_token"] = signRS256(foreign, all)
		case tourIdPOtherAudience:
			all["aud"] = "another-client"
			answer["id_token"] = p.sign(all)
		default:
			answer["id_token"] = p.sign(all)
		}
		return standInJSON(http.StatusOK, answer)
	}
	return standInJSON(http.StatusNotFound, map[string]string{"error": "not_found"})
}

// sign makes an RS256 JWT of claims with the provider's key.
func (p *tourIdP) sign(claims map[string]any) string { return signRS256(p.key, claims) }

// signRS256 makes an RS256 JWT of claims with key, under the kid the
// provider's JWKS publishes.
func signRS256(key *rsa.PrivateKey, claims map[string]any) string {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	input := enc(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "tour-idp"}) + "." + enc(claims)
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// ----------------------------------------------------------------------------
// A check with no server

// TestTourStandIns checks the stand-ins with no database: a client that
// trusts the tour's CA and goes through the recording proxy, as the server
// does, reaches each stand-in over TLS and gets its script's answers; each
// request is recorded, normalised, under the exchange it came during; a host
// with no stand-in is still refused; and the proxy's summary says which
// CONNECTs it tunnelled.
func TestTourStandIns(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("see prepareStandIns")
	}
	const prices = `[{"price":"price_tour_lite_month","plan":"business_lite","interval":"month"}]`
	stripe := newTourStripe(prices)
	google := newTourGoogle()
	google.user("code-1", map[string]any{"email": "g@example.com", "email_verified": true})
	idp := newTourIdP("idp.tour.example", "tour-oidc")
	idp.issue("code-2", map[string]any{"nonce": "n", "email": "o@example.com"})
	tr := &tour{t: t, norm: newTourNormaliser(), names: map[string]string{}, values: map[string]string{},
		shown: map[string]string{}, clock: &tourClock{start: time.Now()},
		area: tourArea{standIns: append(google.standIns(), stripe.standIn(), idp.standIn())}}
	tr.norm.clock = tr.clock
	env, tunnels := tr.prepareStandIns(map[string]string{})
	if env["SSL_CERT_FILE"] == "" || len(tunnels) != 4 {
		t.Fatalf("prepareStandIns: %v %d", env, len(tunnels))
	}
	tr.proxy = startRecordingProxyWith(t, tunnels)
	proxyURL, _ := url.Parse(tr.proxy.url())
	roots := x509.NewCertPool()
	roots.AddCert(tourCA.cert)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: roots}}}
	mark := func(label string) { tr.clock.marks = append(tr.clock.marks, tourMark{end: time.Now(), label: label}) }
	get := func(u string, header ...string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		for i := 0; i+1 < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	post := func(u string, form url.Values) (int, string) {
		t.Helper()
		resp, err := client.PostForm(u, form)
		if err != nil {
			return 0, err.Error()
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if st, body := get("https://api.stripe.com/v1/prices/price_tour_lite_month?expand%5B%5D=currency_options",
		"Authorization", "Bearer sk_test_x"); st != 200 || !strings.Contains(body, `"interval":"month"`) {
		t.Errorf("a price: %d %s", st, body)
	}
	mark("before step 1")
	if st, body := post("https://api.stripe.com/v1/checkout/sessions", url.Values{"client_reference_id": {"org"},
		"success_url": {"https://app.tour.example/org/settings?tab=billing"}}); st != 200 ||
		!strings.Contains(body, `"url":"https://checkout.stripe.com/c/pay/cs_tour_1"`) {
		t.Errorf("a checkout session: %d %s", st, body)
	}
	if st, body := post("https://oauth2.googleapis.com/token", url.Values{"code": {"code-1"}}); st != 200 ||
		!strings.Contains(body, tourGoogleAccessPrefix+"code-1") {
		t.Errorf("Google's token: %d %s", st, body)
	}
	if st, body := get("https://www.googleapis.com/oauth2/v3/userinfo", "Authorization", "Bearer "+tourGoogleAccessPrefix+"code-1"); st != 200 ||
		!strings.Contains(body, `"email":"g@example.com"`) {
		t.Errorf("Google's userinfo: %d %s", st, body)
	}
	if st, body := post("https://idp.tour.example/token", url.Values{"code": {"code-2"}}); st != 200 || !strings.Contains(body, `"id_token":"`) {
		t.Errorf("the identity provider's token: %d %s", st, body)
	}
	mark("step 1")
	if st, body := get("https://api.github.com/"); st != 0 || !strings.Contains(body, proxyRefusal) {
		t.Errorf("a host with no stand-in: %d %s", st, body)
	}
	steps := tr.renderStandInRequests("step 1")
	if len(steps) != 4 || steps[0].Request != "POST https://api.stripe.com/v1/checkout/sessions" ||
		strings.Join(steps[0].Form, "|") != "client_reference_id=org|success_url=https://app.tour.example/org/settings?tab=billing" ||
		steps[0].Status != 200 || steps[3].Request != "POST https://idp.tour.example/token" {
		t.Errorf("the step's stand-in requests: %+v", steps)
	}
	outside := tr.renderStandInsOutsideSteps()
	if len(outside) != 1 || outside[0].During != "boot" || strings.Join(outside[0].Query, "|") != "expand[]=currency_options" ||
		!contains(outside[0].Headers, "Authorization: Bearer sk_test_x") {
		t.Errorf("the requests outside steps: %+v", outside)
	}
	summary := strings.Join(tr.proxy.summary(), "\n")
	for _, want := range []string{"CONNECT api.stripe.com:443: 2 tunnelled", "CONNECT oauth2.googleapis.com:443: 1 tunnelled",
		"CONNECT api.github.com:443: 1 tried, each answered 403"} {
		if !strings.Contains(summary, want) {
			t.Errorf("the proxy's summary lacks %q:\n%s", want, summary)
		}
	}

	// The identity provider misbehaving: a discovery that fails once, and
	// the wrong token answers, each with the claims issued.
	idp.failDiscovery(1)
	for i, want := range []int{503, 200} {
		if st, _ := get("https://idp.tour.example/.well-known/openid-configuration"); st != want {
			t.Errorf("discovery request %d while it fails once: %d, want %d", i+1, st, want)
		}
	}
	idp.issueWrong("code-none", tourIdPNoIDToken, map[string]any{"nonce": "n"})
	idp.issueWrong("code-foreign", tourIdPForeignKey, map[string]any{"nonce": "n"})
	idp.issueWrong("code-aud", tourIdPOtherAudience, map[string]any{"nonce": "n"})
	idToken := func(code string) (header, claims map[string]any, verified bool) {
		t.Helper()
		st, body := post("https://idp.tour.example/token", url.Values{"code": {code}})
		var ans struct {
			AccessToken string `json:"access_token"`
			IDToken     string `json:"id_token"`
		}
		if err := json.Unmarshal([]byte(body), &ans); st != 200 || err != nil || ans.AccessToken == "" {
			t.Fatalf("the token answer for %s: %d %s", code, st, body)
		}
		if ans.IDToken == "" {
			return nil, nil, false
		}
		parts := strings.Split(ans.IDToken, ".")
		part := func(s string) map[string]any {
			b, _ := base64.RawURLEncoding.DecodeString(s)
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			return m
		}
		sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		return part(parts[0]), part(parts[1]), rsa.VerifyPKCS1v15(&idp.key.PublicKey, crypto.SHA256, digest[:], sig) == nil
	}
	if h, _, _ := idToken("code-none"); h != nil {
		t.Errorf("a token answer with no id_token carries one: %v", h)
	}
	if h, c, ok := idToken("code-foreign"); ok || h["kid"] != "tour-idp" || c["aud"] != "tour-oidc" || c["nonce"] != "n" {
		t.Errorf("an id_token signed by a foreign key: verifies %v, header %v, claims %v", ok, h, c)
	}
	if _, c, ok := idToken("code-aud"); !ok || c["aud"] != "another-client" || c["nonce"] != "n" {
		t.Errorf("an id_token for another client: verifies %v, claims %v", ok, c)
	}
	if _, c, ok := idToken("code-2"); !ok || c["aud"] != "tour-oidc" || c["iss"] != "https://idp.tour.example" {
		t.Errorf("a good id_token: verifies %v, claims %v", ok, c)
	}
	if got := strconv.Itoa(len(tr.standInLog().failures)); got != "0" {
		t.Errorf("stand-in failures: %v", tr.standInLog().failures)
	}
}
