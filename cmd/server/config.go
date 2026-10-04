// The server's settings follow internal/envparse's one rule: a value is
// trimmed, and a malformed count or boolean reads as its fallback with one
// warning naming the variable; a credential is exact (envSecret, at the end).

package main

import (
	"os"

	"github.com/openv/requirements-platform/internal/envparse"
)

// envOr reads a text setting, trimmed, falling back when that leaves
// nothing.
func envOr(key, fallback string) string {
	return envparse.Text(os.Getenv(key), fallback)
}

// envInt reads a count, a whole number above 0.
func envInt(key string, fallback int) int {
	return envparse.Count(key, os.Getenv(key), fallback)
}

// envBool reads a boolean: true or false in any case, or 1 or 0.
func envBool(key string, fallback bool) bool {
	return envparse.Bool(key, os.Getenv(key), fallback)
}

// envSwitch reads a switch: on or off in any case, or a boolean as envBool
// reads one.
func envSwitch(key string, fallback bool) bool {
	return envparse.Switch(key, os.Getenv(key), fallback)
}

// envSecret reads a credential exactly as set (#379, question 24): never
// trimmed, since a key, token, password or private key cut short of its
// spaces is another one, with one warning naming the variable, never the
// value, when spaces or a line break sit around it. Only an unset or empty
// variable falls back.
func envSecret(key, fallback string) string {
	if v := envparse.Secret(key, os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
