package quality

import (
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// linted is a requirement whose body is whatever the case under test needs.
func linted(ref, body string) *artifacts.Artifact {
	return &artifacts.Artifact{
		ID:    "self",
		Ref:   ref,
		Type:  artifacts.TypeRequirement,
		Title: "Noise limit",
		Body:  body,
	}
}

func linkedTo(refs ...string) Context {
	set := map[string]bool{}
	for _, r := range refs {
		set[strings.ToUpper(r)] = true
	}
	return Context{LinkedRefs: set, LinksKnown: true}
}

// findingsFor returns only the unlinked-citation findings, so an unrelated
// wording rule firing on the fixture cannot make one of these tests pass.
func findingsFor(a *artifacts.Artifact, ctx Context) []Finding {
	var out []Finding
	for _, f := range LintArtifact(a, DefaultRuleSet(), ctx).Findings {
		if f.Rule == RuleUnlinkedCitation {
			out = append(out, f)
		}
	}
	return out
}

func TestUnlinkedCitationFlagsAnArtifactWithNoLink(t *testing.T) {
	a := linted("REQ-1", "The system shall meet ##REQ-99 in full.")
	got := findingsFor(a, linkedTo())
	if len(got) != 1 {
		t.Fatalf("findings = %+v, want one", got)
	}
	if got[0].Severity != SeverityError {
		t.Errorf("severity = %q, want %q — an untraceable citation is not a style point",
			got[0].Severity, SeverityError)
	}
	if got[0].Match != "##REQ-99" {
		t.Errorf("match = %q, want the citation as written", got[0].Match)
	}
	if !strings.Contains(got[0].Message, "REQ-99") {
		t.Errorf("message = %q, want it to name the artifact", got[0].Message)
	}
}

func TestUnlinkedCitationStaysQuietWhenLinked(t *testing.T) {
	a := linted("REQ-1", "The system shall meet ##REQ-99 in full.")
	if got := findingsFor(a, linkedTo("REQ-99")); len(got) != 0 {
		t.Errorf("findings = %+v, want none: the link is there", got)
	}
}

// The single marker comes from a menu that only offers linked artifacts, but
// the citation outlives the link: delete the link and the text still claims
// one. That is exactly when the warning earns its keep.
func TestUnlinkedCitationCatchesTheSingleMarkerToo(t *testing.T) {
	a := linted("REQ-1", "The system shall refine #REQ-99.")
	if got := findingsFor(a, linkedTo()); len(got) != 1 {
		t.Errorf("findings = %+v, want one", got)
	}
}

// Pointing a reader at a drawing asserts nothing about how two artifacts
// relate, so it needs no link to justify it.
func TestUnlinkedCitationIgnoresFigures(t *testing.T) {
	a := linted("REQ-1", "The system shall match ##REQ-99-FIG-2 and #REQ-1-FIG-1.")
	if got := findingsFor(a, linkedTo()); len(got) != 0 {
		t.Errorf("findings = %+v, want none: a figure citation is evidence, not a claim", got)
	}
}

func TestUnlinkedCitationIgnoresTheArtifactsOwnRef(t *testing.T) {
	a := linted("REQ-1", "The system shall supersede ##REQ-1.")
	if got := findingsFor(a, linkedTo()); len(got) != 0 {
		t.Errorf("findings = %+v, want none: an artifact is not unlinked from itself", got)
	}
}

func TestUnlinkedCitationReportsEachArtifactOnce(t *testing.T) {
	a := linted("REQ-1", "See ##REQ-99, and again ##REQ-99, and also ##REQ-50.")
	got := findingsFor(a, linkedTo())
	if len(got) != 2 {
		t.Fatalf("findings = %+v, want one per artifact", got)
	}
}

// "We did not look" must not read as "nothing is linked": a caller that could
// not read the links would otherwise flag every citation in the project.
func TestUnlinkedCitationSilentWhenLinksAreUnknown(t *testing.T) {
	a := linted("REQ-1", "The system shall meet ##REQ-99 in full.")
	if got := findingsFor(a, Context{}); len(got) != 0 {
		t.Errorf("findings = %+v, want none when the links were never read", got)
	}
}

func TestUnlinkedCitationLeavesOrdinaryTextAlone(t *testing.T) {
	a := linted("REQ-1", "# Heading\n\nThe system shall log issue PR#12 and ISO-9001 conformance.")
	if got := findingsFor(a, linkedTo()); len(got) != 0 {
		t.Errorf("findings = %+v, want none: none of that is a citation", got)
	}
}

func TestUnlinkedCitationCanBeTurnedOff(t *testing.T) {
	rs := DefaultRuleSet()
	rs.Severities[RuleUnlinkedCitation] = SeverityOff
	a := linted("REQ-1", "The system shall meet ##REQ-99 in full.")
	for _, f := range LintArtifact(a, rs, linkedTo()).Findings {
		if f.Rule == RuleUnlinkedCitation {
			t.Fatalf("rule fired while switched off: %+v", f)
		}
	}
}

func TestLinkedRefsByArtifactReadsBothDirections(t *testing.T) {
	export := &exports.ProjectExport{
		Artifacts: []*artifacts.Artifact{
			{ID: "a", Ref: "REQ-1", Type: artifacts.TypeRequirement},
			{ID: "b", Ref: "REQ-2", Type: artifacts.TypeRequirement},
			{ID: "c", Ref: "REQ-3", Type: artifacts.TypeRequirement},
		},
		Links: []*links.Link{
			{FromID: "a", ToID: "b", Type: "refines"},
			{FromID: "c", ToID: "a", Type: "verifies"},
		},
	}
	got := LinkedRefsByArtifact(export)
	if !got["a"]["REQ-2"] {
		t.Error("an outgoing link should count")
	}
	if !got["a"]["REQ-3"] {
		t.Error("an incoming link should count")
	}
	if got["b"]["REQ-3"] {
		t.Error("b and c are not linked to each other")
	}
}

// The project report and the single-artifact endpoint must agree, so the
// whole-project path has to supply the links rather than lint blind.
func TestLintProjectJudgesCitationsAgainstTheLinkGraph(t *testing.T) {
	export := &exports.ProjectExport{
		ProjectID: "p1",
		Artifacts: []*artifacts.Artifact{
			{ID: "a", Ref: "REQ-1", Type: artifacts.TypeRequirement,
				Title: "Noise limit", Body: "The system shall meet ##REQ-2 and ##REQ-3."},
			{ID: "b", Ref: "REQ-2", Type: artifacts.TypeRequirement, Title: "Seal", Body: "The system shall seal."},
			{ID: "c", Ref: "REQ-3", Type: artifacts.TypeRequirement, Title: "Mass", Body: "The system shall weigh 2 kg."},
		},
		Links: []*links.Link{{FromID: "a", ToID: "b", Type: "refines"}},
	}
	report := LintProject(export, DefaultRuleSet())
	var flagged []string
	for _, entry := range report.Entries {
		for _, f := range entry.Findings {
			if f.Rule == RuleUnlinkedCitation {
				flagged = append(flagged, f.Match)
			}
		}
	}
	if len(flagged) != 1 || flagged[0] != "##REQ-3" {
		t.Errorf("flagged = %v, want only ##REQ-3 — REQ-2 is linked", flagged)
	}
}
