package providers

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type memKeyRepo struct {
	rows map[string]*StoredKey
}

func (m *memKeyRepo) UpsertKey(k *StoredKey) error {
	cp := *k
	m.rows[k.OrgID+"/"+k.KeyEnv] = &cp
	return nil
}

func (m *memKeyRepo) FindKey(orgID, keyEnv string) (*StoredKey, error) {
	return m.rows[orgID+"/"+keyEnv], nil
}

func (m *memKeyRepo) ListKeys(orgID string) ([]*StoredKey, error) {
	var out []*StoredKey
	for _, k := range m.rows {
		if k.OrgID == orgID {
			out = append(out, k)
		}
	}
	return out, nil
}

func (m *memKeyRepo) DeleteKey(orgID, keyEnv string) (bool, error) {
	_, ok := m.rows[orgID+"/"+keyEnv]
	delete(m.rows, orgID+"/"+keyEnv)
	return ok, nil
}

// testSecretsKey is a stand-in OPENV_SECRETS_KEY of the minimum length.
var testSecretsKey = strings.Repeat("k", MinSecretsKeyLength)

func newTestKeyService(t *testing.T) (*DefaultKeyService, *memKeyRepo) {
	t.Helper()
	sealer, err := NewSealer(testSecretsKey)
	if err != nil {
		t.Fatal(err)
	}
	repo := &memKeyRepo{rows: map[string]*StoredKey{}}
	return NewKeyService(repo, sealer), repo
}

// TestPastedKeyIsSealedAtRestAndRevealedForItsWorkspace: a pasted key is
// stored sealed (its value appears nowhere in the row), shows only its last
// four characters, and opens again for the same workspace and variable.
func TestPastedKeyIsSealedAtRestAndRevealedForItsWorkspace(t *testing.T) {
	svc, repo := newTestKeyService(t)
	const value = "fake-test-key-value-WXYZ"
	stored, err := svc.SetKey("org-1", "ANTHROPIC_API_KEY", "  "+value+"\n", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Last4 != "WXYZ" || stored.UpdatedBy != "user-1" {
		t.Errorf("stored = %+v, want last4 WXYZ by user-1", stored)
	}
	row := repo.rows["org-1/ANTHROPIC_API_KEY"]
	if row == nil || bytes.Contains(row.Sealed, []byte(value)) || bytes.Contains(row.Sealed, []byte("fake-test-key")) {
		t.Fatalf("the stored row holds the key in the clear: %q", row.Sealed)
	}
	got, err := svc.RevealKey("org-1", "ANTHROPIC_API_KEY")
	if err != nil || got != value {
		t.Fatalf("RevealKey = %q, %v; want the pasted key, trimmed", got, err)
	}
	if got, err := svc.RevealKey("org-2", "ANTHROPIC_API_KEY"); err != nil || got != "" {
		t.Errorf("another workspace's RevealKey = %q, %v; want none", got, err)
	}
}

// TestSealedKeyDoesNotOpenElsewhere: the seal binds the workspace and the
// variable, so a row moved to another workspace or variable does not open,
// nor does one sealed under another secrets key.
func TestSealedKeyDoesNotOpenElsewhere(t *testing.T) {
	sealer, _ := NewSealer(testSecretsKey)
	sealed, err := sealer.Seal("org-1", "ANTHROPIC_API_KEY", "fake-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sealer.Open("org-2", "ANTHROPIC_API_KEY", sealed); err == nil {
		t.Error("a key sealed for org-1 opened for org-2")
	}
	if _, err := sealer.Open("org-1", "OPENAI_API_KEY", sealed); err == nil {
		t.Error("a key sealed for ANTHROPIC_API_KEY opened for OPENAI_API_KEY")
	}
	other, _ := NewSealer(strings.Repeat("z", MinSecretsKeyLength))
	if _, err := other.Open("org-1", "ANTHROPIC_API_KEY", sealed); err == nil {
		t.Error("a key opened under a different secrets key")
	}
	if _, err := NewSealer("short"); err == nil {
		t.Error("a secrets key shorter than the minimum was accepted")
	}
}

// TestSetKeyRefusesBadInput: only a catalogued variable, and a key that is
// one unbroken token.
func TestSetKeyRefusesBadInput(t *testing.T) {
	svc, _ := newTestKeyService(t)
	for _, tc := range []struct{ env, value string }{
		{"", "fake-1"},
		{"DATABASE_URL", "fake-1"},
		{"ANTHROPIC_API_KEY", "   "},
		{"ANTHROPIC_API_KEY", "fake-1 fake-2"},
		{"ANTHROPIC_API_KEY", strings.Repeat("k", maxKeyLength+1)},
	} {
		if _, err := svc.SetKey("org-1", tc.env, tc.value, "u"); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("SetKey(%q, %q) = %v, want ErrInvalidKey", tc.env, tc.value, err)
		}
	}
}

// TestKeyStoreOffWithoutSecretsKey: with no sealer the store refuses writes
// and reveals nothing.
func TestKeyStoreOffWithoutSecretsKey(t *testing.T) {
	svc := NewKeyService(&memKeyRepo{rows: map[string]*StoredKey{}}, nil)
	if svc.KeysEnabled() {
		t.Error("KeysEnabled with no sealer")
	}
	if _, err := svc.SetKey("org-1", "ANTHROPIC_API_KEY", "fake-1", "u"); !errors.Is(err, ErrKeyStoreOff) {
		t.Errorf("SetKey = %v, want ErrKeyStoreOff", err)
	}
	if got, err := svc.RevealKey("org-1", "ANTHROPIC_API_KEY"); got != "" || err != nil {
		t.Errorf("RevealKey = %q, %v; want nothing", got, err)
	}
}
