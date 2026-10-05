// Tokens: minting one, and the hashed form that a session, a run, an
// email verification or a password reset stores in place of the token.
// Both live in internal/domain/tokens (refactor step P2b); these names
// delegate to it, so every caller of users.NewToken and users.HashToken
// keeps working unchanged.

package users

import "github.com/openv/requirements-platform/internal/domain/tokens"

// HashToken returns the stored form of a session or run token.
func HashToken(token string) string {
	return tokens.HashToken(token)
}

// NewToken generates a random URL-safe token.
func NewToken() (string, error) {
	return tokens.NewToken()
}
