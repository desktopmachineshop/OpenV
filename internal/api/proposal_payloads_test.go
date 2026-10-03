package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/agents"
	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/links"
	"github.com/openv/requirements-platform/internal/domain/proposals"
	"github.com/openv/requirements-platform/internal/domain/vv"
)

// TestProposalPayloadsGolden is refactor plan step S9's proposal payload
// golden (invariant I16: stored data). A write a proposal-mode agent run
// makes is not applied: maybePropose marshals the request DTO the handler
// decoded into a map, the proposal service lifts a create_artifact's
// temporary ref out of it, and the map is stored in proposals.payload; the
// applier decodes it back into the DTO when a person approves it, maybe
// releases later. So a stored payload must keep decoding into the DTO, and
// these files under testdata/proposal_payloads/ pin, per operation, the
// DTO's JSON (every field set, and the zero value), the payload as
// maybePropose and the proposal service store it, and what the applier
// decodes from it. A DTO field added, renamed or retagged changes them; a
// field the "every field set" case leaves zero fails the test, so a new
// field is in the golden from the change that adds it; so does a file
// under testdata/proposal_payloads/ that names no operation here.
//
// record_test_result has an applier but no route proposes it today; its
// file pins the DTO and what the applier decodes from a payload carrying
// run_id, the one key it reads beside the DTO.
//
// Regenerate, for a deliberate change only, with
//
//	UPDATE_GOLDEN=1 go test ./internal/api -count=1 -run '^(TestFormatsGolden|TestProposalPayloadsGolden)$'
//
// Only the value 1 regenerates; any other value compares.
func TestProposalPayloadsGolden(t *testing.T) {
	text, body, sort, parent := "Changed", "The system shall change.", 7, s9ID(101)
	typ := "requirement"
	cases := []struct {
		op, dto, route string
		target         bool
		variants       []s9PayloadVariant
	}{
		{proposals.OpCreateArtifact, "CreateArtifactRequest", "POST /api/v1/artifacts", false, []s9PayloadVariant{
			{"every field set", &artifacts.CreateArtifactRequest{ProjectID: s9P, ParentID: &parent, Type: "requirement",
				Title: "Proposed", Body: "The system shall be proposed.", SortOrder: &sort,
				Attributes: map[string]interface{}{"priority": "must", "weight": 3, "tags": []interface{}{"a", "b"}},
				Ref:        "tmp-1", CopiedFrom: s9ID(104)}, true},
			{"zero value", &artifacts.CreateArtifactRequest{}, false},
		}},
		{proposals.OpUpdateArtifact, "UpdateArtifactRequest", "PUT /api/v1/artifacts/{id}", true, []s9PayloadVariant{
			{"every field set", &artifacts.UpdateArtifactRequest{ParentID: artifacts.PresentString(&parent), Type: &typ,
				Title: &text, Body: &body, SortOrder: &sort, Attributes: map[string]interface{}{"priority": "should"},
				LinksSnapshot:      []interface{}{map[string]interface{}{"id": s9ID(202), "type": "verifies"}},
				PendingLinkAdds:    []interface{}{map[string]interface{}{"to_id": s9ID(105), "type": "decomposes-to"}},
				PendingLinkRemoves: []string{s9ID(201)}}, true},
			{"parent_id null: to the root", &artifacts.UpdateArtifactRequest{ParentID: artifacts.PresentString(nil),
				Title: &text}, false},
			{"zero value: every field omitted", &artifacts.UpdateArtifactRequest{}, false},
		}},
		{proposals.OpCreateLink, "CreateLinkRequest", "POST /api/v1/links", false, []s9PayloadVariant{
			{"every field set", &links.CreateLinkRequest{FromID: s9ID(107), ToID: "tmp-1", Type: "verifies",
				Attributes: map[string]interface{}{"note": "pending"}}, true},
			{"zero value", &links.CreateLinkRequest{}, false},
		}},
		{proposals.OpDeleteArtifact, "", "DELETE /api/v1/artifacts/{id}", true, []s9PayloadVariant{{"no body", nil, false}}},
		{proposals.OpDeleteLink, "", "DELETE /api/v1/links/{id}", true, []s9PayloadVariant{{"no body", nil, false}}},
	}
	// A golden whose operation is gone would pin nothing, and regenerating
	// deletes no file, so this holds under UPDATE_GOLDEN=1 too.
	ops := map[string]bool{proposals.OpRecordTestResult: true}
	for _, c := range cases {
		ops[c.op] = true
	}
	checkS9Orphans(t, "testdata/proposal_payloads", ops, "operation of TestProposalPayloadsGolden")

	for _, c := range cases {
		t.Run(c.op, func(t *testing.T) {
			var b strings.Builder
			fmt.Fprintf(&b, s9PayloadHeader, c.op, c.route)
			if c.dto != "" {
				fmt.Fprintf(&b, "dto: %s\n", c.dto)
			} else {
				b.WriteString("dto: none (the handler proposes no body)\n")
			}
			for _, v := range c.variants {
				if v.full {
					s9RequireEveryField(t, v.dto)
				}
				var target *string
				if c.target {
					target = s9Ptr(s9ID(104))
				}
				stored := s9Propose(t, c.op, target, v.dto)
				fmt.Fprintf(&b, "case %q:\n", v.name)
				if v.dto != nil {
					fmt.Fprintf(&b, "  request: %s\n", s9JSON(t, v.dto))
				}
				fmt.Fprintf(&b, "  stored payload: %s\n", s9JSON(t, stored.Payload))
				fmt.Fprintf(&b, "  stored ref: %q\n", stored.Ref)
				fmt.Fprintf(&b, "  stored target_id: %s\n", s9JSON(t, stored.TargetID))
				if v.dto != nil {
					applied := reflect.New(reflect.TypeOf(v.dto).Elem()).Interface()
					if err := decodeProposalPayload(stored.Payload, applied); err != nil {
						t.Fatalf("the applier cannot decode the stored payload: %v", err)
					}
					fmt.Fprintf(&b, "  applied: %s\n", s9JSON(t, applied))
				}
			}
			checkS9Golden(t, "testdata/proposal_payloads", c.op+".txt", b.String())
		})
	}

	t.Run(proposals.OpRecordTestResult, func(t *testing.T) {
		var b strings.Builder
		fmt.Fprintf(&b, s9PayloadHeader, proposals.OpRecordTestResult, "none: no route proposes it today")
		b.WriteString("dto: UpsertResultRequest\n")
		full := &vv.UpsertResultRequest{TestCaseID: s9ID(107), Status: "pass", Notes: "1.4 s at peak",
			Evidence: []string{s9ID(301)}}
		s9RequireEveryField(t, full)
		for _, v := range []s9PayloadVariant{{"every field set", full, true}, {"zero value", &vv.UpsertResultRequest{}, false}} {
			payload := map[string]interface{}{}
			if err := json.Unmarshal([]byte(s9JSON(t, v.dto)), &payload); err != nil {
				t.Fatal(err)
			}
			payload["run_id"] = s9ID(401)
			var applied vv.UpsertResultRequest
			if err := decodeProposalPayload(payload, &applied); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&b, "case %q:\n  request: %s\n  payload with run_id: %s\n  applied: %s\n",
				v.name, s9JSON(t, v.dto), s9JSON(t, payload), s9JSON(t, &applied))
		}
		checkS9Golden(t, "testdata/proposal_payloads", proposals.OpRecordTestResult+".txt", b.String())
	})
}

const s9PayloadHeader = `# What a proposal-mode agent run's %s stores in proposals.payload
# (refactor plan S9, invariant I16), reached by %s.
# request: the DTO the handler decoded, as encoding/json writes it.
# stored payload: the map maybePropose builds from it, after the proposal
# service lifts a create_artifact's ref into its own column (keys sorted, as
# encoding/json writes a map; Postgres keeps it as JSONB).
# applied: the DTO the applier decodes from the stored payload on approval.
# A refactor never changes this file; for a deliberate change regenerate it
# with
#   UPDATE_GOLDEN=1 go test ./internal/api -count=1 -run '^(TestFormatsGolden|TestProposalPayloadsGolden)$'
`

type s9PayloadVariant struct {
	name string
	dto  interface{}
	full bool
}

// s9RequireEveryField fails when a field of the DTO is zero in the case
// meant to set every field, so a field a change adds is in the golden.
func s9RequireEveryField(t *testing.T, dto interface{}) {
	t.Helper()
	v := reflect.ValueOf(dto).Elem()
	for i := 0; i < v.NumField(); i++ {
		if v.Type().Field(i).IsExported() && v.Field(i).IsZero() {
			t.Fatalf("%s.%s is not set in the \"every field set\" case of TestProposalPayloadsGolden: set it, "+
				"then regenerate the golden, so the new field's stored form is pinned", v.Type().Name(), v.Type().Field(i).Name)
		}
	}
}

func s9JSON(t *testing.T, v interface{}) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// s9ProposalRepo keeps the proposals the service saves.
type s9ProposalRepo struct {
	proposals.Repository
	saved []*proposals.Proposal
}

func (r *s9ProposalRepo) Save(p *proposals.Proposal) error {
	r.saved = append(r.saved, p)
	return nil
}

func (r *s9ProposalRepo) CountByRun(string) (int, error) { return len(r.saved), nil }

func (r *s9ProposalRepo) List(_, _, _, runID string) ([]*proposals.Proposal, error) {
	return r.saved, nil
}

type s9ProposalAgents struct{ agents.Service }

func (s9ProposalAgents) Get(id string) (*agents.Agent, error) {
	return &agents.Agent{ID: id, WriteMode: agents.WriteModeProposal}, nil
}

// s9Propose runs maybePropose for a proposal-mode run, over the real
// proposal service, and returns the proposal it stored.
func s9Propose(t *testing.T, op string, target *string, dto interface{}) *proposals.Proposal {
	t.Helper()
	repo := &s9ProposalRepo{}
	h := vvHandler(t, nil) // K6: reuse vvHandler (see newS9Fixture)
	h.agentService = s9ProposalAgents{}
	h.proposalService = proposals.NewDefaultService(repo, proposals.Appliers{})
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r = r.WithContext(context.WithValue(r.Context(), ctxRun,
		&agentruns.Run{ID: s9ID(951), AgentID: s9ID(952), ProjectID: s9Ptr(s9P)}))
	w := httptest.NewRecorder()
	var body interface{}
	if dto != nil {
		body = reflect.ValueOf(dto).Elem().Interface()
	}
	if !h.maybePropose(w, r, s9P, op, target, body) || w.Code != http.StatusAccepted {
		t.Fatalf("maybePropose did not divert %s to a proposal: %d %s", op, w.Code, w.Body.String())
	}
	if len(repo.saved) != 1 {
		t.Fatalf("%d proposals stored, want 1", len(repo.saved))
	}
	return repo.saved[0]
}
