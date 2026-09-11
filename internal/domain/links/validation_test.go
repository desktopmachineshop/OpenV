package links

import (
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// The link rule table is the contract the API enforces on every link that is
// created (internal/api/handlers.go) and on every link an agent proposes
// (internal/api/proposal_appliers.go), and the frontend mirrors it in
// frontend/src/config/linkTypeRules.ts. These tests pin the table's
// behaviour rule by rule: every allowed from/to pair is accepted, every
// other artifact type in the catalog is rejected on both sides unless the
// rule wildcards it, and unknown link types are refused outright.

// wildcard is the "any artifact type" marker used in a rule's allowed lists.
const wildcard = "*"

// catalogTypes returns the whole artifact type vocabulary. The catalog in
// internal/domain/artifacts is the single Go-side source of truth for the
// type names the rules are written against, so the suite reads it rather
// than repeating the list: a type added there starts being exercised here.
func catalogTypes(t *testing.T) []string {
	t.Helper()
	defs := artifacts.TypeCatalog()
	if len(defs) == 0 {
		t.Fatal("artifact type catalog is empty")
	}
	values := make([]string, 0, len(defs))
	for _, def := range defs {
		values = append(values, def.Value)
	}
	return values
}

// hasWildcard reports whether an allowed list accepts every artifact type.
func hasWildcard(allowed []string) bool {
	for _, a := range allowed {
		if a == wildcard {
			return true
		}
	}
	return false
}

// expand resolves an allowed list to the concrete artifact types it permits:
// a wildcard list permits the whole catalog.
func expand(t *testing.T, allowed []string) []string {
	t.Helper()
	if hasWildcard(allowed) {
		return catalogTypes(t)
	}
	return allowed
}

func inList(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// TestRuleTableIsWellFormed guards the table itself: unique link types, no
// empty labels, and artifact types that actually exist in the catalog. A
// typo'd type name would otherwise make a rule silently unusable — nothing
// could ever satisfy it — while the per-rule tests below would still pass.
func TestRuleTableIsWellFormed(t *testing.T) {
	catalog := catalogTypes(t)
	seen := map[string]bool{}

	for _, rule := range linkTypeRules {
		if rule.Type == "" {
			t.Error("a rule has an empty link type")
			continue
		}
		if seen[rule.Type] {
			// ValidateLinkType matches the first rule with a given type,
			// so a duplicate would be dead configuration.
			t.Errorf("duplicate rule for link type %q", rule.Type)
		}
		seen[rule.Type] = true

		if rule.Label == "" || rule.InverseLabel == "" || rule.Description == "" {
			t.Errorf("rule %q is missing a label, inverse label or description", rule.Type)
		}
		if len(rule.AllowedFromTypes) == 0 || len(rule.AllowedToTypes) == 0 {
			t.Errorf("rule %q allows no artifact types on one of its ends", rule.Type)
		}
		for _, list := range [][]string{rule.AllowedFromTypes, rule.AllowedToTypes} {
			for _, value := range list {
				if value == wildcard {
					continue
				}
				if !inList(catalog, value) {
					t.Errorf("rule %q names artifact type %q, which is not in the artifact type catalog",
						rule.Type, value)
				}
			}
		}
	}
}

// TestGetLinkTypeRulesExposesTheTable checks the accessor the API meta
// endpoint and the ReqIF export read: it must hand back the same rules the
// validator enforces, not a copy that has drifted.
func TestGetLinkTypeRulesExposesTheTable(t *testing.T) {
	exposed := GetLinkTypeRules()
	if len(exposed) != len(linkTypeRules) {
		t.Fatalf("GetLinkTypeRules returned %d rules, want %d", len(exposed), len(linkTypeRules))
	}
	for i, rule := range exposed {
		if rule.Type != linkTypeRules[i].Type {
			t.Errorf("rule %d = %q, want %q", i, rule.Type, linkTypeRules[i].Type)
		}
	}
}

// TestValidateLinkTypeRules walks every rule in the table: each allowed
// from/to pair is accepted, and every other artifact type in the catalog is
// refused on the side the rule constrains — with an error that says which
// end was wrong, since that message is what the API returns to the user.
func TestValidateLinkTypeRules(t *testing.T) {
	catalog := catalogTypes(t)

	for _, rule := range linkTypeRules {
		rule := rule
		t.Run(rule.Type, func(t *testing.T) {
			allowedFrom := expand(t, rule.AllowedFromTypes)
			allowedTo := expand(t, rule.AllowedToTypes)

			t.Run("allowed pairs accepted", func(t *testing.T) {
				for _, from := range allowedFrom {
					for _, to := range allowedTo {
						if err := ValidateLinkType(rule.Type, from, to); err != nil {
							t.Errorf("ValidateLinkType(%q, %q, %q) = %v, want nil",
								rule.Type, from, to, err)
						}
					}
				}
			})

			t.Run("other source types rejected", func(t *testing.T) {
				if hasWildcard(rule.AllowedFromTypes) {
					t.Skip("rule accepts every source artifact type")
				}
				// TestRuleTableIsWellFormed already reports an empty end as
				// a table defect; bail out here rather than index into it.
				if len(allowedTo) == 0 {
					t.Fatalf("rule %q allows no target artifact types", rule.Type)
				}
				to := allowedTo[0]
				for _, from := range catalog {
					if inList(rule.AllowedFromTypes, from) {
						continue
					}
					err := ValidateLinkType(rule.Type, from, to)
					if err == nil {
						t.Errorf("ValidateLinkType(%q, %q, %q) = nil, want a rejection",
							rule.Type, from, to)
						continue
					}
					if !strings.Contains(err.Error(), "cannot originate from") {
						t.Errorf("ValidateLinkType(%q, %q, %q) error = %q, want it to name the source end",
							rule.Type, from, to, err)
					}
				}
			})

			t.Run("other target types rejected", func(t *testing.T) {
				if hasWildcard(rule.AllowedToTypes) {
					t.Skip("rule accepts every target artifact type")
				}
				if len(allowedFrom) == 0 {
					t.Fatalf("rule %q allows no source artifact types", rule.Type)
				}
				from := allowedFrom[0]
				for _, to := range catalog {
					if inList(rule.AllowedToTypes, to) {
						continue
					}
					err := ValidateLinkType(rule.Type, from, to)
					if err == nil {
						t.Errorf("ValidateLinkType(%q, %q, %q) = nil, want a rejection",
							rule.Type, from, to)
						continue
					}
					if !strings.Contains(err.Error(), "cannot target") {
						t.Errorf("ValidateLinkType(%q, %q, %q) error = %q, want it to name the target end",
							rule.Type, from, to, err)
					}
				}
			})

			t.Run("direction is enforced", func(t *testing.T) {
				// Reversing an allowed pair is only valid when the rule
				// happens to permit the swapped types too (decomposes-to
				// runs requirement -> requirement, so it is symmetric; the
				// wildcard rules are symmetric by construction).
				for _, from := range allowedFrom {
					for _, to := range allowedTo {
						reversible := inList(allowedFrom, to) && inList(allowedTo, from)
						err := ValidateLinkType(rule.Type, to, from)
						if reversible && err != nil {
							t.Errorf("ValidateLinkType(%q, %q, %q) = %v, want nil (rule is symmetric for these types)",
								rule.Type, to, from, err)
						}
						if !reversible && err == nil {
							t.Errorf("ValidateLinkType(%q, %q, %q) = nil, want the reversed pair rejected",
								rule.Type, to, from)
						}
					}
				}
			})
		})
	}
}

// TestValidateLinkTypeNamedPairs states the headline rules literally, so a
// silent edit to the table (say, letting a requirement verify a hazard) is
// caught even if the table-driven walk above were to be weakened.
func TestValidateLinkTypeNamedPairs(t *testing.T) {
	valid := []struct{ linkType, from, to string }{
		{"verifies", artifacts.TypeTestCase, artifacts.TypeRequirement},
		{"validates", artifacts.TypeTestCase, artifacts.TypeUserNeed},
		{"satisfies", artifacts.TypeDesignItem, artifacts.TypeRequirement},
		{"mitigates", artifacts.TypeDesignItem, artifacts.TypeHazard},
		{"decomposes-to", artifacts.TypeRequirement, artifacts.TypeRequirement},
		{"derives-from", artifacts.TypeRequirement, artifacts.TypeUserNeed},
	}
	for _, c := range valid {
		if err := ValidateLinkType(c.linkType, c.from, c.to); err != nil {
			t.Errorf("ValidateLinkType(%q, %q, %q) = %v, want nil", c.linkType, c.from, c.to, err)
		}
	}

	invalid := []struct {
		name                   string
		linkType, from, to     string
		wantMessageContainsAny []string
	}{
		{"a requirement cannot verify", "verifies", artifacts.TypeRequirement, artifacts.TypeRequirement,
			[]string{"cannot originate from"}},
		{"a test case cannot verify a hazard", "verifies", artifacts.TypeTestCase, artifacts.TypeHazard,
			[]string{"cannot target"}},
		{"a requirement does not mitigate a hazard", "mitigates", artifacts.TypeRequirement, artifacts.TypeHazard,
			[]string{"cannot originate from"}},
		{"a design item does not satisfy a user need", "satisfies", artifacts.TypeDesignItem, artifacts.TypeUserNeed,
			[]string{"cannot target"}},
		{"derives-from runs requirement to need, not the reverse", "derives-from", artifacts.TypeUserNeed, artifacts.TypeRequirement,
			[]string{"cannot originate from"}},
		{"a heading decomposes to nothing", "decomposes-to", artifacts.TypeHeading, artifacts.TypeRequirement,
			[]string{"cannot originate from"}},
	}
	for _, c := range invalid {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateLinkType(c.linkType, c.from, c.to)
			if err == nil {
				t.Fatalf("ValidateLinkType(%q, %q, %q) = nil, want a rejection", c.linkType, c.from, c.to)
			}
			for _, want := range c.wantMessageContainsAny {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

// TestValidateLinkTypeRejectsUnknownLinkTypes: only the types in the table
// exist. Matching is exact — no trimming, no case folding — so anything the
// UI or an agent invents is refused rather than silently stored unvalidated.
func TestValidateLinkTypeRejectsUnknownLinkTypes(t *testing.T) {
	unknown := []string{
		"",
		"implements",
		"traces-to",
		"refines",
		"VERIFIES",
		"Verifies",
		"verifies ",
		" verifies",
		"verifies\n",
		"decomposes_to",
		wildcard,
	}
	for _, linkType := range unknown {
		err := ValidateLinkType(linkType, artifacts.TypeTestCase, artifacts.TypeRequirement)
		if err == nil {
			t.Errorf("ValidateLinkType(%q, test-case, requirement) = nil, want a rejection", linkType)
			continue
		}
		if !strings.Contains(err.Error(), "invalid link type") {
			t.Errorf("ValidateLinkType(%q, ...) error = %q, want it to name an invalid link type", linkType, err)
		}
	}
}

// TestUnconstrainedLinkTypes pins the two rules the table documents as
// applying to all types: impacts and relates-to accept every ordered pair of
// catalog types. relates-to is symmetric in wording as well ("relates-to"
// both ways); impacts is directional in its labels but unconstrained in the
// types it will join.
func TestUnconstrainedLinkTypes(t *testing.T) {
	catalog := catalogTypes(t)

	for _, linkType := range []string{"impacts", "relates-to"} {
		t.Run(linkType, func(t *testing.T) {
			rule := ruleFor(t, linkType)
			if !hasWildcard(rule.AllowedFromTypes) || !hasWildcard(rule.AllowedToTypes) {
				t.Fatalf("rule %q is no longer wildcarded on both ends: from=%v to=%v",
					linkType, rule.AllowedFromTypes, rule.AllowedToTypes)
			}

			for _, from := range catalog {
				for _, to := range catalog {
					if err := ValidateLinkType(linkType, from, to); err != nil {
						t.Errorf("ValidateLinkType(%q, %q, %q) = %v, want nil", linkType, from, to, err)
					}
				}
			}
		})
	}

	relatesTo := ruleFor(t, "relates-to")
	if got, want := relatesTo.Label, relatesTo.InverseLabel; got != want {
		t.Errorf("relates-to label = %q, inverse = %q; the table documents it as symmetric", got, want)
	}
}

// ruleFor returns the table's rule for a link type, failing the test if the
// table has none. ValidateLinkType matches on the first rule with a given
// type, and TestRuleTableIsWellFormed rejects duplicates, so this lookup is
// the same one the validator performs.
func ruleFor(t *testing.T, linkType string) LinkTypeRule {
	t.Helper()
	for _, rule := range linkTypeRules {
		if rule.Type == linkType {
			return rule
		}
	}
	t.Fatalf("no rule for link type %q in the table", linkType)
	return LinkTypeRule{}
}

// TestValidateLinkTypeDoesNotPoliceArtifactTypes documents a deliberate
// limit of the validator: it checks artifact types against the rule's
// allowed lists only, never against the artifact type catalog. A wildcard
// rule therefore accepts a type name that does not exist. Callers pass the
// stored type of a real artifact, so this is unreachable through the API —
// the test exists so the behaviour is a decision rather than a surprise.
func TestValidateLinkTypeDoesNotPoliceArtifactTypes(t *testing.T) {
	if artifacts.ValidType("not-an-artifact-type") {
		t.Fatal("test premise broken: 'not-an-artifact-type' is in the catalog")
	}
	if err := ValidateLinkType("relates-to", "not-an-artifact-type", artifacts.TypeRequirement); err != nil {
		t.Errorf("wildcard rule rejected an unknown artifact type: %v", err)
	}
	// A constrained rule still refuses it, because it is not in the list.
	if err := ValidateLinkType("verifies", "not-an-artifact-type", artifacts.TypeRequirement); err == nil {
		t.Error("ValidateLinkType(verifies, not-an-artifact-type, requirement) = nil, want a rejection")
	}
}

// TestContainsWildcard covers the helper directly: a "*" anywhere in the
// allowed list opens it to everything, and matching is exact otherwise.
func TestContainsWildcard(t *testing.T) {
	cases := []struct {
		name  string
		slice []string
		value string
		want  bool
	}{
		{"exact match", []string{"requirement", "hazard"}, "hazard", true},
		{"no match", []string{"requirement", "hazard"}, "test-case", false},
		{"wildcard accepts anything", []string{wildcard}, "anything", true},
		{"wildcard beside concrete types accepts anything", []string{"requirement", wildcard}, "hazard", true},
		{"empty list matches nothing", nil, "requirement", false},
		{"a wildcard value is not itself a match", []string{"requirement"}, wildcard, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := contains(c.slice, c.value); got != c.want {
				t.Errorf("contains(%v, %q) = %v, want %v", c.slice, c.value, got, c.want)
			}
		})
	}
}
