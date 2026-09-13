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
func (m *memRepo) Delete(id string) error               { delete(m.byID, id); return nil }
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
