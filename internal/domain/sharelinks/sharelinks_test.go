package sharelinks

import (
	"errors"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/users"
)

type memRepo struct {
	links  map[string]*Link
	hashes map[string]string // hash -> id
}

func newMemRepo() *memRepo { return &memRepo{links: map[string]*Link{}, hashes: map[string]string{}} }

func (m *memRepo) Create(link *Link, tokenHash string) error {
	cp := *link
	m.links[link.ID] = &cp
	m.hashes[tokenHash] = link.ID
	return nil
}
func (m *memRepo) Get(id string) (*Link, error) {
	if l, ok := m.links[id]; ok {
		cp := *l
		return &cp, nil
	}
	return nil, ErrNotFound
}
func (m *memRepo) ListByProject(projectID string) ([]*Link, error) {
	var out []*Link
	for _, l := range m.links {
		if l.ProjectID == projectID {
			cp := *l
			out = append(out, &cp)
		}
	}
	return out, nil
}
func (m *memRepo) FindByTokenHash(hash string) (*Link, error) {
	id, ok := m.hashes[hash]
	if !ok {
		return nil, ErrNotFound
	}
	return m.Get(id)
}
func (m *memRepo) Revoke(id string, at time.Time) error {
	l, ok := m.links[id]
	if !ok {
		return ErrNotFound
	}
	if l.RevokedAt == nil {
		l.RevokedAt = &at
	}
	return nil
}

func TestCreateStoresOnlyTheHash(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo)
	link, token, err := svc.Create("p1", RolePublic, "  Customer  ", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if token == "" || link.ID == "" || link.Label != "Customer" || link.Role != RolePublic {
		t.Fatalf("unexpected link %+v token %q", link, token)
	}
	if _, ok := repo.hashes[token]; ok {
		t.Fatal("raw token was stored")
	}
	if _, ok := repo.hashes[users.HashToken(token)]; !ok {
		t.Fatal("token hash was not stored")
	}
	got, err := svc.Resolve(token)
	if err != nil || got.ID != link.ID {
		t.Fatalf("Resolve = %+v, %v", got, err)
	}
	if _, err := svc.Resolve(" " + token + " "); err != nil {
		t.Fatalf("Resolve should trim whitespace: %v", err)
	}
}

func TestCreateRejectsOtherRoles(t *testing.T) {
	svc := NewService(newMemRepo())
	for _, role := range []string{"", "editor", "owner", "viewer", "contributor"} {
		if _, _, err := svc.Create("p1", role, "", nil, nil); !errors.Is(err, ErrInvalidRole) {
			t.Errorf("role %q: err = %v, want ErrInvalidRole", role, err)
		}
	}
}

func TestResolveRefusesRevokedExpiredAndUnknown(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	if _, err := svc.Resolve("no-such-token"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("unknown token: %v", err)
	}
	if _, err := svc.Resolve(""); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("empty token: %v", err)
	}

	revoked, tokenR, _ := svc.Create("p1", RoleReviewer, "", nil, nil)
	if err := svc.Revoke(revoked.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := svc.Resolve(tokenR); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("revoked token resolved: %v", err)
	}
	// Revoking twice keeps the first time.
	first := *repo.links[revoked.ID].RevokedAt
	svc.now = func() time.Time { return now.Add(time.Hour) }
	_ = svc.Revoke(revoked.ID)
	if !repo.links[revoked.ID].RevokedAt.Equal(first) {
		t.Error("second revoke moved revoked_at")
	}

	expiry := now.Add(24 * time.Hour)
	_, tokenE, _ := svc.Create("p1", RolePublic, "", nil, &expiry)
	svc.now = func() time.Time { return now }
	if _, err := svc.Resolve(tokenE); err != nil {
		t.Errorf("link before expiry: %v", err)
	}
	svc.now = func() time.Time { return expiry }
	if _, err := svc.Resolve(tokenE); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("expired token resolved: %v", err)
	}
}

func TestUsable(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Minute), now.Add(time.Minute)
	cases := []struct {
		name string
		link *Link
		want bool
	}{
		{"nil", nil, false},
		{"plain", &Link{}, true},
		{"revoked", &Link{RevokedAt: &past}, false},
		{"expires later", &Link{ExpiresAt: &future}, true},
		{"expired", &Link{ExpiresAt: &past}, false},
	}
	for _, c := range cases {
		if got := c.link.Usable(now); got != c.want {
			t.Errorf("%s: Usable = %v, want %v", c.name, got, c.want)
		}
	}
}
