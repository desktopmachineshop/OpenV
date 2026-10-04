package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
)

// TestClassifySite locks every runner finish site to its error class. Adding a
// finish site without a class (or changing a mapping) must update this table.
func TestClassifySite(t *testing.T) {
	cases := []struct {
		name    string
		site    finishSite
		waitErr error
		want    string
	}{
		{"no adapter", siteNoAdapter, nil, agentruns.ErrorClassProviderUnavailable},
		{"workspace prep", siteWorkspacePrep, nil, agentruns.ErrorClassWorkspace},
		{"start transition", siteStartTransition, nil, agentruns.ErrorClassWorkerError},
		{"api key missing", siteAPIKeyMissing, nil, agentruns.ErrorClassAuth},
		{"adapter start", siteAdapterStart, nil, agentruns.ErrorClassProviderUnavailable},
		{"timeout", siteTimeout, context.DeadlineExceeded, agentruns.ErrorClassTimeout},
		{"panic", sitePanic, nil, agentruns.ErrorClassWorkerError},
		{"agent generic failure", siteAgentResult, errors.New("exit status 1: something broke"), agentruns.ErrorClassAgentError},
		{"agent auth failure", siteAgentResult, errors.New("Error: 401 Unauthorized"), agentruns.ErrorClassAuth},
		{"agent provider outage", siteAgentResult, errors.New("api error 529: overloaded_error"), agentruns.ErrorClassProviderUnavailable},
		{"agent exits non-zero with no error", siteAgentExit, nil, agentruns.ErrorClassAgentError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifySite(tc.site, tc.waitErr); got != tc.want {
				t.Errorf("classifySite(%v) = %q, want %q", tc.site, got, tc.want)
			}
		})
	}
}

// TestClassifyAgentError exercises the CLI-output heuristic that separates an
// auth or provider problem the agent surfaced from a genuine agent error.
func TestClassifyAgentError(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"", ""},
		{"invalid API key provided", agentruns.ErrorClassAuth},
		{"you are not logged in; run `claude` to sign in", agentruns.ErrorClassAuth},
		{"403 Forbidden", agentruns.ErrorClassAuth},
		{"429 Too Many Requests: rate limit exceeded", agentruns.ErrorClassProviderUnavailable},
		{"upstream returned 503 Service Unavailable", agentruns.ErrorClassProviderUnavailable},
		{"dial tcp: connection refused", agentruns.ErrorClassProviderUnavailable},
		{"tool 'Bash' failed: file not found", agentruns.ErrorClassAgentError},
		{"the model produced no answer", agentruns.ErrorClassAgentError},

		// #379 question 26 (bug 56): a status code counts only as a whole
		// number next to an HTTP word, or as the whole text; a word only as a
		// whole word; and a rate limit or overload outranks a mention of an
		// API key.
		{"tool Read failed: wrote 403 lines", agentruns.ErrorClassAgentError},
		{"processed 1500 files before the tool failed", agentruns.ErrorClassAgentError},
		{"the network tool is disabled for this agent", agentruns.ErrorClassAgentError},
		{"rate limit exceeded; check your API key", agentruns.ErrorClassProviderUnavailable},
		{"401", agentruns.ErrorClassAuth},
		{"HTTP 403", agentruns.ErrorClassAuth},
		{"HTTP/1.1 403", agentruns.ErrorClassAuth},
		{"upstream answered status 503", agentruns.ErrorClassProviderUnavailable},
		{`{"status_code": 502}`, agentruns.ErrorClassProviderUnavailable},
		{"API Error: 429", agentruns.ErrorClassProviderUnavailable},
		{"error 4290 while parsing", agentruns.ErrorClassAgentError},
		{"read 401 files", agentruns.ErrorClassAgentError},
		{"api error 529: overloaded_error", agentruns.ErrorClassProviderUnavailable},
		{"the forbiddenfruit tool failed", agentruns.ErrorClassAgentError},
		{"Credentials file is missing", agentruns.ErrorClassAuth},
		{"rpc error: code = Unauthenticated", agentruns.ErrorClassAuth},
		{"dial tcp: connect: network is unreachable", agentruns.ErrorClassProviderUnavailable},
		{"Network error while streaming the response", agentruns.ErrorClassProviderUnavailable},
		{"quota exceeded; your API key has no credit left", agentruns.ErrorClassProviderUnavailable},
		{"overloaded: retry later, API key accepted", agentruns.ErrorClassProviderUnavailable},
		{"rate limit exceeded; invalid API key", agentruns.ErrorClassAuth},
		{"rate limit exceeded; not logged in", agentruns.ErrorClassAuth},
		// Inflected forms the substring match used to catch keep their class.
		{"you are being rate limited, slow down", agentruns.ErrorClassProviderUnavailable},
		{"request was rate-limited by the provider", agentruns.ErrorClassProviderUnavailable},
		{"rate limits reached; check your API keys", agentruns.ErrorClassProviderUnavailable},
		{"all quotas used for today", agentruns.ErrorClassProviderUnavailable},
		{"usage limits reached for this plan", agentruns.ErrorClassProviderUnavailable},
		{"no API keys configured for this provider", agentruns.ErrorClassAuth},
		{"network timeout talking to the model", agentruns.ErrorClassProviderUnavailable},
		{"503 Service Unavailable: check your API key", agentruns.ErrorClassAuth},
	}
	for _, tc := range cases {
		var err error
		if tc.msg != "" {
			err = errors.New(tc.msg)
		}
		if got := classifyAgentError(err); got != tc.want {
			t.Errorf("classifyAgentError(%q) = %q, want %q", tc.msg, got, tc.want)
		}
	}
}

// TestThrottleSignalsAreProviderSignals: a rate limit or overload outranks a
// mention of an API key only because it is a provider signal itself; one
// missing from providerSignals would turn that text into agent_error.
func TestThrottleSignalsAreProviderSignals(t *testing.T) {
	provider := map[string]bool{}
	for _, s := range providerSignals {
		provider[s] = true
	}
	for _, s := range throttleSignals {
		if !provider[s] {
			t.Errorf("throttle signal %q is not in providerSignals", s)
		}
	}
	found := false
	for _, s := range authSignals {
		found = found || s == apiKeyMention
	}
	if !found {
		t.Errorf("apiKeyMention %q is not in authSignals", apiKeyMention)
	}
}

// TestRetryableClassesAlignWithTaxonomy guards the retry contract: exactly the
// transient classes retry; auth, workspace and agent_error do not.
func TestRetryableClassesAlignWithTaxonomy(t *testing.T) {
	retryable := map[string]bool{
		agentruns.ErrorClassProviderUnavailable: true,
		agentruns.ErrorClassTimeout:             true,
		agentruns.ErrorClassWorkerError:         true,
	}
	notRetryable := []string{
		agentruns.ErrorClassAuth,
		agentruns.ErrorClassWorkspace,
		agentruns.ErrorClassAgentError,
		"",
	}
	for class := range retryable {
		if !agentruns.IsRetryableClass(class) {
			t.Errorf("IsRetryableClass(%q) = false, want true", class)
		}
	}
	for _, class := range notRetryable {
		if agentruns.IsRetryableClass(class) {
			t.Errorf("IsRetryableClass(%q) = true, want false", class)
		}
	}
}
