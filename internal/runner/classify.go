package runner

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// finishSite identifies where in a run's lifecycle a terminal outcome was
// produced. Keeping the site -> error-class mapping in one place (classifySite)
// makes the taxonomy auditable and lets a table test lock every finish site's
// class down without driving a whole run.
type finishSite int

const (
	// siteNoAdapter: the worker has no adapter for the run's provider.
	siteNoAdapter finishSite = iota
	// siteWorkspacePrep: PrepareWorkspace failed (clone/disk), or a run with
	// repository access could not read its project's repository connections.
	siteWorkspacePrep
	// siteStartTransition: the API start transition failed (worker/API fault,
	// not the run's).
	siteStartTransition
	// siteAPIKeyMissing: the project uses api-key auth but the key env is unset
	// on the runner host.
	siteAPIKeyMissing
	// siteAdapterStart: the provider adapter failed to launch its CLI, or
	// refused the agent definition outright (ErrAgentPolicy — see
	// classifySite, which separates the two).
	siteAdapterStart
	// siteAgentResult: the run executed and handle.Wait returned an error — the
	// agent CLI itself failed. The underlying error is inspected to separate a
	// provider/auth failure surfaced through the CLI from a genuine agent error.
	siteAgentResult
	// siteAgentExit: the agent CLI exited with a non-zero code but handle.Wait
	// returned no error with it. The CLI failed, with no text to classify.
	siteAgentExit
	// siteTimeout: the run exceeded its deadline (adapter watchdog).
	siteTimeout
	// sitePanic: the worker panicked executing the run.
	sitePanic
	// siteAgentPolicy: the agent definition itself forbids the run — today,
	// an agent carrying no tool allowlist (REQ-91). Nothing was launched, and
	// retrying cannot help until someone edits the definition.
	siteAgentPolicy
)

// classifySite maps a terminal finish at the given site to an agentruns error
// class. Two sites inspect the error they are given — siteAgentResult (what
// the CLI failed with) and siteAdapterStart (whether the adapter refused the
// definition rather than failing to launch); every other site maps to a fixed
// class.
func classifySite(site finishSite, waitErr error) string {
	switch site {
	case siteNoAdapter:
		return agentruns.ErrorClassProviderUnavailable
	case siteWorkspacePrep:
		return agentruns.ErrorClassWorkspace
	case siteStartTransition:
		return agentruns.ErrorClassWorkerError
	case siteAPIKeyMissing:
		return agentruns.ErrorClassAuth
	case siteAdapterStart:
		// An adapter that refused the definition itself (no allowlist, repo
		// access on a CLI that cannot confine edits per tool) is not a
		// provider outage: the same run will be refused again, so it must not
		// be auto-retried. Everything else at this site is the CLI failing to
		// launch, which is.
		if errors.Is(waitErr, ErrAgentPolicy) {
			return agentruns.ErrorClassAgentError
		}
		return agentruns.ErrorClassProviderUnavailable
	case siteTimeout:
		return agentruns.ErrorClassTimeout
	case sitePanic:
		return agentruns.ErrorClassWorkerError
	case siteAgentPolicy:
		// Not retryable: the definition has to change first.
		return agentruns.ErrorClassAgentError
	case siteAgentExit:
		return agentruns.ErrorClassAgentError
	case siteAgentResult:
		return classifyAgentError(waitErr)
	default:
		return agentruns.ErrorClassWorkerError
	}
}

// authSignals are the words, phrases and status codes in a CLI's failure
// text that mark a credential problem (retrying cannot fix it — auth is not
// retryable). Matching is containsSignal's: whole words, and a status code
// only as an HTTP status.
var authSignals = []string{
	"401", "403", "unauthorized", "forbidden",
	"authentication", "authenticated", "unauthenticated", "invalid api key", "invalid_api_key",
	apiKeyMention, apiKeysMention, "not logged in", "please log in", "please login",
	"login required", "sign in", "credential", "credentials", "permission denied",
}

// apiKeyMention is the one auth signal that only mentions a credential
// rather than reporting it bad, so a throttleSignals match outranks it:
// "rate limit exceeded; check your API key" is the provider throttling,
// which a retry can outlast, not a key to fix.
const apiKeyMention = "api key"

// apiKeysMention is apiKeyMention's plural, which whole-word matching would
// otherwise miss: "check your API keys".
const apiKeysMention = "api keys"

// providerSignals mark a transient provider-side failure surfaced through the
// CLI (rate limit, overload, upstream 5xx, network) — retryable.
var providerSignals = []string{
	"429", "rate limit", "rate_limit", "overloaded", "overload",
	"usage limit", "quota", "capacity",
	// Inflected forms that whole-word matching would otherwise miss.
	"rate limited", "rate-limited", "rate limits", "usage limits", "quotas",
	"500", "502", "503", "504", "bad gateway", "gateway timeout",
	"service unavailable", "server error", "internal server error",
	"temporarily unavailable", "connection refused", "connection reset",
	"econnrefused", "no route to host", "network error", "network is unreachable",
	"network timeout", "network unreachable",
}

// throttleSignals are the providerSignals that say the provider is rate
// limiting or overloaded; one of them outranks an apiKeyMention.
var throttleSignals = []string{
	"429", "rate limit", "rate_limit", "overloaded", "overload", "usage limit", "quota", "capacity",
	"rate limited", "rate-limited", "rate limits", "usage limits", "quotas",
}

// classifyAgentError inspects the error a provider CLI surfaced on a non-zero
// exit (or an error result) and buckets it. This is the small classify hook the
// runner applies to adapter OUTPUT — it never reaches into adapter internals.
// Anything not recognisably an auth or provider problem is a genuine
// agent_error (a deterministic failure that retrying would only repeat).
// Auth signals are checked first, except that a rate limit or an overload
// outranks a mere mention of an API key.
func classifyAgentError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	throttled := containsAnySignal(msg, throttleSignals)
	for _, s := range authSignals {
		if (s == apiKeyMention || s == apiKeysMention) && throttled {
			continue
		}
		if containsSignal(msg, s) {
			return agentruns.ErrorClassAuth
		}
	}
	if containsAnySignal(msg, providerSignals) {
		return agentruns.ErrorClassProviderUnavailable
	}
	return agentruns.ErrorClassAgentError
}

func containsAnySignal(haystack string, needles []string) bool {
	for _, n := range needles {
		if containsSignal(haystack, n) {
			return true
		}
	}
	return false
}

// containsSignal reports whether the lower-cased failure text msg carries
// signal. A signal of digits is an HTTP status code: it counts only as the
// whole text ("401") or as a whole number next to an HTTP word ("HTTP 403",
// "HTTP/1.1 403", "status 503", "status_code: 502", "Error: 429"), never as
// a count ("wrote 403 lines", "processed 1500 files"). Any other signal
// counts only as whole words: a letter or digit on either side makes it part
// of a longer word ("forbiddenfruit"), while punctuation, including the
// underscore in "overloaded_error", does not.
func containsSignal(msg, signal string) bool {
	if isStatusCode(signal) {
		if strings.TrimSpace(msg) == signal {
			return true
		}
		for _, m := range httpStatusRE.FindAllStringSubmatchIndex(msg, -1) {
			if msg[m[2]:m[3]] == signal && !wordRuneAt(msg, m[3]) {
				return true
			}
		}
		return false
	}
	for from := 0; from < len(msg); {
		i := strings.Index(msg[from:], signal)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(signal)
		if !wordRuneBefore(msg, start) && !wordRuneAt(msg, end) {
			return true
		}
		from = start + 1
	}
	return false
}

// httpStatusRE finds a number right after an HTTP word: "http" (with an
// optional protocol version), "status" or "error", each optionally followed
// by "code", then up to three separators.
var httpStatusRE = regexp.MustCompile(`(?:^|[^\pL\pN])(?:http(?:/[0-9.]+)?|status|error)(?:[ _]?code)?[^\pL\pN]{1,3}([0-9]+)`)

func isStatusCode(signal string) bool {
	for _, r := range signal {
		if r < '0' || r > '9' {
			return false
		}
	}
	return signal != ""
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func wordRuneBefore(s string, i int) bool {
	r, n := utf8.DecodeLastRuneInString(s[:i])
	return n > 0 && isWordRune(r)
}

func wordRuneAt(s string, i int) bool {
	r, n := utf8.DecodeRuneInString(s[i:])
	return n > 0 && isWordRune(r)
}
