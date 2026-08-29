package runner

import (
	"strings"

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
	// siteWorkspacePrep: PrepareWorkspace failed (clone/disk).
	siteWorkspacePrep
	// siteStartTransition: the API start transition failed (worker/API fault,
	// not the run's).
	siteStartTransition
	// siteAPIKeyMissing: the project uses api-key auth but the key env is unset
	// on the runner host.
	siteAPIKeyMissing
	// siteAdapterStart: the provider adapter failed to launch its CLI.
	siteAdapterStart
	// siteAgentResult: the run executed and handle.Wait returned an error — the
	// agent CLI itself failed. The underlying error is inspected to separate a
	// provider/auth failure surfaced through the CLI from a genuine agent error.
	siteAgentResult
	// siteTimeout: the run exceeded its deadline (adapter watchdog).
	siteTimeout
	// sitePanic: the worker panicked executing the run.
	sitePanic
)

// classifySite maps a terminal finish at the given site to an agentruns error
// class. For siteAgentResult the wait error is inspected; every other site maps
// to a fixed class.
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
		return agentruns.ErrorClassProviderUnavailable
	case siteTimeout:
		return agentruns.ErrorClassTimeout
	case sitePanic:
		return agentruns.ErrorClassWorkerError
	case siteAgentResult:
		return classifyAgentError(waitErr)
	default:
		return agentruns.ErrorClassWorkerError
	}
}

// authSignals are substrings in a CLI's failure text that mark a credential
// problem (retrying cannot fix it — auth is not retryable).
var authSignals = []string{
	"401", "403", "unauthorized", "forbidden",
	"authentication", "authenticated", "invalid api key", "invalid_api_key",
	"api key", "not logged in", "please log in", "please login",
	"login required", "sign in", "credential", "permission denied",
}

// providerSignals mark a transient provider-side failure surfaced through the
// CLI (rate limit, overload, upstream 5xx, network) — retryable.
var providerSignals = []string{
	"429", "rate limit", "rate_limit", "overloaded", "overload",
	"usage limit", "quota", "capacity",
	"500", "502", "503", "504", "bad gateway", "gateway timeout",
	"service unavailable", "server error", "internal server error",
	"temporarily unavailable", "connection refused", "connection reset",
	"econnrefused", "no route to host", "network",
}

// classifyAgentError inspects the error a provider CLI surfaced on a non-zero
// exit (or an error result) and buckets it. This is the small classify hook the
// runner applies to adapter OUTPUT — it never reaches into adapter internals.
// Anything not recognisably an auth or provider problem is a genuine
// agent_error (a deterministic failure that retrying would only repeat).
func classifyAgentError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	if containsAnySignal(msg, authSignals) {
		return agentruns.ErrorClassAuth
	}
	if containsAnySignal(msg, providerSignals) {
		return agentruns.ErrorClassProviderUnavailable
	}
	return agentruns.ErrorClassAgentError
}

func containsAnySignal(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
