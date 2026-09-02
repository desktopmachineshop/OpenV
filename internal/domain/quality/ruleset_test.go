package quality

import (
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
)

func req(title, body string) *artifacts.Artifact {
	return &artifacts.Artifact{ID: "r1", Type: artifacts.TypeRequirement, Title: title, Body: body}
}

func mkExport(arts ...*artifacts.Artifact) *exports.ProjectExport {
	return &exports.ProjectExport{ProjectID: "p1", Artifacts: arts}
}

func rules(findings []Finding, rule string) []Finding {
	var out []Finding
	for _, f := range findings {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

// TestConventionDecidesWeakWords is the whole point of the rule set: the same
// sentence is house style under one convention and a finding under the other.
func TestConventionDecidesWeakWords(t *testing.T) {
	art := req("Retry", "The runner should retry a failed claim within 30 seconds.")

	shall := LintArtifact(art, DefaultRuleSet())
	if len(rules(shall.Findings, RuleWeakWord)) == 0 {
		t.Error("under 'shall', 'should' must read as weak wording")
	}

	rfc := DefaultRuleSet()
	rfc.Convention = ConventionRFC2119
	got := LintArtifact(art, rfc)
	if n := len(rules(got.Findings, RuleWeakWord)); n != 0 {
		t.Errorf("under RFC 2119, 'should' is normative; got %d weak-word findings", n)
	}
	if len(rules(got.Findings, RuleNotTestable)) != 0 {
		t.Error("under RFC 2119, 'should' is an imperative — not-testable must not fire")
	}
}

// TestOffConventionFlagsTheOtherVocabulary covers the rule that keeps one
// project from drifting between the two wordings.
func TestOffConventionFlagsTheOtherVocabulary(t *testing.T) {
	rfc := DefaultRuleSet()
	rfc.Convention = ConventionRFC2119

	cases := []struct {
		name string
		rs   RuleSet
		body string
		want bool
	}{
		{"must under shall", DefaultRuleSet(), "The system must lock the account after 3 attempts.", true},
		{"shall under shall", DefaultRuleSet(), "The system shall lock the account after 3 attempts.", false},
		{"shall under rfc2119", rfc, "The system shall lock the account after 3 attempts.", true},
		{"must under rfc2119", rfc, "The system MUST lock the account after 3 attempts.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := len(rules(LintArtifact(req("Lockout", tc.body), tc.rs).Findings, RuleOffConvention)) > 0
			if got != tc.want {
				t.Errorf("off-convention fired = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSeverityOverridesAndOff covers re-grading and disabling a rule.
func TestSeverityOverridesAndOff(t *testing.T) {
	art := req("UX", "The interface shall be intuitive for 3 operators.")

	base := LintArtifact(art, DefaultRuleSet())
	if len(rules(base.Findings, RuleWeakWord)) == 0 {
		t.Fatal("expected a weak-word finding to re-grade")
	}

	regraded := DefaultRuleSet()
	regraded.Severities[RuleWeakWord] = SeverityError
	got := LintArtifact(art, regraded)
	for _, f := range rules(got.Findings, RuleWeakWord) {
		if f.Severity != SeverityError {
			t.Errorf("severity = %q, want error", f.Severity)
		}
	}
	if got.Score >= base.Score {
		t.Errorf("re-grading up must cost more score: %d vs %d", got.Score, base.Score)
	}

	off := DefaultRuleSet()
	off.Severities[RuleWeakWord] = SeverityOff
	if n := len(rules(LintArtifact(art, off).Findings, RuleWeakWord)); n != 0 {
		t.Errorf("disabled rule still produced %d findings", n)
	}
}

// TestResolveLayersDefaultsWorkspaceProject covers the inheritance the whole
// feature rests on: each level overrides only what it names.
func TestResolveLayersDefaultsWorkspaceProject(t *testing.T) {
	org := map[string]interface{}{"quality": map[string]interface{}{
		"convention": ConventionRFC2119,
		"severities": map[string]interface{}{RuleLongSentence: SeverityOff},
	}}
	project := map[string]interface{}{"quality": map[string]interface{}{
		"severities": map[string]interface{}{RulePassiveVoice: SeverityInfo},
	}}

	got := Resolve(org, project)
	if got.Convention != ConventionRFC2119 {
		t.Errorf("convention = %q, want the workspace's %q", got.Convention, ConventionRFC2119)
	}
	if got.SeverityFor(RuleLongSentence) != SeverityOff {
		t.Error("workspace's disabled rule was lost")
	}
	if got.SeverityFor(RulePassiveVoice) != SeverityInfo {
		t.Error("project override was lost")
	}
	if got.SeverityFor(RulePlaceholder) != SeverityError {
		t.Error("untouched rule must keep its default severity")
	}

	if plain := Resolve(nil, nil); plain.Convention != ConventionShall {
		t.Errorf("nothing set anywhere = %q, want %q", plain.Convention, ConventionShall)
	}
}

// TestFromSettingsIgnoresJunk keeps a hand-edited or corrupt settings row from
// breaking linting: unknown keys and values are dropped, not fatal.
func TestFromSettingsIgnoresJunk(t *testing.T) {
	rs, ok := FromSettings(map[string]interface{}{"quality": map[string]interface{}{
		"convention": "klingon",
		"severities": map[string]interface{}{
			"no-such-rule":  SeverityError,
			RuleWeakWord:    "shouty",
			RulePlaceholder: SeverityInfo,
		},
	}})
	if !ok {
		t.Fatal("a set with one usable key must resolve")
	}
	if rs.Convention != "" {
		t.Errorf("unknown convention should be dropped, got %q", rs.Convention)
	}
	if _, bad := rs.Severities["no-such-rule"]; bad {
		t.Error("unknown rule was kept")
	}
	if _, bad := rs.Severities[RuleWeakWord]; bad {
		t.Error("unknown severity was kept")
	}
	if rs.Severities[RulePlaceholder] != SeverityInfo {
		t.Error("the one valid override was dropped")
	}

	if _, ok := FromSettings(map[string]interface{}{}); ok {
		t.Error("empty settings must report nothing set")
	}
}

// TestValidateRejectsBadInput covers what the API refuses to store.
func TestValidateRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		rs   RuleSet
		want string
	}{
		{"empty is inherit", RuleSet{}, ""},
		{"good", RuleSet{Convention: ConventionRFC2119, Severities: map[string]string{RuleWeakWord: SeverityOff}}, ""},
		{"bad convention", RuleSet{Convention: "iso-9001"}, "unknown convention"},
		{"bad rule", RuleSet{Severities: map[string]string{"vibes": SeverityError}}, "unknown rule"},
		{"bad severity", RuleSet{Severities: map[string]string{RuleWeakWord: "loud"}}, "unknown severity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.rs)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("err = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

// TestRoundTripThroughSettings covers storage: only what a level sets is
// written, so an override stays a diff rather than a full copy.
func TestRoundTripThroughSettings(t *testing.T) {
	rs := RuleSet{Convention: ConventionRFC2119, Severities: map[string]string{RuleWeakWord: SeverityOff}}
	stored := map[string]interface{}{SettingsKey: ToSettings(rs)}
	back, ok := FromSettings(stored)
	if !ok || back.Convention != rs.Convention || back.Severities[RuleWeakWord] != SeverityOff {
		t.Fatalf("round trip lost data: %+v", back)
	}
	if len(ToSettings(RuleSet{})) != 0 {
		t.Error("an empty rule set must store nothing, so the level inherits")
	}
}

// TestDescribeIsAgentReadable checks the sentence agents get before drafting.
func TestDescribeIsAgentReadable(t *testing.T) {
	rs := DefaultRuleSet()
	if got := rs.Describe(); !strings.Contains(got, "shall") {
		t.Errorf("default description does not name the convention: %q", got)
	}
	rs.Severities[RuleLongSentence] = SeverityOff
	got := rs.Describe()
	if !strings.Contains(got, RuleLongSentence) || !strings.Contains(got, SeverityOff) {
		t.Errorf("adjusted rule missing from description: %q", got)
	}
}

// TestReportCarriesRuleSet keeps a report readable next to the rules that
// produced it.
func TestReportCarriesRuleSet(t *testing.T) {
	rs := DefaultRuleSet()
	rs.Convention = ConventionRFC2119
	report := LintProject(mkExport(req("A", "The system MUST answer within 200 ms.")), rs)
	if report.RuleSet.Convention != ConventionRFC2119 {
		t.Errorf("report rule set = %+v, want the one it was linted with", report.RuleSet)
	}
}
