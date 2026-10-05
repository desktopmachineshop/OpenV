package contract

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/providers"
	"github.com/openv/requirements-platform/internal/domain/release"
)

// contract is what both generated files hold: testdata/contract.json as
// JSON, frontend/src/generated/contract.ts as TypeScript constants. A
// vocabulary contracts/vocab.json also holds keeps vocab.json's JSON name
// and shape, so TestContractAgreesWithS13 compares the two directly.
type contract struct {
	About            string           `json:"about"`
	FeatureKeys      []string         `json:"feature_keys"`
	EventTypes       []string         `json:"event_types"`
	SSEEvents        []string         `json:"sse_events"`
	ErrorCodes       []string         `json:"error_codes"`
	LinkRules        []linkRule       `json:"link_rules"`
	ArtifactTypes    []artifactType   `json:"artifact_types"`
	ArtifactStatuses []artifactStatus `json:"artifact_statuses"`
	Plans            []string         `json:"plans"`
	GapLabels        []gapLabel       `json:"gap_labels"`
	Providers        []string         `json:"providers"`
}

type linkRule struct {
	Type             string   `json:"type"`
	Label            string   `json:"label"`
	InverseLabel     string   `json:"inverse_label"`
	AllowedFromTypes []string `json:"allowed_from_types"`
	AllowedToTypes   []string `json:"allowed_to_types"`
	Description      string   `json:"description"`
}

type artifactType struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type artifactStatus struct {
	Value string   `json:"value"`
	Next  []string `json:"next"`
}

type gapLabel struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

const contractAbout = "The cross-language contract (refactor plan X4a): the Go vocabularies the frontend reads, each " +
	"from its Go catalogue: feature keys in registry order, domain event types, the SSE event names (contracts/" +
	"sse-events.json, which S6 writes from the Go sources), API error codes, link rules with their Go text, " +
	"artifact types, artifact statuses with the transitions each allows, plans, V&V gap labels by JSON key (the " +
	"two Go tables in internal/domain/reports must be equal) and agent providers in display order. Written, with " +
	"the same values as " + tsFile + ", by TestContract (internal/contract); every vocabulary contracts/vocab.json " +
	"also holds must equal it there (TestContractAgreesWithS13). Regenerate only for a deliberate change: " +
	regenerate

// sseContractFile is S6's golden. TestSSEContract (internal/api) writes it
// from the four places a name enters a stream in the Go sources and fails
// when it is stale, so its event names are the Go names; reading them here
// keeps one scanner of those sources.
const sseContractFile = "contracts/sse-events.json"

// buildContract reads every vocabulary from its Go catalogue. Each must be
// non-empty: a reader that finds nothing has lost its catalogue, not
// emptied it.
func buildContract(t *testing.T, src *sources) contract {
	t.Helper()
	c := contract{About: contractAbout}

	for _, f := range release.Registry {
		c.FeatureKeys = append(c.FeatureKeys, f.Key)
	}

	c.EventTypes = src.constBlock(t, "internal/domain/events", "ArtifactCreated")

	c.SSEEvents = sseEventNames(t, src)

	c.ErrorCodes = src.constBlock(t, "internal/api", "ErrCodeEmailUnverified")

	for _, r := range links.GetLinkTypeRules() {
		c.LinkRules = append(c.LinkRules, linkRule{
			Type: r.Type, Label: r.Label, InverseLabel: r.InverseLabel,
			AllowedFromTypes: r.AllowedFromTypes, AllowedToTypes: r.AllowedToTypes, Description: r.Description,
		})
	}

	for _, d := range artifacts.TypeCatalog() {
		c.ArtifactTypes = append(c.ArtifactTypes, artifactType{Value: d.Value, Label: d.Label})
	}

	// The statuses are the constant block StatusDraft heads; the state
	// machine is unexported, so each status's next ones are asked of
	// CanTransition, in the block's order.
	statuses := src.constBlock(t, "internal/domain/artifacts", "StatusDraft")
	for _, s := range statuses {
		if !artifacts.ValidStatus(s) {
			t.Fatalf("status constant %q is not a valid status (artifacts.ValidStatus)", s)
		}
		next := []string{}
		for _, to := range statuses {
			if artifacts.CanTransition(s, to) {
				next = append(next, to)
			}
		}
		c.ArtifactStatuses = append(c.ArtifactStatuses, artifactStatus{Value: s, Next: next})
	}

	// Every plan constant, the legacy aliases included: PlanFree and PlanTeam
	// are no catalogue of their own, so the list is flat, as in vocab.json.
	c.Plans = src.constBlock(t, "internal/domain/orgs", "PlanSingle")
	for _, p := range c.Plans {
		if !orgs.ValidPlan(p) {
			t.Fatalf("plan constant %q is not a valid plan (orgs.ValidPlan)", p)
		}
	}

	// The PDF V&V report and the project reports title the gap lists in two
	// tables (S13's TestVocabularyGapLabelCopiesAgree holds them equal);
	// the contract has one, so it refuses to choose between two that differ.
	vvReport, project := gapTables(t, src)
	if !reflect.DeepEqual(vvReport, project) {
		t.Fatalf("the two Go gap-label tables in internal/domain/reports differ, and the contract holds one:\n"+
			"  buildVVReportPDF: %v\n  gapSections:      %v\nkeep them equal (go test ./internal/vocabparity "+
			"-run '^TestVocabularyGapLabelCopiesAgree$')", vvReport, project)
	}
	c.GapLabels = vvReport

	c.Providers = providers.KnownProviders()

	for name, n := range map[string]int{
		"feature_keys": len(c.FeatureKeys), "event_types": len(c.EventTypes), "sse_events": len(c.SSEEvents),
		"error_codes": len(c.ErrorCodes), "link_rules": len(c.LinkRules), "artifact_types": len(c.ArtifactTypes),
		"artifact_statuses": len(c.ArtifactStatuses), "plans": len(c.Plans), "gap_labels": len(c.GapLabels),
		"providers": len(c.Providers),
	} {
		if n == 0 {
			t.Errorf("%s: the reader found nothing; its Go catalogue has moved or changed shape", name)
		}
	}
	for name, list := range map[string][]string{
		"feature_keys":      c.FeatureKeys,
		"event_types":       c.EventTypes,
		"error_codes":       c.ErrorCodes,
		"link_rules":        keysOf(c.LinkRules, func(r linkRule) string { return r.Type }),
		"artifact_types":    keysOf(c.ArtifactTypes, func(a artifactType) string { return a.Value }),
		"artifact_statuses": keysOf(c.ArtifactStatuses, func(s artifactStatus) string { return s.Value }),
		"plans":             c.Plans,
		"gap_labels":        keysOf(c.GapLabels, func(g gapLabel) string { return g.Key }),
		"providers":         c.Providers,
	} {
		if dup := firstDuplicate(list); dup != "" {
			t.Errorf("%s: %q appears twice; a union type of the values could not tell the two apart", name, dup)
		}
	}
	return c
}

func keysOf[T any](items []T, key func(T) string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, key(it))
	}
	return out
}

// sseEventNames reads the event names of S6's SSE contract, sorted, since
// they come from four places in the sources and have no order of their own.
func sseEventNames(t *testing.T, src *sources) []string {
	t.Helper()
	data, err := os.ReadFile(src.path(sseContractFile))
	if err != nil {
		t.Fatalf("read %s: %v", sseContractFile, err)
	}
	var doc struct {
		Events []struct {
			Name string `json:"name"`
		} `json:"events"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s: %v", sseContractFile, err)
	}
	var names []string
	for _, e := range doc.Events {
		if e.Name == "" {
			t.Fatalf("%s: an event with no name", sseContractFile)
		}
		names = append(names, e.Name)
	}
	sort.Strings(names)
	if dup := firstDuplicate(names); dup != "" {
		t.Fatalf("%s: the event %q is listed twice", sseContractFile, dup)
	}
	return names
}

func firstDuplicate(list []string) string {
	seen := map[string]bool{}
	for _, s := range list {
		if seen[s] {
			return s
		}
		seen[s] = true
	}
	return ""
}
