package quality

import (
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
)

func mkReq(id, title, body string) *artifacts.Artifact {
	return &artifacts.Artifact{
		ID:    id,
		Type:  artifacts.TypeRequirement,
		Title: title,
		Body:  body,
	}
}

// hasRule reports whether any finding carries the given rule id.
func hasRule(findings []Finding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func countRule(findings []Finding, rule string) int {
	n := 0
	for _, f := range findings {
		if f.Rule == rule {
			n++
		}
	}
	return n
}

// TestRulesFireOnBadExamples asserts each rule triggers on content crafted to
// violate exactly it (plus, unavoidably, the not-testable catch-all where the
// example carries no "shall"/measurable).
func TestRulesFireOnBadExamples(t *testing.T) {
	cases := []struct {
		name string
		art  *artifacts.Artifact
		rule string
	}{
		{
			name: "weak word",
			art:  mkReq("r1", "Performance", "The system shall be fast and user-friendly for 3 users."),
			rule: RuleWeakWord,
		},
		{
			name: "vague quantifier",
			art:  mkReq("r2", "Storage", "The system shall store several files for up to 5 users."),
			rule: RuleVagueQuantifier,
		},
		{
			name: "placeholder",
			art:  mkReq("r3", "Auth", "The login flow shall enforce 2 factors. TODO: define lockout."),
			rule: RulePlaceholder,
		},
		{
			name: "passive voice",
			art:  mkReq("r4", "Audit", "Each record shall be stored and the log is validated within 2 seconds."),
			rule: RulePassiveVoice,
		},
		{
			name: "not testable",
			art:  mkReq("r5", "Vision", "The product helps operators keep track of their day."),
			rule: RuleNotTestable,
		},
		{
			name: "long sentence",
			art: mkReq("r6", "Reporting",
				"The system shall generate a report that includes every single field from the order, the customer, the shipment, the invoice, the tax record, and any related note so nothing at all is ever missed by anyone."),
			rule: RuleLongSentence,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := LintArtifact(tc.art, DefaultRuleSet())
			if !hasRule(got.Findings, tc.rule) {
				t.Fatalf("expected rule %q to fire; findings: %+v", tc.rule, got.Findings)
			}
		})
	}
}

// TestCleanRequirementScoresHigh confirms a crisp, testable, measurable
// requirement passes cleanly with no findings and a perfect score.
func TestCleanRequirementScoresHigh(t *testing.T) {
	clean := mkReq("ok",
		"Render latency",
		"The system shall render the requirements list within 500 ms for projects of up to 5000 artifacts.")
	got := LintArtifact(clean, DefaultRuleSet())
	if len(got.Findings) != 0 {
		t.Fatalf("clean requirement produced findings: %+v", got.Findings)
	}
	if got.Score != 100 {
		t.Fatalf("clean requirement score = %d, want 100", got.Score)
	}
	if got.Band != BandGood {
		t.Fatalf("clean requirement band = %q, want %q", got.Band, BandGood)
	}
}

// TestScoreMonotonicAndBands checks the scoring deducts by severity and maps to
// the right band, and never goes below zero.
func TestScoreMonotonicAndBands(t *testing.T) {
	// Two weak words + passive voice + no measurable: several deductions.
	messy := mkReq("bad", "UX",
		"The interface should be intuitive and the layout should be adequate and pleasant for the user.")
	got := LintArtifact(messy, DefaultRuleSet())
	if got.Score >= 100 {
		t.Fatalf("messy requirement should lose points, got %d", got.Score)
	}
	if got.Score < 0 {
		t.Fatalf("score must clamp at 0, got %d", got.Score)
	}
	if Band(85) != BandGood || Band(60) != BandFair || Band(20) != BandPoor {
		t.Fatalf("band boundaries wrong: %q %q %q", Band(85), Band(60), Band(20))
	}
}

// TestDeterministic runs the linter twice and requires identical output,
// including finding order.
func TestDeterministic(t *testing.T) {
	art := mkReq("d1", "Mixed",
		"The system should quickly process several requests. TODO: confirm the exact limit.")
	a := LintArtifact(art, DefaultRuleSet())
	b := LintArtifact(art, DefaultRuleSet())
	if a.Score != b.Score || len(a.Findings) != len(b.Findings) {
		t.Fatalf("non-deterministic score/len: %+v vs %+v", a, b)
	}
	for i := range a.Findings {
		if a.Findings[i] != b.Findings[i] {
			t.Fatalf("finding %d differs: %+v vs %+v", i, a.Findings[i], b.Findings[i])
		}
	}
	// Findings must be sorted by start offset.
	for i := 1; i < len(a.Findings); i++ {
		if a.Findings[i].Start < a.Findings[i-1].Start {
			t.Fatalf("findings not sorted by offset at %d: %+v", i, a.Findings)
		}
	}
}

// TestFindingSpansPointAtMatch verifies reported offsets slice back to the
// matched text for span-bearing rules.
func TestFindingSpansPointAtMatch(t *testing.T) {
	art := mkReq("s1", "Speed", "The system shall be fast for 2 users.")
	text := combinedText(art.Title, art.Body)
	got := LintArtifact(art, DefaultRuleSet())
	var found bool
	for _, f := range got.Findings {
		if f.Rule == RuleWeakWord {
			found = true
			if f.Start < 0 || f.End > len(text) || f.Start >= f.End {
				t.Fatalf("bad span [%d,%d] over text len %d", f.Start, f.End, len(text))
			}
			if text[f.Start:f.End] != f.Match {
				t.Fatalf("span %q != match %q", text[f.Start:f.End], f.Match)
			}
		}
	}
	if !found {
		t.Fatal("expected a weak-word finding")
	}
}

// TestLintProjectSkipsNonRequirements confirms only requirement-ish types are
// scored and the band summary tallies.
func TestLintProjectSkipsNonRequirements(t *testing.T) {
	export := &exports.ProjectExport{
		ProjectID: "proj-1",
		Artifacts: []*artifacts.Artifact{
			mkReq("r1", "Good", "The system shall respond within 200 ms for 10 users."),
			{ID: "h1", Type: artifacts.TypeHeading, Title: "Section 1", Body: "should be ignored fast"},
			{ID: "tc1", Type: artifacts.TypeTestCase, Title: "TC", Body: "TODO ignore me"},
			{ID: "n1", Type: artifacts.TypeUserNeed, Title: "Need", Body: "The operator needs to see status quickly."},
		},
	}
	report := LintProject(export, DefaultRuleSet())
	if len(report.Entries) != 2 {
		t.Fatalf("expected 2 linted entries (requirement + user-need), got %d", len(report.Entries))
	}
	for _, e := range report.Entries {
		if e.ArtifactID == "h1" || e.ArtifactID == "tc1" {
			t.Fatalf("non-requirement type %q was linted", e.ArtifactID)
		}
	}
	total := report.Summary[BandGood] + report.Summary[BandFair] + report.Summary[BandPoor]
	if total != 2 {
		t.Fatalf("summary counts (%d) do not sum to entries (2)", total)
	}
}

// TestPlaceholderIsError locks in that placeholders carry the heaviest penalty.
func TestPlaceholderIsError(t *testing.T) {
	art := mkReq("p1", "Auth", "The system shall lock the account after 3 attempts. TBD threshold.")
	got := LintArtifact(art, DefaultRuleSet())
	if countRule(got.Findings, RulePlaceholder) == 0 {
		t.Fatal("expected a placeholder finding")
	}
	for _, f := range got.Findings {
		if f.Rule == RulePlaceholder && f.Severity != SeverityError {
			t.Fatalf("placeholder severity = %q, want %q", f.Severity, SeverityError)
		}
	}
}
