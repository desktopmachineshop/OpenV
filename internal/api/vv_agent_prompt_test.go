package api

import (
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

func testCase(id, title, method string) *artifacts.Artifact {
	attrs := map[string]interface{}{}
	if method != "" {
		attrs[vv.ExecutionMethodAttr] = method
	}
	return &artifacts.Artifact{ID: id, Title: title, Type: "test-case", Attributes: attrs}
}

func TestTestRunAgentPromptNamesRunnableCases(t *testing.T) {
	run := &vv.TestRun{ID: "run-1", ProjectID: "proj-1", Name: "Release smoke"}
	runnable := []*artifacts.Artifact{
		testCase("tc-1", "Login returns a session", ""),
		testCase("tc-2", "Export produces valid JSON", vv.ExecutionAutomated),
	}

	prompt := testRunAgentPrompt(run, runnable, nil)

	for _, want := range []string{
		"run-1",         // the run to record against
		"proj-1",        // the project scope
		"Release smoke", // human context
		"tc-1", "tc-2",  // ids the agent must record for
		"Login returns a session", // titles for readability
		"record_test_result",      // the tool to call
		"get_artifact",            // how to read the steps
		"blocked",                 // the honest-failure path
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q\n---\n%s", want, prompt)
		}
	}
}

func TestTestRunAgentPromptExcludesHumanVerifiedCases(t *testing.T) {
	run := &vv.TestRun{ID: "run-1", ProjectID: "proj-1", Name: "Release smoke"}
	runnable := []*artifacts.Artifact{testCase("tc-1", "Export produces valid JSON", "")}
	skipped := []*artifacts.Artifact{
		testCase("tc-9", "Enclosure survives drop test", vv.ExecutionPhysical),
		testCase("tc-8", "Onboarding copy reads clearly", vv.ExecutionManual),
	}

	prompt := testRunAgentPrompt(run, runnable, skipped)

	// Skipped cases are named so the agent knows to leave them alone, and the
	// instruction to do so must be explicit.
	for _, want := range []string{
		"Enclosure survives drop test",
		"Onboarding copy reads clearly",
		vv.ExecutionPhysical,
		vv.ExecutionManual,
		"do not attempt them",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q\n---\n%s", want, prompt)
		}
	}

	// The skipped cases' IDs must not appear: the agent should have no way to
	// address them via record_test_result.
	for _, id := range []string{"tc-9", "tc-8"} {
		if strings.Contains(prompt, id) {
			t.Errorf("prompt leaks the id of a human-verified case (%s)\n---\n%s", id, prompt)
		}
	}
}

func TestTestRunAgentPromptOmitsSkippedSectionWhenNoneSkipped(t *testing.T) {
	run := &vv.TestRun{ID: "run-1", ProjectID: "proj-1", Name: "Release smoke"}
	runnable := []*artifacts.Artifact{testCase("tc-1", "Export produces valid JSON", "")}

	prompt := testRunAgentPrompt(run, runnable, nil)

	if strings.Contains(prompt, "deliberately excluded") {
		t.Errorf("prompt mentions exclusions when nothing was skipped\n---\n%s", prompt)
	}
}
