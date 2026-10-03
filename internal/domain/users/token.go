// Tokens: minting one, and the hashed form that a session, a run, an
// email verification or a password reset stores in place of the token.

package users

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// HashToken returns the stored form of a session or run token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewToken generates a random URL-safe token.
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
