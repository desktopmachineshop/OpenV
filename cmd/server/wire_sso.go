package main

import (
	"os"
	"strings"

	"github.com/openv/requirements-platform/internal/api"
)

// sso reads the optional Google OAuth and OIDC single sign-on settings.
func (a *app) sso() {
	// Google OAuth (optional).
	if clientID := envOr("GOOGLE_CLIENT_ID", ""); clientID != "" {
		publicURL := envOr("PUBLIC_URL", "http://localhost:"+a.port)
		a.googleOAuth = &api.GoogleOAuthConfig{
			ClientID:     clientID,
			ClientSecret: envSecret("GOOGLE_CLIENT_SECRET", ""),
			RedirectURL:  publicURL + "/api/v1/auth/google/callback",
			FrontendURL:  envOr("FRONTEND_URL", "http://localhost:3000"),
		}
	}

	// Generic OIDC single sign-on (optional, issue #225). One IdP per
	// deployment; strictly opt-in — with OPENV_OIDC_ISSUER unset the endpoints
	// report "not configured" and the default deployment is unaffected.
	if issuer := envOr("OPENV_OIDC_ISSUER", ""); issuer != "" {
		publicURL := envOr("PUBLIC_URL", "http://localhost:"+a.port)
		redirectURL := envOr("OPENV_OIDC_REDIRECT_URL", publicURL+"/api/v1/auth/oidc/callback")
		var scopes []string
		if raw := os.Getenv("OPENV_OIDC_SCOPES"); raw != "" {
			scopes = strings.Fields(raw)
		}
		a.oidcConfig = &api.OIDCConfig{
			Issuer:       issuer,
			ClientID:     envOr("OPENV_OIDC_CLIENT_ID", ""),
			ClientSecret: envSecret("OPENV_OIDC_CLIENT_SECRET", ""),
			RedirectURL:  redirectURL,
			Scopes:       scopes,
			ProviderName: envOr("OPENV_OIDC_NAME", "SSO"),
			FrontendURL:  envOr("FRONTEND_URL", "http://localhost:3000"),
		}
	}
}
