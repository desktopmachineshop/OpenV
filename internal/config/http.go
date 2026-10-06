package config

// The settings of stages sso, billing, handlers and server: single sign-on,
// billing, the API handler's settings and rate limits, and the HTTP
// server's.

import (
	"strings"

	"github.com/openv/requirements-platform/internal/billing"
	"github.com/openv/requirements-platform/internal/envparse"
)

// GoogleOAuth is Google sign-in as stage sso configures it, the fields of
// api.GoogleOAuthConfig.
type GoogleOAuth struct {
	ClientID     string
	ClientSecret string // a credential, exactly as set
	RedirectURL  string // PUBLIC_URL (else this server's localhost) + /api/v1/auth/google/callback
	FrontendURL  string // FRONTEND_URL, else the dev frontend
}

// GoogleOAuth is nil unless GOOGLE_CLIENT_ID is set; only then are the
// secret, PUBLIC_URL and FRONTEND_URL read, in today's order.
func (c *Config) GoogleOAuth() *GoogleOAuth {
	clientID := c.text("GOOGLE_CLIENT_ID", "")
	if clientID == "" {
		return nil
	}
	publicURL := c.text("PUBLIC_URL", "http://localhost:"+c.Port())
	return &GoogleOAuth{
		ClientID:     clientID,
		ClientSecret: c.secret("GOOGLE_CLIENT_SECRET", ""),
		RedirectURL:  publicURL + "/api/v1/auth/google/callback",
		FrontendURL:  c.text("FRONTEND_URL", "http://localhost:3000"),
	}
}

// OIDC is generic single sign-on as stage sso configures it, the settings
// of api.OIDCConfig.
type OIDC struct {
	Issuer       string
	ClientID     string
	ClientSecret string   // a credential, exactly as set
	RedirectURL  string   // OPENV_OIDC_REDIRECT_URL, else PUBLIC_URL (else this server's localhost) + /api/v1/auth/oidc/callback
	Scopes       []string // OPENV_OIDC_SCOPES split on spaces; nil when unset or empty
	ProviderName string   // OPENV_OIDC_NAME, else SSO
	FrontendURL  string   // FRONTEND_URL, else the dev frontend
}

// OIDC is nil unless OPENV_OIDC_ISSUER is set; only then are the other
// OIDC settings, PUBLIC_URL and FRONTEND_URL read, in today's order.
// OPENV_OIDC_SCOPES is read untrimmed, as today: a value of only spaces is
// set, and splits into no scope (an empty list, not nil).
func (c *Config) OIDC() *OIDC {
	issuer := c.text("OPENV_OIDC_ISSUER", "")
	if issuer == "" {
		return nil
	}
	publicURL := c.text("PUBLIC_URL", "http://localhost:"+c.Port())
	redirectURL := c.text("OPENV_OIDC_REDIRECT_URL", publicURL+"/api/v1/auth/oidc/callback")
	var scopes []string
	if raw := c.getenv("OPENV_OIDC_SCOPES"); raw != "" {
		scopes = strings.Fields(raw)
	}
	return &OIDC{
		Issuer:       issuer,
		ClientID:     c.text("OPENV_OIDC_CLIENT_ID", ""),
		ClientSecret: c.secret("OPENV_OIDC_CLIENT_SECRET", ""),
		RedirectURL:  redirectURL,
		Scopes:       scopes,
		ProviderName: c.text("OPENV_OIDC_NAME", "SSO"),
		FrontendURL:  c.text("FRONTEND_URL", "http://localhost:3000"),
	}
}

// Billing is billing.ConfigFromEnv over the recorded values. Its error is
// fatal in stage billing ("billing configuration is not usable") whether
// or not STRIPE_SECRET_KEY is set and whether or not the install is
// self-hosted; a self-hosted install with a key keeps billing off, which is
// the stage's decision, not this one's.
func (c *Config) Billing() (billing.Config, error) {
	return billing.ConfigFromEnv(c.getenv)
}

// SecureCookies is SECURE_COOKIES (a boolean, false by default).
func (c *Config) SecureCookies() bool {
	return c.boolean("SECURE_COOKIES", false)
}

// CrossSiteCookies is CROSS_SITE_COOKIES (a boolean, false by default).
func (c *Config) CrossSiteCookies() bool {
	return c.boolean("CROSS_SITE_COOKIES", false)
}

// HandlerFrontendURL is api.HandlerDeps.FrontendURL as stage handlers sets
// it: FRONTEND_URL, else PUBLIC_URL, else the dev frontend; the chain of
// EmailLinkBase, kept a field of its own (Q12).
func (c *Config) HandlerFrontendURL() string {
	return c.text("FRONTEND_URL", c.text("PUBLIC_URL", "http://localhost:3000"))
}

// PublicAPIURL is api.HandlerDeps.PublicAPIURL: PUBLIC_URL, else this
// server's localhost.
func (c *Config) PublicAPIURL() string {
	return c.text("PUBLIC_URL", "http://localhost:"+c.Port())
}

// ConnectorDistDir is where the connector downloads are served from
// (CONNECTOR_DIST_DIR, default ./dist).
func (c *Config) ConnectorDistDir() string {
	return c.text("CONNECTOR_DIST_DIR", "./dist")
}

// RateLimit is one of api.NewHandler's token buckets: its burst (a count)
// and its hourly refill (a rate), as newRateLimiterFromEnv reads them.
type RateLimit struct {
	Burst         int
	RefillPerHour float64
}

// RateLimits are api.NewHandler's thirteen limiters, in the order it builds
// them.
type RateLimits struct {
	InterviewMsg, InterviewIP, InterviewStream RateLimit
	AuthIP, AuthAccount, RegisterIP, SSOIP     RateLimit
	VerifyResend, PasswordReset, InvitePreview RateLimit
	BillingRefresh, BillingWrite, Invite       RateLimit
}

// RateLimits reads each limiter's burst, then its refill, with
// internal/api's defaults, in NewHandler's order.
func (c *Config) RateLimits() RateLimits {
	return RateLimits{
		InterviewMsg:    c.rateLimit("OPENV_INTERVIEW_MSG_BURST", "OPENV_INTERVIEW_MSG_REFILL_PER_HOUR", 5, 20),
		InterviewIP:     c.rateLimit("OPENV_INTERVIEW_IP_BURST", "OPENV_INTERVIEW_IP_REFILL_PER_HOUR", 20, 60),
		InterviewStream: c.rateLimit("OPENV_INTERVIEW_STREAM_BURST", "OPENV_INTERVIEW_STREAM_REFILL_PER_HOUR", 30, 120),
		AuthIP:          c.rateLimit("OPENV_AUTH_IP_BURST", "OPENV_AUTH_IP_REFILL_PER_HOUR", 30, 120),
		AuthAccount:     c.rateLimit("OPENV_AUTH_ACCOUNT_BURST", "OPENV_AUTH_ACCOUNT_REFILL_PER_HOUR", 5, 20),
		RegisterIP:      c.rateLimit("OPENV_REGISTER_IP_BURST", "OPENV_REGISTER_IP_REFILL_PER_HOUR", 5, 10),
		SSOIP:           c.rateLimit("OPENV_SSO_IP_BURST", "OPENV_SSO_IP_REFILL_PER_HOUR", 20, 60),
		VerifyResend:    c.rateLimit("OPENV_VERIFY_RESEND_BURST", "OPENV_VERIFY_RESEND_REFILL_PER_HOUR", 3, 6),
		PasswordReset:   c.rateLimit("OPENV_PASSWORD_RESET_BURST", "OPENV_PASSWORD_RESET_REFILL_PER_HOUR", 3, 6),
		InvitePreview:   c.rateLimit("OPENV_INVITE_PREVIEW_BURST", "OPENV_INVITE_PREVIEW_REFILL_PER_HOUR", 60, 240),
		BillingRefresh:  c.rateLimit("OPENV_BILLING_REFRESH_BURST", "OPENV_BILLING_REFRESH_REFILL_PER_HOUR", 10, 120),
		BillingWrite:    c.rateLimit("OPENV_BILLING_WRITE_BURST", "OPENV_BILLING_WRITE_REFILL_PER_HOUR", 5, 20),
		Invite:          c.rateLimit("OPENV_INVITE_BURST", "OPENV_INVITE_REFILL_PER_HOUR", 20, 60),
	}
}

// rateLimit is newRateLimiterFromEnv's read: the burst, a count, then the
// refill, a positive finite rate, each falling back to its default.
func (c *Config) rateLimit(burstVar, refillVar string, defBurst int, defRefill float64) RateLimit {
	return RateLimit{
		Burst:         envparse.Count(burstVar, c.getenv(burstVar), defBurst),
		RefillPerHour: envparse.Rate(refillVar, c.getenv(refillVar), defRefill),
	}
}

// MetricsToken is the bearer token /metrics requires (OPENV_METRICS_TOKEN,
// a credential), "" for none.
func (c *Config) MetricsToken() string {
	return c.secret("OPENV_METRICS_TOKEN", "")
}

// CORSOrigin is the frontend origin the API answers with credentials
// (CORS_ORIGIN, default the dev frontend). api.CORSMiddleware refuses a
// wildcard or null, which stage server makes fatal ("invalid CORS_ORIGIN"),
// as today.
func (c *Config) CORSOrigin() string {
	return c.text("CORS_ORIGIN", "http://localhost:3000")
}

// MaxBodyBytes is the cap on a request body, maxRequestBodyBytes's value:
// OPENV_MAX_BODY_MB (a count, 32 by default) mebibytes, and the default for
// a count whose bytes do not fit in an int64, with a warning.
func (c *Config) MaxBodyBytes() int64 {
	return int64(envparse.Mebibytes("OPENV_MAX_BODY_MB", c.count("OPENV_MAX_BODY_MB", 32), 32)) * 1024 * 1024
}
