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
