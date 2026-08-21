package workerkeys

import (
	"testing"
	"time"
)

type fakeKeyRepo struct {
	keys map[string]*Key // by hash
}

func newFakeKeyRepo() *fakeKeyRepo { return &fakeKeyRepo{keys: map[string]*Key{}} }

func (f *fakeKeyRepo) Save(k *Key) error { f.keys[k.KeyHash] = k; return nil }
func (f *fakeKeyRepo) List(orgID string) ([]*Key, error) {
	var out []*Key
	for _, k := range f.keys {
		if k.OrgID == orgID {
			out = append(out, k)
		}
	}
	return out, nil
}
func (f *fakeKeyRepo) FindByID(id string) (*Key, error) {
	for _, k := range f.keys {
		if k.ID == id {
			return k, nil
		}
	}
	return nil, nil
}
func (f *fakeKeyRepo) FindByHash(hash string) (*Key, error) { return f.keys[hash], nil }
func (f *fakeKeyRepo) FindPersonal(orgID, userID string) (*Key, error) {
	for _, k := range f.keys {
		if k.OrgID == orgID && k.UserID != nil && *k.UserID == userID && !k.Revoked {
			return k, nil
		}
	}
	return nil, nil
}
func (f *fakeKeyRepo) Revoke(id string) error {
	for _, k := range f.keys {
		if k.ID == id {
			k.Revoked = true
		}
	}
	return nil
}
func (f *fakeKeyRepo) Touch(id string, at time.Time) error { return nil }
func (f *fakeKeyRepo) HasOnlinePersonalKey(orgID, userID string, since time.Time) (bool, error) {
	for _, k := range f.keys {
		if k.OrgID == orgID && k.UserID != nil && *k.UserID == userID && !k.Revoked && k.LastUsedAt != nil && k.LastUsedAt.After(since) {
			return true, nil
		}
	}
	return false, nil
}

func TestCreateResolveRoundTrip(t *testing.T) {
	s := NewDefaultService(newFakeKeyRepo())
	key, plaintext, err := s.Create("org-a", "test", nil, nil)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if plaintext == "" || key.KeyHash == plaintext {
		t.Fatalf("plaintext should be returned and differ from stored hash")
	}
	resolved, err := s.Resolve(plaintext)
	if err != nil || resolved.OrgID != "org-a" || resolved.KeyID != key.ID || resolved.UserID != "" {
		t.Fatalf("Resolve = (%+v, %v), want org-a workspace key %s", resolved, err, key.ID)
	}
}

func TestResolveRejectsRevokedAndUnknown(t *testing.T) {
	repo := newFakeKeyRepo()
	s := NewDefaultService(repo)
	key, plaintext, _ := s.Create("org-a", "test", nil, nil)
	if err := s.Revoke("org-a", key.ID); err != nil {
		t.Fatalf("Revoke failed: %v", err)
	}
	if resolved, _ := s.Resolve(plaintext); resolved.OrgID != "" {
		t.Errorf("revoked key resolved to org %q, want none", resolved.OrgID)
	}
	if resolved, _ := s.Resolve("not-a-key"); resolved.OrgID != "" {
		t.Errorf("unknown key resolved to org %q, want none", resolved.OrgID)
	}
}

func TestRevokeEnforcesOrgOwnership(t *testing.T) {
	s := NewDefaultService(newFakeKeyRepo())
	key, _, _ := s.Create("org-a", "test", nil, nil)
	if err := s.Revoke("org-b", key.ID); err == nil {
		t.Errorf("expected foreign-org revoke to fail")
	}
}

func TestPersonalKeyResolveAndRotation(t *testing.T) {
	s := NewDefaultService(newFakeKeyRepo())
	uid := "user-1"
	first, firstPlain, err := s.Create("org-a", "bob's runner", &uid, &uid)
	if err != nil {
		t.Fatalf("Create personal failed: %v", err)
	}
	resolved, _ := s.Resolve(firstPlain)
	if resolved.UserID != uid {
		t.Fatalf("personal key should resolve to user %s, got %+v", uid, resolved)
	}
	// Rotation revokes the previous personal key.
	_, secondPlain, err := s.Create("org-a", "bob's runner", &uid, &uid)
	if err != nil {
		t.Fatalf("rotate failed: %v", err)
	}
	if resolved, _ := s.Resolve(firstPlain); resolved.OrgID != "" {
		t.Errorf("old personal key %s should be revoked after rotation", first.ID)
	}
	if resolved, _ := s.Resolve(secondPlain); resolved.UserID != uid {
		t.Errorf("new personal key should resolve to the user")
	}
}
