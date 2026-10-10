package main

import (
	"github.com/openv/requirements-platform/internal/api"
)

// sso reads the optional Google OAuth and OIDC single sign-on settings.
func (a *app) sso() {
	cfg := a.env()
	// Google OAuth (optional): its settings are read only with
	// GOOGLE_CLIENT_ID set.
	if g := cfg.GoogleOAuth(); g != nil {
		a.googleOAuth = &api.GoogleOAuthConfig{
			ClientID:     g.ClientID,
			ClientSecret: g.ClientSecret,
			RedirectURL:  g.RedirectURL,
			FrontendURL:  g.FrontendURL,
		}
	}

	// Generic OIDC single sign-on (optional, issue #225). One IdP per
	// deployment; strictly opt-in — with OPENV_OIDC_ISSUER unset the endpoints
	// report "not configured" and the default deployment is unaffected, and
	// no other OIDC setting is read.
	if o := cfg.OIDC(); o != nil {
		a.oidcConfig = &api.OIDCConfig{
			Issuer:       o.Issuer,
			ClientID:     o.ClientID,
			ClientSecret: o.ClientSecret,
			RedirectURL:  o.RedirectURL,
			Scopes:       o.Scopes,
			ProviderName: o.ProviderName,
			FrontendURL:  o.FrontendURL,
		}
	}
}
