package usertokens

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/openv/requirements-platform/internal/domain/providers"
)

type memRepo struct{ rows map[string]*Token }

func (m *memRepo) Get(u, p string) (*Token, error) { return m.rows[u+"/"+p], nil }
func (m *memRepo) Upsert(t *Token) error           { m.rows[t.UserID+"/"+t.Provider] = t; return nil }
func (m *memRepo) Delete(u, p string) error        { delete(m.rows, u+"/"+p); return nil }

func newKey(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

const sample = "sk-ant-oat01-abcdefghijklmnopqrstuvwxyz0123456789_-AbCd"

func TestSaveSealsAndForRunOpens(t *testing.T) {
	repo := &memRepo{rows: map[string]*Token{}}
	s, err := NewDefaultService(repo, newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.Save("u1", providers.ProviderClaudeCode, "  "+sample+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Connected || st.Last4 != "AbCd" || st.Expired {
		t.Fatalf("status = %+v", st)
	}
	row := repo.rows["u1/claude-code"]
	if strings.Contains(string(row.Sealed), "sk-ant") {
		t.Fatal("token stored in the clear")
	}
	got, err := s.ForRun("u1", providers.ProviderClaudeCode)
	if err != nil || got != sample {
		t.Fatalf("ForRun = %q, %v", got, err)
	}
	if got, _ := s.ForRun("u2", providers.ProviderClaudeCode); got != "" {
		t.Fatal("another member's run got the token")
	}
	// A row moved to another member does not open.
	repo.rows["u2/claude-code"] = &Token{UserID: "u2", Provider: row.Provider, Sealed: row.Sealed, ExpiresAt: row.ExpiresAt}
	if got, err := s.ForRun("u2", providers.ProviderClaudeCode); got != "" || err == nil {
		t.Fatalf("moved row opened: %q, %v", got, err)
	}
}

func TestExpiredTokenIsNotHandedOut(t *testing.T) {
	repo := &memRepo{rows: map[string]*Token{}}
	s, _ := NewDefaultService(repo, newKey(t))
	if _, err := s.Save("u1", providers.ProviderClaudeCode, sample); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Now().UTC().Add(Lifetime + time.Hour) }
	if got, _ := s.ForRun("u1", providers.ProviderClaudeCode); got != "" {
		t.Fatal("expired token handed out")
	}
	if st, _ := s.Status("u1", providers.ProviderClaudeCode); !st.Expired {
		t.Fatal("status not expired")
	}
}

func TestValidationAndUnavailable(t *testing.T) {
	repo := &memRepo{rows: map[string]*Token{}}
	s, _ := NewDefaultService(repo, "")
	if _, err := s.Save("u1", providers.ProviderClaudeCode, sample); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("no key: %v", err)
	}
	for _, bad := range []string{"", "sk-ant-api03-xxxxxxxxxxxxxxxxxxxxxxxx", "sk-ant-oat01-short", sample + " x"} {
		if _, err := s.Save("u1", providers.ProviderClaudeCode, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if _, err := s.Status("u1", "codex-cli"); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown provider accepted")
	}
	if _, err := NewDefaultService(repo, base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("short key accepted")
	}
}
