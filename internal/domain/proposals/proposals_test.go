package proposals

import "testing"

// fakeProposalRepo is an in-memory proposal store; only the methods the review
// paths touch are implemented (anything else panics via the embedded nil).
type fakeProposalRepo struct {
	Repository
	items map[string]*Proposal
}

func (f *fakeProposalRepo) FindByID(id string) (*Proposal, error) {
	p, ok := f.items[id]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (f *fakeProposalRepo) Update(p *Proposal) error {
	cp := *p
	f.items[p.ID] = &cp
	return nil
}

func newProposalService(appliers Appliers, seed ...*Proposal) (*DefaultService, *fakeProposalRepo) {
	repo := &fakeProposalRepo{items: map[string]*Proposal{}}
	for _, p := range seed {
		repo.items[p.ID] = p
	}
	return NewDefaultService(repo, appliers), repo
}

// TestResolutionFiresOnResolved locks in the funnel the run service depends on:
// approving (applied or apply-failed) or rejecting a proposal must invoke the
// OnResolved callback with the run id, so a run awaiting approval is finalized
// once its last proposal is reviewed — including the applier-failure path,
// which the HTTP handler would otherwise skip on its error return.
func TestResolutionFiresOnResolved(t *testing.T) {
	cases := []struct {
		name    string
		seed    *Proposal
		action  func(s *DefaultService) error
		wantErr bool
	}{
		{
			name: "approve applied",
			seed: &Proposal{ID: "p1", RunID: "r1", Op: OpDeleteLink, TargetID: strptr("l1"), Status: StatusPending},
			action: func(s *DefaultService) error {
				_, err := s.Approve("p1", nil, "")
				return err
			},
		},
		{
			name: "approve apply-failed still fires",
			seed: &Proposal{ID: "p1", RunID: "r1", Op: OpDeleteLink, TargetID: strptr("l1"), Status: StatusPending},
			action: func(s *DefaultService) error {
				_, err := s.Approve("p1", nil, "")
				return err
			},
			wantErr: true,
		},
		{
			name: "reject",
			seed: &Proposal{ID: "p1", RunID: "r1", Op: OpDeleteLink, TargetID: strptr("l1"), Status: StatusPending},
			action: func(s *DefaultService) error {
				_, err := s.Reject("p1", nil, "no")
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			appliers := Appliers{DeleteLink: func(string) error { return nil }}
			if tc.wantErr {
				appliers.DeleteLink = func(string) error { return errDummy }
			}
			svc, _ := newProposalService(appliers, tc.seed)
			var got []string
			svc.OnResolved(func(runID string) { got = append(got, runID) })

			err := tc.action(svc)
			if tc.wantErr && err == nil {
				t.Fatal("expected an apply error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != 1 || got[0] != "r1" {
				t.Fatalf("OnResolved calls = %v, want [r1]", got)
			}
		})
	}
}

func strptr(s string) *string { return &s }

type dummyErr struct{}

func (dummyErr) Error() string { return "apply boom" }

var errDummy = dummyErr{}
