package exports

import (
	"sort"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
)

// Content chooses what a rendered document — the PDF specification or the
// Word document — carries beyond the artifacts the Selection kept. The data
// formats (JSON, CSV, Excel, ReqIF) ignore it: they carry everything.
//
// Every switch defaults to the document a reader expects of a specification:
// fields, traceability, figures and a table of contents on; test evidence
// and V&V status off, because they describe verification rather than the
// product, and a template turns them on for the reviews that need them.
type Content struct {
	// Template names the preset the reader started from ("", or a key from
	// Templates()). It is recorded on the cover; the switches below are what
	// the document actually does.
	Template string `json:"template,omitempty"`
	// Traceability includes each artifact's incoming and outgoing links.
	Traceability bool `json:"traceability"`
	// Figures embeds each artifact's figures, captioned with their reference.
	Figures bool `json:"figures"`
	// TOC adds a table of contents after the cover.
	TOC bool `json:"toc"`
	// AllFields shows every attribute an artifact carries. When false, only
	// the keys in Fields are shown; an empty Fields then shows none.
	AllFields bool     `json:"all_fields"`
	Fields    []string `json:"fields,omitempty"`
	// TestResults adds each test case's latest result and a test-run
	// appendix.
	TestResults bool `json:"test_results"`
	// VVStatus adds each requirement's verification rollup and a coverage
	// summary with the gap analysis.
	VVStatus bool `json:"vv_status"`
}

// DefaultContent is the specification document: everything about the
// product, nothing about its verification.
func DefaultContent() Content {
	return Content{Traceability: true, Figures: true, TOC: true, AllFields: true}
}

// ShowsField reports whether an attribute key belongs in the document.
func (c Content) ShowsField(key string) bool {
	if c.AllFields {
		return true
	}
	for _, f := range c.Fields {
		if f == key {
			return true
		}
	}
	return false
}

// NeedsEvidence reports whether rendering needs test results and runs
// loaded beside the snapshot.
func (c Content) NeedsEvidence() bool {
	return c.TestResults || c.VVStatus
}

// Template is a preset for a standard review: which artifact types it keeps
// and what the document carries. A reader picks one and may then adjust
// every switch; the preset is a starting point, not a lock.
type Template struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Types are the artifact types the preset keeps; nil keeps every type.
	Types   []string `json:"types,omitempty"`
	Content Content  `json:"content"`
}

// Standard field keys, in the order a document shows them. Custom attribute
// definitions follow these, in their own order.
const (
	FieldPriority           = "priority"
	FieldStatus             = "status"
	FieldVerificationMethod = "verification_method"
	FieldVerificationStatus = "verification_status"
	FieldExecutionMethod    = "execution_method"
	FieldSeverity           = "severity"
)

// StandardFields are the attribute keys the platform itself writes, shown
// before any custom definition.
var StandardFields = []string{
	FieldPriority, FieldStatus, FieldVerificationMethod, FieldVerificationStatus, FieldExecutionMethod, FieldSeverity,
}

// hiddenFields are attribute keys that are bookkeeping, never document
// content.
var hiddenFields = map[string]bool{"links_snapshot": true}

// Templates lists the presets in the order a chooser offers them.
func Templates() []Template {
	return []Template{
		{
			Key:         "standard",
			Name:        "Specification",
			Description: "The whole project as a specification: every artifact with its fields, figures and traceability.",
			Content:     DefaultContent(),
		},
		{
			Key:         "requirements-review",
			Name:        "Requirements review",
			Description: "Needs and requirements with their priority, status and verification method, traced to what they derive from, for a review board.",
			Types:       []string{artifacts.TypeUserNeed, artifacts.TypeRequirement},
			Content: Content{
				Traceability: true, Figures: true, TOC: true,
				Fields: []string{FieldPriority, FieldStatus, FieldVerificationMethod},
			},
		},
		{
			Key:         "test-planning",
			Name:        "Test planning",
			Description: "Requirements and the test cases that verify them, with verification and execution methods, no results yet.",
			Types:       []string{artifacts.TypeRequirement, artifacts.TypeTestCase},
			Content: Content{
				Traceability: true, TOC: true,
				Fields: []string{FieldPriority, FieldStatus, FieldVerificationMethod, FieldExecutionMethod},
			},
		},
		{
			Key:         "vv",
			Name:        "Verification & Validation",
			Description: "Requirements, tests and hazards with every field, each requirement's verification rollup, the latest test results, the coverage summary and gaps.",
			Types:       []string{artifacts.TypeUserNeed, artifacts.TypeRequirement, artifacts.TypeTestCase, artifacts.TypeHazard},
			Content: Content{
				Traceability: true, TOC: true, AllFields: true, TestResults: true, VVStatus: true,
			},
		},
	}
}

// TemplateByKey finds a preset; ok is false for an unknown key.
func TemplateByKey(key string) (Template, bool) {
	for _, t := range Templates() {
		if t.Key == key {
			return t, true
		}
	}
	return Template{}, false
}

// FieldOption is one attribute key a document can show, with a label and
// how many artifacts carry it, so a form can offer only what the project
// holds.
type FieldOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
	// Custom is true for a key that comes from an attribute definition or
	// was discovered on artifacts rather than written by the platform.
	Custom bool `json:"custom"`
}

// Fields lists the attribute keys present in a snapshot: the standard keys
// first in their fixed order, then attribute definitions in theirs, then any
// other key found on an artifact, alphabetically.
func Fields(data *ProjectExport) []FieldOption {
	if data == nil {
		return nil
	}
	counts := map[string]int{}
	for _, a := range data.Artifacts {
		if a == nil {
			continue
		}
		for k, v := range a.Attributes {
			if hiddenFields[k] || v == nil {
				continue
			}
			if s, ok := v.(string); ok && s == "" {
				continue
			}
			counts[k]++
		}
	}
	labels := map[string]string{}
	var defined []string
	for _, d := range data.AttributeDefs {
		if d == nil || hiddenFields[d.Key] {
			continue
		}
		if _, seen := labels[d.Key]; !seen {
			defined = append(defined, d.Key)
		}
		if d.Label != "" {
			labels[d.Key] = d.Label
		} else {
			labels[d.Key] = FieldLabel(d.Key)
		}
	}

	var out []FieldOption
	added := map[string]bool{}
	for _, k := range StandardFields {
		if counts[k] > 0 {
			out = append(out, FieldOption{Key: k, Label: FieldLabel(k), Count: counts[k]})
			added[k] = true
		}
	}
	for _, k := range defined {
		if added[k] {
			continue
		}
		out = append(out, FieldOption{Key: k, Label: labels[k], Count: counts[k], Custom: true})
		added[k] = true
	}
	var rest []string
	for k := range counts {
		if !added[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		out = append(out, FieldOption{Key: k, Label: FieldLabel(k), Count: counts[k], Custom: true})
	}
	return out
}

// FieldLabel turns an attribute key into the words a document prints:
// "verification_method" → "Verification method".
func FieldLabel(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	words := strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(key))
	if len(words) == 0 {
		return key
	}
	words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	return strings.Join(words, " ")
}
