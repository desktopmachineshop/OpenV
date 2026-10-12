package projects

import (
	"errors"
	"testing"
)

type memRepo struct{ byID map[string]*Project }

func (m *memRepo) Create(p *Project) error { m.byID[p.ID] = p; return nil }
func (m *memRepo) GetByID(id string) (*Project, error) {
	if p, ok := m.byID[id]; ok {
		return p, nil
	}
	return nil, errors.New("project not found")
}
func (m *memRepo) GetAll() ([]*Project, error)          { return nil, nil }
func (m *memRepo) ListByOrg(string) ([]*Project, error) { return nil, nil }
func (m *memRepo) Update(p *Project) error              { m.byID[p.ID] = p; return nil }
func (m *memRepo) Delete(id string) (*Removed, error)   { delete(m.byID, id); return &Removed{}, nil }
func (m *memRepo) ListChildren(id string) ([]*Project, error) {
	var out []*Project
	for _, p := range m.byID {
		if p.ParentProjectID == id {
			out = append(out, p)
		}
	}
	return out, nil
}

func str(s string) *string { return &s }

// TestParentAssignment: a parent must exist, be in the same workspace, not
// be the project itself nor one of its descendants; "" detaches; nil leaves
// the parent alone.
func TestParentAssignment(t *testing.T) {
	repo := &memRepo{byID: map[string]*Project{
		"plane": {ID: "plane", OrgID: "org"},
		"gear":  {ID: "gear", OrgID: "org", ParentProjectID: "plane"},
		"wheel": {ID: "wheel", OrgID: "org", ParentProjectID: "gear"},
		"other": {ID: "other", OrgID: "elsewhere"},
	}}
	svc := NewService(repo)
	cases := map[string]struct {
		project, parent string
		want            error
	}{
		"missing":    {"plane", "nope", ErrParentNotFound},
		"other org":  {"plane", "other", ErrParentOtherOrg},
		"self":       {"plane", "plane", ErrParentIsSelf},
		"descendant": {"plane", "wheel", ErrParentIsDescendant},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.UpdateProject(c.project, UpdateProjectRequest{ParentProjectID: str(c.parent)}); !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
	if p, err := svc.UpdateProject("wheel", UpdateProjectRequest{Name: "Wheel"}); err != nil || p.ParentProjectID != "gear" {
		t.Fatalf("a rename moved the project: %+v, %v", p, err)
	}
	if p, err := svc.UpdateProject("wheel", UpdateProjectRequest{ParentProjectID: str("")}); err != nil || p.ParentProjectID != "" {
		t.Fatalf("detach: %+v, %v", p, err)
	}
	if p, err := svc.UpdateProject("wheel", UpdateProjectRequest{ParentProjectID: str("plane")}); err != nil || p.ParentProjectID != "plane" {
		t.Fatalf("attach: %+v, %v", p, err)
	}
	chain, err := svc.Ancestors("wheel")
	if err != nil || len(chain) != 1 || chain[0].ID != "plane" {
		t.Fatalf("ancestors = %+v, %v", chain, err)
	}
	// A loop in stored data ends the walk instead of hanging.
	repo.byID["plane"].ParentProjectID = "wheel"
	if chain, err := svc.Ancestors("wheel"); err != nil || len(chain) != 1 {
		t.Fatalf("looped ancestors = %+v, %v", chain, err)
	}
}

// copyingRepo reads a copy, as a database does, so that what a service
// changes in the project it read is stored only by an Update.
type copyingRepo struct{ memRepo }

func (c *copyingRepo) GetByID(id string) (*Project, error) {
	p, err := c.memRepo.GetByID(id)
	if err != nil {
		return nil, err
	}
	cp := *p
	return &cp, nil
}

// TestAnUnknownAgentAuthIsRefusedWithItsSentinel: creating or updating with
// an agent_auth the service does not take answers ErrInvalidAgentAuth (the
// API answers 400 for it, #643) and stores nothing; the two modes it takes,
// and an empty one, pass.
func TestAnUnknownAgentAuthIsRefusedWithItsSentinel(t *testing.T) {
	repo := &copyingRepo{memRepo{byID: map[string]*Project{"p": {ID: "p", OrgID: "org", Name: "Keep", AgentAuth: AgentAuthUserAccount}}}}
	svc := NewService(repo)

	if err := svc.CreateProject(&Project{ID: "new", AgentAuth: "bogus"}); !errors.Is(err, ErrInvalidAgentAuth) {
		t.Fatalf("create: err = %v, want ErrInvalidAgentAuth", err)
	}
	if _, ok := repo.byID["new"]; ok {
		t.Error("a refused create stored the project")
	}
	if _, err := svc.UpdateProject("p", UpdateProjectRequest{Name: "Renamed", AgentAuth: "bogus"}); !errors.Is(err, ErrInvalidAgentAuth) {
		t.Fatalf("update: err = %v, want ErrInvalidAgentAuth", err)
	}
	if got := repo.byID["p"]; got.Name != "Keep" || got.AgentAuth != AgentAuthUserAccount {
		t.Errorf("a refused update stored %+v", got)
	}
	for _, mode := range []string{"", AgentAuthUserAccount, AgentAuthAPIKey} {
		if err := svc.CreateProject(&Project{ID: "ok-" + mode, AgentAuth: mode}); err != nil {
			t.Errorf("create with agent_auth %q: %v", mode, err)
		}
		if _, err := svc.UpdateProject("p", UpdateProjectRequest{AgentAuth: mode}); err != nil {
			t.Errorf("update with agent_auth %q: %v", mode, err)
		}
	}
}
