package links

import (
	"fmt"
)

// LinkTypeRule defines constraints on link types
type LinkTypeRule struct {
	Type             string
	Label            string
	InverseLabel     string
	AllowedFromTypes []string
	AllowedToTypes   []string
	Description      string
}

var linkTypeRules = []LinkTypeRule{
	{
		Type:             "verifies",
		Label:            "verifies",
		InverseLabel:     "verified by",
		AllowedFromTypes: []string{"test-case"},
		AllowedToTypes:   []string{"requirement"},
		Description:      "A test case demonstrates that a requirement is met",
	},
	{
		Type:             "satisfies",
		Label:            "satisfies",
		InverseLabel:     "satisfied by",
		AllowedFromTypes: []string{"design-item"},
		AllowedToTypes:   []string{"requirement"},
		Description:      "A design element implements or fulfills a requirement",
	},
	{
		Type:             "mitigates",
		Label:            "mitigates",
		InverseLabel:     "mitigated by",
		AllowedFromTypes: []string{"design-item"},
		AllowedToTypes:   []string{"hazard"},
		Description:      "A design element reduces or eliminates a hazard",
	},
	{
		Type:             "decomposes-to",
		Label:            "decomposes to",
		InverseLabel:     "decomposed from",
		AllowedFromTypes: []string{"requirement"},
		AllowedToTypes:   []string{"requirement"},
		Description:      "A high-level requirement is broken down into more specific sub-requirements",
	},
	{
		Type:             "derives-from",
		Label:            "derives from",
		InverseLabel:     "gives rise to",
		AllowedFromTypes: []string{"requirement"},
		AllowedToTypes:   []string{"user-need"},
		Description:      "A requirement is derived from a user need",
	},
	{
		Type:             "validates",
		Label:            "validates",
		InverseLabel:     "validated by",
		AllowedFromTypes: []string{"test-case"},
		AllowedToTypes:   []string{"user-need"},
		Description:      "A test case confirms that a user need is met",
	},
	{
		Type:             "impacts",
		Label:            "impacts",
		InverseLabel:     "impacted by",
		AllowedFromTypes: []string{"*"}, // All types
		AllowedToTypes:   []string{"*"}, // All types
		Description:      "A change to one artifact affects another",
	},
	{
		Type:             "relates-to",
		Label:            "relates-to",
		InverseLabel:     "relates-to",
		AllowedFromTypes: []string{"*"}, // All types
		AllowedToTypes:   []string{"*"}, // All types
		Description:      "A loose, non-semantic association between artifacts",
	},
}

// contains checks if a slice contains a string
func contains(slice []string, str string) bool {
	for _, s := range slice {
		if s == str || s == "*" {
			return true
		}
	}
	return false
}

// ValidateLinkType checks if a link type is valid for the given artifact types
func ValidateLinkType(linkType, fromArtifactType, toArtifactType string) error {
	var rule *LinkTypeRule
	for i := range linkTypeRules {
		if linkTypeRules[i].Type == linkType {
			rule = &linkTypeRules[i]
			break
		}
	}

	if rule == nil {
		return fmt.Errorf("invalid link type: %s", linkType)
	}

	if !contains(rule.AllowedFromTypes, fromArtifactType) {
		return fmt.Errorf("link type '%s' cannot originate from artifact type '%s' (allowed: %v)", 
			linkType, fromArtifactType, rule.AllowedFromTypes)
	}

	if !contains(rule.AllowedToTypes, toArtifactType) {
		return fmt.Errorf("link type '%s' cannot target artifact type '%s' (allowed: %v)", 
			linkType, toArtifactType, rule.AllowedToTypes)
	}

	return nil
}

// GetLinkTypeRules returns all link type rules
func GetLinkTypeRules() []LinkTypeRule {
	return linkTypeRules
}
