package proposals

import (
	"strings"
	"testing"
)

// Save and CountByRun round out the fake for the Propose path.
func (f *fakeProposalRepo) Save(p *Proposal) error {
	cp := *p
	f.items[p.ID] = &cp
	return nil
}

func (f *fakeProposalRepo) CountByRun(runID string) (int, error) {
	n := 0
	for _, p := range f.items {
		if p.RunID == runID {
			n++
		}
	}
	return n, nil
}

// List lets the ref-resolution path (resolveLinkPayload) find a run's sibling
// proposals. Only the filters that path uses are honoured.
func (f *fakeProposalRepo) List(projectID, status, runID string) ([]*Proposal, error) {
	var out []*Proposal
	for _, p := range f.items {
		if runID != "" && p.RunID != runID {
			continue
		}
		if projectID != "" && p.ProjectID != projectID {
			continue
		}
		if status != "" && p.Status != status {
			continue
		}
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

func artifactProposal(id, run, ref string) *Proposal {
	return &Proposal{ID: id, RunID: run, Op: OpCreateArtifact, Ref: ref, Status: StatusPending, Payload: map[string]interface{}{"title": "t"}}
}

func linkProposal(id, run, from, to string) *Proposal {
	return &Proposal{ID: id, RunID: run, Op: OpCreateLink, Status: StatusPending, Payload: map[string]interface{}{"from_id": from, "to_id": to, "type": "verifies"}}
}

// TestRefResolvesWhenArtifactApprovedFirst is the happy path: an artifact
// proposal mints ref "t1", the link proposal names it as from_id, and approving
// the artifact then the link rewrites "t1" to the real artifact id before the
// link is created.
func TestRefResolvesWhenArtifactApprovedFirst(t *testing.T) {
	var linkFrom, linkTo string
	appliers := Appliers{
		CreateArtifact: func(map[string]interface{}) (string, error) { return "real-art-id", nil },
		CreateLink: func(payload map[string]interface{}) (string, error) {
			linkFrom, _ = payload["from_id"].(string)
			linkTo, _ = payload["to_id"].(string)
			return "link-id", nil
		},
	}
	svc, _ := newProposalService(appliers,
		artifactProposal("p_art", "r1", "t1"),
		linkProposal("p_link", "r1", "t1", "req-real"),
	)

	if _, err := svc.Approve("p_art", nil, ""); err != nil {
		t.Fatalf("approve artifact: %v", err)
	}
	linkP, err := svc.Approve("p_link", nil, "")
	if err != nil {
		t.Fatalf("approve link: %v", err)
	}
	if linkP.Status != StatusApplied {
		t.Fatalf("link status = %s, want applied (note: %s)", linkP.Status, linkP.ReviewNote)
	}
	if linkFrom != "real-art-id" {
		t.Fatalf("from_id resolved to %q, want real-art-id", linkFrom)
	}
	if linkTo != "req-real" {
		t.Fatalf("to_id = %q, want the literal req-real (not a ref)", linkTo)
	}
}

// TestRefFailsWhenLinkApprovedBeforeArtifact locks in clean failure: approving
// the link while the artifact proposal is still pending must apply_fail with a
// message that names the token and its blocking status — never create a
// dangling link.
func TestRefFailsWhenLinkApprovedBeforeArtifact(t *testing.T) {
	created := false
	appliers := Appliers{
		CreateArtifact: func(map[string]interface{}) (string, error) { return "real-art-id", nil },
		CreateLink:     func(map[string]interface{}) (string, error) { created = true; return "link-id", nil },
	}
	svc, _ := newProposalService(appliers,
		artifactProposal("p_art", "r1", "t1"),
		linkProposal("p_link", "r1", "t1", "req-real"),
	)

	linkP, err := svc.Approve("p_link", nil, "")
	if err == nil {
		t.Fatal("expected an apply error when the referenced artifact is still pending")
	}
	if created {
		t.Fatal("link must not be created when its ref cannot resolve")
	}
	if linkP.Status != StatusApplyFailed {
		t.Fatalf("status = %s, want apply_failed", linkP.Status)
	}
	if !strings.Contains(linkP.ReviewNote, "t1") || !strings.Contains(linkP.ReviewNote, StatusPending) {
		t.Fatalf("failure note should name the token and its pending status, got: %q", linkP.ReviewNote)
	}
}

// TestRefFailsWhenArtifactRejected covers the reject-then-link case: once the
// artifact proposal is rejected the token can never resolve, so the link fails
// cleanly with a message naming the rejected status.
func TestRefFailsWhenArtifactRejected(t *testing.T) {
	created := false
	appliers := Appliers{
		CreateArtifact: func(map[string]interface{}) (string, error) { return "real-art-id", nil },
		CreateLink:     func(map[string]interface{}) (string, error) { created = true; return "link-id", nil },
	}
	svc, _ := newProposalService(appliers,
		artifactProposal("p_art", "r1", "t1"),
		linkProposal("p_link", "r1", "t1", "req-real"),
	)

	if _, err := svc.Reject("p_art", nil, "no"); err != nil {
		t.Fatalf("reject artifact: %v", err)
	}
	linkP, err := svc.Approve("p_link", nil, "")
	if err == nil {
		t.Fatal("expected an apply error when the referenced artifact was rejected")
	}
	if created {
		t.Fatal("link must not be created after its referenced artifact is rejected")
	}
	if !strings.Contains(linkP.ReviewNote, StatusRejected) {
		t.Fatalf("failure note should name the rejected status, got: %q", linkP.ReviewNote)
	}
}

// TestLinkWithoutRefsPassesThrough confirms the resolution path is transparent
// when neither endpoint is a ref: the payload reaches the applier unchanged.
func TestLinkWithoutRefsPassesThrough(t *testing.T) {
	var gotFrom, gotTo string
	appliers := Appliers{
		CreateLink: func(payload map[string]interface{}) (string, error) {
			gotFrom, _ = payload["from_id"].(string)
			gotTo, _ = payload["to_id"].(string)
			return "link-id", nil
		},
	}
	svc, _ := newProposalService(appliers, linkProposal("p_link", "r1", "art-a", "art-b"))
	if _, err := svc.Approve("p_link", nil, ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if gotFrom != "art-a" || gotTo != "art-b" {
		t.Fatalf("endpoints changed unexpectedly: from=%q to=%q", gotFrom, gotTo)
	}
}

// TestProposeLiftsRefFromPayload verifies Propose moves a payload ref token into
// the Ref column and strips it from the stored payload, so the artifact applier
// receives a clean CreateArtifactRequest.
func TestProposeLiftsRefFromPayload(t *testing.T) {
	repo := &fakeProposalRepo{items: map[string]*Proposal{}}
	svc := NewDefaultService(repo, Appliers{})
	p, err := svc.Propose("r1", "proj1", OpCreateArtifact, nil, map[string]interface{}{
		"title": "T",
		"ref":   "t1",
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if p.Ref != "t1" {
		t.Fatalf("Ref = %q, want t1", p.Ref)
	}
	if _, present := p.Payload["ref"]; present {
		t.Fatalf("ref should be stripped from payload, got: %v", p.Payload)
	}
}
