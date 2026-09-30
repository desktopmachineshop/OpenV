// Package envparse reads the value of an environment setting by the one rule
// every setting of the server and of agentd follows (issue #379, question
// 15):
//
//   - the value is trimmed, and a blank one is an unset one;
//   - a count is a whole number above 0;
//   - a duration is positive;
//   - a rate is a positive, finite number;
//   - a boolean is true or false in any case, or 1 or 0.
//
// A value that breaks the rule reads as the setting's default, and the log
// carries one warning that names the variable, never its value: a secret
// pasted into the wrong variable must not reach a log line (REQ-97).
//
// A credential is the exception (question 24): a key, token, password or
// private key is used exactly as set, never trimmed, and one with spaces or
// a line break around it is named in one warning, again without its value
// (Secret).
//
// The package reads no environment itself. Each caller reads its variable
// where it always has, which keeps S8's inventory of who reads what and a
// per-request read per request, and hands the raw value over.
package envparse

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

// What each kind of setting must be, as the warning says it.
const (
	wantCount    = "a whole number above 0"
	wantDuration = "a positive duration, such as 90m or 24h"
	wantRate     = "a positive, finite number"
	wantBool     = "true or false (any case), or 1 or 0"
	wantNumber   = "a whole number"
)

// Secret returns raw exactly as set: a credential is not trimmed, since a
// key, token, password or private key with its spaces cut off is another
// credential. When raw has spaces or a line break around it, or is only
// that, the log carries one warning naming the variable, never the value,
// so the operator can find why it is refused. Whether an empty value means
// unset is the caller's to say.
func Secret(name, raw string) string {
	if raw != "" && strings.TrimSpace(raw) != raw {
		warnSecret(name, raw)
	}
	return raw
}

// Text is raw trimmed, or def when that leaves nothing.
func Text(raw, def string) string {
	if v := strings.TrimSpace(raw); v != "" {
		return v
	}
	return def
}

// WholeNumber parses raw, trimmed, as a base-10 whole number: an optional
// sign and digits, nothing before or after them. It is false for a blank
// value and for anything else, 7x, 1e3 and 2.5 among them. It logs nothing,
// for a caller that refuses a malformed value rather than falling back
// (billing's counts).
func WholeNumber(raw string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	return n, err == nil
}

// Count reads a whole number above 0, or def.
func Count(name, raw string, def int) int {
	if strings.TrimSpace(raw) == "" {
		return def
	}
	if n, ok := WholeNumber(raw); ok && n > 0 {
		return n
	}
	warn(name, raw, wantCount)
	return def
}

// Number reads a whole number of any sign, or def. It is for the one
// setting whose zero and negatives have a meaning of their own
// (HOSTED_RUNNER_PIDS_LIMIT: no cap); every other count is a Count.
func Number(name, raw string, def int) int {
	if strings.TrimSpace(raw) == "" {
		return def
	}
	if n, ok := WholeNumber(raw); ok {
		return n
	}
	warn(name, raw, wantNumber)
	return def
}

// Duration reads a positive Go duration (90m, 24h, 1h30m), or def.
func Duration(name, raw string, def time.Duration) time.Duration {
	v := strings.TrimSpace(raw)
	if v == "" {
		return def
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d
	}
	warn(name, raw, wantDuration)
	return def
}

// Rate reads a positive, finite number (2.5 and 1e3 included), or def.
// Inf and NaN are not rates: an infinite refill would switch a rate limit
// off.
func Rate(name, raw string, def float64) float64 {
	v := strings.TrimSpace(raw)
	if v == "" {
		return def
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && !math.IsInf(f, 0) {
		return f
	}
	warn(name, raw, wantRate)
	return def
}

// Bool reads true or false in any case, or 1 or 0, or def.
func Bool(name, raw string, def bool) bool {
	v := strings.TrimSpace(raw)
	switch {
	case v == "":
		return def
	case v == "1" || strings.EqualFold(v, "true"):
		return true
	case v == "0" || strings.EqualFold(v, "false"):
		return false
	}
	warn(name, raw, wantBool)
	return def
}

// warned holds the variable and value pairs already warned about, so that a
// setting read more than once (at boot and again, or on every request)
// warns once.
var warned sync.Map

// warn logs, once per variable and value, that the variable's value breaks
// the rule and its default applies. The value is left out on purpose.
func warn(name, raw, want string) {
	if _, seen := warned.LoadOrStore(name+"\x00"+raw, true); seen {
		return
	}
	slog.Warn("ignoring a malformed setting; its default applies", "var", name, "want", want)
}

// warnSecret logs, once per variable and value, that a credential has spaces
// or a line break around it. It dedupes as warn does, but on a digest of the
// value, so the set of warnings keeps no copy of a credential.
func warnSecret(name, raw string) {
	sum := sha256.Sum256([]byte(raw))
	if _, seen := warned.LoadOrStore("secret\x00"+name+"\x00"+hex.EncodeToString(sum[:]), true); seen {
		return
	}
	slog.Warn("a credential setting has spaces or a line break around it; it is used exactly as set", "var", name)
}
