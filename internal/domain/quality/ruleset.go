package quality

import (
	"fmt"
	"sort"
	"strings"
)

// Convention names the normative-keyword vocabulary a project writes its
// requirements in. It decides which words the linter treats as binding and
// which it flags as weak — the same sentence is good practice under one
// convention and a finding under the other.
const (
	// ConventionShall is ISO/IEC/IEEE 29148 house style: "the system shall…"
	// states every binding requirement, and should/may are weak words.
	ConventionShall = "shall"
	// ConventionRFC2119 grades obligation with MUST / SHOULD / MAY, so
	// "should" and "may" are normative keywords rather than weak words.
	ConventionRFC2119 = "rfc2119"
)

// SeverityOff disables a rule. It is a severity value rather than a separate
// list so a rule set is one flat rule -> severity map.
const SeverityOff = "off"

// Conventions lists the supported conventions in display order.
var Conventions = []string{ConventionShall, ConventionRFC2119}

// conventionLabels describe each convention for humans and agents.
var conventionLabels = map[string]string{
	ConventionShall:   "ISO/IEC/IEEE 29148 \"shall\": state every binding requirement as \"<actor> shall <action>\"; should/may/might/could read as weak wording.",
	ConventionRFC2119: "RFC 2119 keywords: MUST (binding), SHOULD (recommended) and MAY (optional) are all normative; \"shall\" is off-convention.",
}

// ruleLabels describe what each rule looks for, for the UI and for agents
// reading the rule set before they draft anything.
var ruleLabels = map[string]string{
	RuleWeakWord:        "weak or subjective wording",
	RulePassiveVoice:    "passive voice hiding the actor",
	RuleNotTestable:     "no imperative or measurable criterion",
	RuleVagueQuantifier: "vague quantifier instead of a number",
	RulePlaceholder:     "TBD/TODO placeholder text",
	RuleLongSentence:    fmt.Sprintf("sentence longer than %d words", longSentenceWords),
	RuleOffConvention:   "normative keyword from the other convention",
}

// Rules lists every rule identifier in report order.
var Rules = []string{
	RuleWeakWord, RuleVagueQuantifier, RulePlaceholder,
	RulePassiveVoice, RuleLongSentence, RuleNotTestable, RuleOffConvention,
}

// defaultSeverities is the out-of-the-box severity per rule. Placeholder text
// is an error because it means the requirement is simply unfinished; the
// heuristic rules warn; off-convention informs, since it is a house-style
// point and not a defect.
var defaultSeverities = map[string]string{
	RuleWeakWord:        SeverityWarning,
	RuleVagueQuantifier: SeverityWarning,
	RulePlaceholder:     SeverityError,
	RulePassiveVoice:    SeverityWarning,
	RuleLongSentence:    SeverityWarning,
	RuleNotTestable:     SeverityWarning,
	RuleOffConvention:   SeverityInfo,
}

// RuleSet is the resolved quality configuration a project is linted against:
// which normative vocabulary it writes in, and how loudly each rule speaks.
// It is advisory — findings never block a write.
type RuleSet struct {
	Convention string            `json:"convention"`
	Severities map[string]string `json:"severities"`
}

// DefaultRuleSet is what a workspace that has never set anything gets.
func DefaultRuleSet() RuleSet {
	severities := make(map[string]string, len(defaultSeverities))
	for rule, severity := range defaultSeverities {
		severities[rule] = severity
	}
	return RuleSet{Convention: ConventionShall, Severities: severities}
}

// SeverityFor returns the severity a rule speaks at, falling back to the
// default for rules the set does not mention.
func (rs RuleSet) SeverityFor(rule string) string {
	if severity, ok := rs.Severities[rule]; ok {
		return severity
	}
	if severity, ok := defaultSeverities[rule]; ok {
		return severity
	}
	return SeverityInfo
}

// Enabled reports whether a rule contributes findings.
func (rs RuleSet) Enabled(rule string) bool {
	return rs.SeverityFor(rule) != SeverityOff
}

// Describe renders the rule set as the short prose an agent reads before it
// drafts a requirement — the convention first, then anything switched off or
// re-graded. Deterministic: rules are listed in Rules order.
func (rs RuleSet) Describe() string {
	var b strings.Builder
	b.WriteString(conventionLabels[rs.Convention])
	var changed []string
	for _, rule := range Rules {
		if got, want := rs.SeverityFor(rule), defaultSeverities[rule]; got != want {
			changed = append(changed, fmt.Sprintf("%s: %s", rule, got))
		}
	}
	if len(changed) > 0 {
		b.WriteString(" Adjusted rules — ")
		b.WriteString(strings.Join(changed, ", "))
		b.WriteString(".")
	}
	return b.String()
}

// Labels describes every convention and rule, keyed by identifier, so a UI or
// an agent can explain a rule set without repeating these strings.
func Labels() map[string]string {
	out := make(map[string]string, len(conventionLabels)+len(ruleLabels))
	for k, v := range conventionLabels {
		out[k] = v
	}
	for k, v := range ruleLabels {
		out[k] = v
	}
	return out
}

// ValidConvention reports whether a convention name is supported.
func ValidConvention(name string) bool {
	for _, c := range Conventions {
		if c == name {
			return true
		}
	}
	return false
}

// ValidSeverity reports whether a severity (including "off") is supported.
func ValidSeverity(name string) bool {
	switch name {
	case SeverityError, SeverityWarning, SeverityInfo, SeverityOff:
		return true
	}
	return false
}

// ValidRule reports whether a rule identifier is one this linter runs.
func ValidRule(name string) bool {
	_, ok := defaultSeverities[name]
	return ok
}

// Validate checks a rule set a caller is trying to store, naming the first
// problem it finds. An empty convention is allowed — it means "inherit".
func Validate(rs RuleSet) error {
	if rs.Convention != "" && !ValidConvention(rs.Convention) {
		return fmt.Errorf("unknown convention %q: use %s", rs.Convention, strings.Join(Conventions, " or "))
	}
	rules := make([]string, 0, len(rs.Severities))
	for rule := range rs.Severities {
		rules = append(rules, rule)
	}
	sort.Strings(rules) // deterministic error for a caller sending several bad keys
	for _, rule := range rules {
		if !ValidRule(rule) {
			return fmt.Errorf("unknown rule %q", rule)
		}
		if !ValidSeverity(rs.Severities[rule]) {
			return fmt.Errorf("rule %q: unknown severity %q: use error, warning, info or off", rule, rs.Severities[rule])
		}
	}
	return nil
}

// SettingsKey is where a rule set lives inside a workspace's or project's
// settings object.
const SettingsKey = "quality"

// FromSettings reads the rule set stored under SettingsKey. Absent or
// malformed settings yield the zero RuleSet and false, which Resolve treats
// as "nothing set at this level" — a corrupt row degrades to inheritance
// rather than failing the request.
func FromSettings(settings map[string]interface{}) (RuleSet, bool) {
	raw, ok := settings[SettingsKey].(map[string]interface{})
	if !ok {
		return RuleSet{}, false
	}
	rs := RuleSet{}
	if convention, ok := raw["convention"].(string); ok && ValidConvention(convention) {
		rs.Convention = convention
	}
	if severities, ok := raw["severities"].(map[string]interface{}); ok {
		for rule, value := range severities {
			severity, isString := value.(string)
			if !isString || !ValidRule(rule) || !ValidSeverity(severity) {
				continue
			}
			if rs.Severities == nil {
				rs.Severities = map[string]string{}
			}
			rs.Severities[rule] = severity
		}
	}
	return rs, rs.Convention != "" || len(rs.Severities) > 0
}

// ToSettings renders a rule set for storage under SettingsKey. Only the
// fields the caller actually set are written, so a project override stays a
// diff against its workspace rather than a full copy.
func ToSettings(rs RuleSet) map[string]interface{} {
	out := map[string]interface{}{}
	if rs.Convention != "" {
		out["convention"] = rs.Convention
	}
	if len(rs.Severities) > 0 {
		severities := make(map[string]interface{}, len(rs.Severities))
		for rule, severity := range rs.Severities {
			severities[rule] = severity
		}
		out["severities"] = severities
	}
	return out
}

// Resolve layers the defaults, the workspace rule set and the project's
// override into the set a project is actually linted against. Each level
// overrides only the keys it names, so a project that re-grades one rule
// keeps its workspace's convention.
func Resolve(orgSettings, projectSettings map[string]interface{}) RuleSet {
	resolved := DefaultRuleSet()
	for _, settings := range []map[string]interface{}{orgSettings, projectSettings} {
		layer, ok := FromSettings(settings)
		if !ok {
			continue
		}
		if layer.Convention != "" {
			resolved.Convention = layer.Convention
		}
		for rule, severity := range layer.Severities {
			resolved.Severities[rule] = severity
		}
	}
	return resolved
}
