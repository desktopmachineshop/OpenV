// Package tokens mints the random tokens the platform hands out, and the
// hashed form that a session, a run, an email verification, a password
// reset, an invitation, an interview invite, a share link, a runner key or
// a pairing code stores in place of the token. It imports no package of
// this module, so any domain package may use it.
package tokens

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
