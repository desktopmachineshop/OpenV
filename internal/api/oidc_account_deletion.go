package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Removing an account at the single sign-on provider's request: DMSid's
// account-deletion protocol (dms-id docs/adr/0002-account-deletion.md),
// modelled on OpenID Connect Back-Channel Logout. The person asks their
// DMSid page to remove them from OpenV; DMSid POSTs a deletion token signed
// with the keys OpenV already trusts for ID tokens, and OpenV answers what
// it did. Server to server: no session, no cookie.

// accountDeletionTokenType is the JWT header typ a deletion token carries.
const accountDeletionTokenType = "account-deletion+jwt"

// accountDeletionEvent is the events member DMSid puts in a deletion token,
// keyed by DMSid's own issuer. A deployment trusting another issuer accepts
// the same path under that issuer too (accountDeletionEventKeys).
const accountDeletionEvent = "https://id.desktopmachineshop.com/events/account-deletion"

// accountDeletionSkew is the clock skew a deletion token's exp is allowed.
const accountDeletionSkew = time.Minute

// accountDeletionMaxBody bounds the form a deletion request posts.
const accountDeletionMaxBody = 64 << 10

// verifyDeletionToken checks a deletion token as the ADR's step 1 asks:
// the signature against the issuer's JWKS, iss, aud equal to the client id,
// exp with at most a minute of skew (go-oidc's verifier), the header's typ
// and the events member; and answers the address it names.
func (c *OIDCConfig) verifyDeletionToken(ctx context.Context, raw string) (string, error) {
	if err := checkDeletionTokenType(raw); err != nil {
		return "", err
	}
	verifier := c.provider.Verifier(&oidc.Config{
		ClientID: c.ClientID,
		// exp is compared with this clock: a minute behind, so a token
		// expired by less than the skew still verifies.
		Now: func() time.Time { return time.Now().Add(-accountDeletionSkew) },
	})
	tok, err := verifier.Verify(ctx, raw)
	if err != nil {
		return "", err
	}
	var claims struct {
		Email  string                     `json:"email"`
		Events map[string]json.RawMessage `json:"events"`
	}
	if err := tok.Claims(&claims); err != nil {
		return "", err
	}
	found := false
	for _, key := range c.accountDeletionEventKeys() {
		if _, ok := claims.Events[key]; ok {
			found = true
		}
	}
	if !found {
		return "", errDeletionToken("the token carries no account-deletion event")
	}
	if strings.TrimSpace(claims.Email) == "" {
		return "", errDeletionToken("the token names no address")
	}
	return claims.Email, nil
}

// accountDeletionEventKeys are the events members a deletion token may
// carry: DMSid's, and the same path under the configured issuer.
func (c *OIDCConfig) accountDeletionEventKeys() []string {
	return []string{accountDeletionEvent, strings.TrimRight(c.Issuer, "/") + "/events/account-deletion"}
}

// checkDeletionTokenType reads the JWT header's typ before the signature is
// verified, so an ID token (typ JWT) can never be replayed as a deletion
// request: its typ differs.
func checkDeletionTokenType(raw string) error {
	head, _, ok := strings.Cut(raw, ".")
	if !ok {
		return errDeletionToken("not a JWT")
	}
	b, err := base64.RawURLEncoding.DecodeString(head)
	if err != nil {
		return errDeletionToken("unreadable header")
	}
	var header struct {
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(b, &header); err != nil {
		return errDeletionToken("unreadable header")
	}
	typ := strings.ToLower(header.Typ)
	if typ != accountDeletionTokenType && typ != "application/"+accountDeletionTokenType {
		return errDeletionToken("wrong typ " + header.Typ)
	}
	return nil
}

type errDeletionToken string

func (e errDeletionToken) Error() string { return string(e) }
