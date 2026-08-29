// Package quality is a pure, dependency-free, rule-based linter over the
// natural-language content of a requirement artifact (its title and body). It
// produces a deterministic per-artifact quality score (0-100) and a list of
// findings, following the same shape-first pattern as the vv coverage report:
// a domain function takes a project export and returns a plain report DTO that
// the API layer serializes untouched.
//
// The rules are heuristic and intentionally conservative — they surface
// language smells that make a requirement ambiguous or untestable (weak words,
// passive voice, missing "shall"/measurable phrasing, vague quantifiers,
// TBD/TODO placeholders, over-long sentences). Nothing here calls out to a
// model or the network; identical input always yields identical output.
package quality

import (
	"regexp"
	"sort"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/exports"
)

// Severity ranks a finding. Weights drive the score deduction.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

// Rule identifiers (stable strings; the UI may group or filter on these).
const (
	RuleWeakWord        = "weak-word"
	RulePassiveVoice    = "passive-voice"
	RuleNotTestable     = "not-testable"
	RuleVagueQuantifier = "vague-quantifier"
	RulePlaceholder     = "placeholder"
	RuleLongSentence    = "long-sentence"
)

// Score bands. Band maps a 0-100 score to one of these.
const (
	BandGood = "good" // >= 80
	BandFair = "fair" // >= 50
	BandPoor = "poor" // < 50
)

// severity weight deducted from the starting score of 100.
var severityWeight = map[string]int{
	SeverityError:   15,
	SeverityWarning: 8,
	SeverityInfo:    4,
}

// longSentenceWords is the word count above which a sentence is flagged.
const longSentenceWords = 30

// Finding is one lint hit against an artifact's content. Start/End are byte
// offsets into the linted text (title, two newlines, then body); Match is the
// exact substring that triggered the rule. Offsets are best-effort context for
// the UI — rules that judge the whole artifact (e.g. not-testable) report a
// zero span.
type Finding struct {
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Match    string `json:"match,omitempty"`
}

// ArtifactScore is the lint result for a single requirement artifact.
type ArtifactScore struct {
	ArtifactID string    `json:"artifact_id"`
	Title      string    `json:"title"`
	Type       string    `json:"type"`
	Score      int       `json:"score"` // 0-100, higher is better
	Band       string    `json:"band"`  // good | fair | poor
	Findings   []Finding `json:"findings"`
}

// Report aggregates per-artifact scores for a project.
type Report struct {
	ProjectID string          `json:"project_id"`
	Entries   []ArtifactScore `json:"entries"`
	Summary   map[string]int  `json:"summary"` // band -> count
}

// requirementTypes are the artifact types this linter judges. Quality linting
// targets natural-language "the system shall…" / "the user needs…" statements;
// structural types (headings, descriptions) and downstream artifacts (test
// cases, design items) are out of scope. Sourced against the artifacts type
// catalog so it stays in step with the vocabulary.
var requirementTypes = map[string]bool{
	artifacts.TypeRequirement: true,
	artifacts.TypeUserNeed:    true,
}

// IsRequirementType reports whether an artifact type is linted by this package.
func IsRequirementType(artifactType string) bool {
	return requirementTypes[artifactType]
}

// Band maps a score to its quality band.
func Band(score int) string {
	switch {
	case score >= 80:
		return BandGood
	case score >= 50:
		return BandFair
	default:
		return BandPoor
	}
}

// --- curated word lists ---

// weakWords are vague, subjective, or non-verifiable qualifiers that make a
// requirement hard to test objectively.
var weakWords = []string{
	"should", "may", "might", "could", "would",
	"appropriate", "adequate", "sufficient", "acceptable", "reasonable",
	"user-friendly", "easy", "simple", "intuitive", "seamless",
	"fast", "quick", "slow", "responsive", "efficient",
	"robust", "flexible", "scalable", "reliable", "secure",
	"optimal", "best", "better", "improved", "enhanced",
	"modern", "state-of-the-art", "cutting-edge", "world-class",
	"minimal", "maximal", "roughly", "approximately", "about",
	"etc", "and/or", "as needed", "if necessary", "where possible",
}

// vagueQuantifiers are imprecise amounts that should be replaced with a
// measurable value.
var vagueQuantifiers = []string{
	"some", "many", "few", "several", "various", "numerous",
	"multiple", "most", "a lot", "lots of", "a number of",
	"as much as possible", "as many as possible", "a variety of",
}

// placeholders mark unfinished content.
// placeholders are matched with word boundaries, so they must contain word
// characters (a bare "???" cannot sit next to a \b and is deliberately omitted).
var placeholders = []string{
	"tbd", "todo", "fixme", "xxx",
	"to be determined", "to be defined", "to be decided",
}

// --- compiled patterns (built once, deterministic) ---

var (
	weakWordRe        = wordListRegexp(weakWords)
	vagueQuantifierRe = wordListRegexp(vagueQuantifiers)
	placeholderRe     = wordListRegexp(placeholders)

	// passiveVoiceRe: a "to be" verb followed by a past participle. Heuristic —
	// catches "is validated", "are stored", "was rejected", "be processed",
	// "been written". Not exhaustive and not free of false positives, which is
	// why it is only a warning.
	passiveVoiceRe = regexp.MustCompile(`(?i)\b(?:is|are|was|were|be|been|being)\s+(?:\w+ed|\w+en)\b`)

	// testablePhraseRe: signals that a requirement is stated in verifiable
	// terms — an imperative "shall"/"must", or a measurable comparison
	// ("<= 500 ms", "at least 3"). Its ABSENCE drives the not-testable rule.
	testablePhraseRe = regexp.MustCompile(`(?i)\b(?:shall|must)\b`)
	measurableRe     = regexp.MustCompile(`\d`)

	// sentenceSplitRe splits on sentence-ending punctuation followed by
	// whitespace, keeping offsets recoverable via index scanning.
	sentenceSplitRe = regexp.MustCompile(`[.!?]+\s+`)
)

// wordListRegexp compiles a case-insensitive alternation with word boundaries
// around each phrase. Longer phrases are placed first so the alternation
// prefers the most specific match. Phrases are escaped, and interior spaces are
// allowed to match any run of whitespace.
func wordListRegexp(words []string) *regexp.Regexp {
	escaped := make([]string, 0, len(words))
	// Sort a copy longest-first for greedy specificity, stable for determinism.
	sorted := append([]string(nil), words...)
	sort.SliceStable(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, w := range sorted {
		q := regexp.QuoteMeta(strings.TrimSpace(w))
		q = strings.ReplaceAll(q, `\ `, `\s+`)
		escaped = append(escaped, q)
	}
	return regexp.MustCompile(`(?i)\b(?:` + strings.Join(escaped, "|") + `)\b`)
}

// combinedText joins title and body into the single string the rules scan.
// The two-newline separator keeps the title a distinct "sentence" so a title
// without terminal punctuation does not merge into the first body sentence.
func combinedText(title, body string) string {
	return strings.TrimSpace(title) + "\n\n" + body
}

// LintArtifact runs every rule over one artifact's title and body and returns
// its score and findings. It is pure: no I/O, no clock, no randomness.
func LintArtifact(a *artifacts.Artifact) ArtifactScore {
	text := combinedText(a.Title, a.Body)
	findings := []Finding{}

	findings = append(findings, matchFindings(text, weakWordRe, RuleWeakWord, SeverityWarning,
		"weak or subjective wording — state a verifiable criterion instead")...)
	findings = append(findings, matchFindings(text, vagueQuantifierRe, RuleVagueQuantifier, SeverityWarning,
		"vague quantifier — replace with a specific, measurable amount")...)
	findings = append(findings, matchFindings(text, placeholderRe, RulePlaceholder, SeverityError,
		"placeholder text — the requirement is incomplete")...)
	findings = append(findings, matchFindings(text, passiveVoiceRe, RulePassiveVoice, SeverityWarning,
		"possible passive voice — name the actor performing the action")...)
	findings = append(findings, longSentenceFindings(text)...)
	findings = append(findings, notTestableFindings(text)...)

	// Stable order: by start offset, then rule name. Rules already emit in
	// offset order, but interleaving across rules needs a final sort.
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Start != findings[j].Start {
			return findings[i].Start < findings[j].Start
		}
		return findings[i].Rule < findings[j].Rule
	})

	score := scoreFor(findings)
	return ArtifactScore{
		ArtifactID: a.ID,
		Title:      a.Title,
		Type:       a.Type,
		Score:      score,
		Band:       Band(score),
		Findings:   findings,
	}
}

// matchFindings emits one finding per non-overlapping regex match.
func matchFindings(text string, re *regexp.Regexp, rule, severity, message string) []Finding {
	out := []Finding{}
	for _, loc := range re.FindAllStringIndex(text, -1) {
		out = append(out, Finding{
			Rule:     rule,
			Severity: severity,
			Message:  message,
			Start:    loc[0],
			End:      loc[1],
			Match:    text[loc[0]:loc[1]],
		})
	}
	return out
}

// longSentenceFindings flags sentences longer than longSentenceWords words.
func longSentenceFindings(text string) []Finding {
	out := []Finding{}
	offset := 0
	remaining := text
	for {
		loc := sentenceSplitRe.FindStringIndex(remaining)
		var sentence string
		var next int
		if loc == nil {
			sentence = remaining
			next = len(remaining)
		} else {
			sentence = remaining[:loc[0]]
			next = loc[1]
		}
		if n := len(strings.Fields(sentence)); n > longSentenceWords {
			trimmed := strings.TrimSpace(sentence)
			start := offset + strings.Index(sentence, trimmed)
			out = append(out, Finding{
				Rule:     RuleLongSentence,
				Severity: SeverityWarning,
				Message:  "over-long sentence — split into shorter, single-condition statements",
				Start:    start,
				End:      start + len(trimmed),
				Match:    firstWords(trimmed, 8),
			})
		}
		if loc == nil {
			break
		}
		offset += next
		remaining = remaining[next:]
	}
	return out
}

// notTestableFindings emits a single artifact-level finding when the content
// carries neither an imperative ("shall"/"must") nor any digit (a proxy for a
// measurable acceptance criterion).
func notTestableFindings(text string) []Finding {
	if testablePhraseRe.MatchString(text) || measurableRe.MatchString(text) {
		return nil
	}
	return []Finding{{
		Rule:     RuleNotTestable,
		Severity: SeverityWarning,
		Message:  "no 'shall'/'must' or measurable criterion — requirement may not be testable",
	}}
}

// scoreFor computes the 0-100 score from the findings' severity weights.
func scoreFor(findings []Finding) int {
	score := 100
	for _, f := range findings {
		score -= severityWeight[f.Severity]
	}
	if score < 0 {
		return 0
	}
	return score
}

// firstWords returns at most n leading words of s, appending an ellipsis when
// truncated. Used to keep long-sentence match snippets short.
func firstWords(s string, n int) string {
	fields := strings.Fields(s)
	if len(fields) <= n {
		return strings.Join(fields, " ")
	}
	return strings.Join(fields[:n], " ") + "…"
}

// LintProject lints every requirement-type artifact in the export and returns a
// report. Non-requirement types are skipped. Entries preserve the export's
// artifact order for a deterministic result.
func LintProject(export *exports.ProjectExport) *Report {
	report := &Report{
		ProjectID: export.ProjectID,
		Entries:   []ArtifactScore{},
		Summary:   map[string]int{BandGood: 0, BandFair: 0, BandPoor: 0},
	}
	for _, a := range export.Artifacts {
		if a == nil || !IsRequirementType(a.Type) {
			continue
		}
		entry := LintArtifact(a)
		report.Entries = append(report.Entries, entry)
		report.Summary[entry.Band]++
	}
	return report
}
