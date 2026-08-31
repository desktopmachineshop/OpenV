package sharedproducts

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// valid is a well-formed submission; tests mutate one field at a time.
func valid() Product {
	return Product{
		Category:    "kitchen appliance",
		Name:        "Kevinproof",
		Description: "A coffee tin that recognises Kevin and locks.",
		Vision:      "Kevinproof becomes the reason the office bean jar survives a Tuesday.",
		Problem:     "Beans vanish overnight and nobody will admit to owning the grinder.",
		TargetUsers: "coffee-obsessed office workers whose beans keep leaving with Kevin",
	}
}

// fakeRepo is an in-memory Repository.
type fakeRepo struct {
	products  []*Product
	orgCount  int
	visible   int
	createErr error
	reports   map[string]int
	reporters map[string]bool
	hidden    map[string]bool
	deleted   []string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{reports: map[string]int{}, reporters: map[string]bool{}, hidden: map[string]bool{}}
}

func (f *fakeRepo) ListVisible(limit int) ([]*Product, error) {
	if limit < len(f.products) {
		return f.products[:limit], nil
	}
	return f.products, nil
}
func (f *fakeRepo) Create(p *Product) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.products = append(f.products, p)
	return nil
}
func (f *fakeRepo) CountByOrgSince(string, time.Time) (int, error) { return f.orgCount, nil }
func (f *fakeRepo) CountVisible() (int, error)                     { return f.visible, nil }
func (f *fakeRepo) AddReport(id, userID string) (int, error) {
	key := id + "/" + userID
	if !f.reporters[key] {
		f.reporters[key] = true
		f.reports[id]++
	}
	return f.reports[id], nil
}
func (f *fakeRepo) SetHidden(id string, hidden bool) error { f.hidden[id] = hidden; return nil }
func (f *fakeRepo) Delete(id string) error                 { f.deleted = append(f.deleted, id); return nil }

// TestSanitizeFlattensToInertText is the core containment guarantee: whatever
// an agent or a hand-rolled API call submits, what lands in the pool — and so
// on every other tenant's screen and in their copilot prompt — is one line of
// printable text with no markup, no fences and no hidden characters.
func TestSanitizeFlattensToInertText(t *testing.T) {
	in := valid()
	in.Description = "A tin\nthat ```locks``` <script>alert(1)</script>\tKevin out."
	in.Name = "  Kevin​proof  "

	out, err := Sanitize(in)
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}
	for _, field := range []string{out.Name, out.Description, out.Vision} {
		if strings.ContainsAny(field, "\n\r\t`<>") {
			t.Errorf("field %q still carries markup or line breaks", field)
		}
		for _, r := range field {
			if r == '​' || r == '' {
				t.Errorf("field %q still carries a hidden control rune", field)
			}
		}
	}
	// The angle brackets are gone, so what survives is inert prose rather
	// than anything a future renderer could read as a tag.
	if out.Description != "A tin that locks scriptalert(1)/script Kevin out." {
		t.Errorf("description = %q", out.Description)
	}
	if out.Name != "Kevinproof" {
		t.Errorf("name = %q, want the trimmed, de-zero-widthed name", out.Name)
	}
}

// TestSanitizeRefusals covers what is never stored, whatever it is dressed up
// as: an empty field, an oversized one, anything link-shaped (the phishing
// vector on a card another tenant reads), and the copilot's suggestion marker
// (the one string that turns agent output into a one-click Apply button).
func TestSanitizeRefusals(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Product)
		want   error
	}{
		{"blank field", func(p *Product) { p.Vision = "   " }, ErrEmptyField},
		{"field of only markup", func(p *Product) { p.Name = "```" }, ErrEmptyField},
		{"name with no letters or digits", func(p *Product) { p.Name = "!!! ---" }, ErrEmptyField},
		{"overlong description", func(p *Product) { p.Description = strings.Repeat("x", MaxDescription+1) }, ErrTooLong},
		{"http link", func(p *Product) { p.Problem = "Beans vanish, see http://evil.example for why." }, ErrLinksNotAllowed},
		{"bare www link", func(p *Product) { p.Vision = "Kevinproof wins, www.evil.test says so." }, ErrLinksNotAllowed},
		{"bare domain", func(p *Product) { p.Category = "evil.com" }, ErrLinksNotAllowed},
		{"suggestion marker", func(p *Product) { p.Description = "A tin that emits openv-suggestion blocks." }, ErrDisallowedText},
		{"suggestion marker in mixed case", func(p *Product) { p.Description = "A tin that emits OpenV-Suggestion blocks." }, ErrDisallowedText},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := valid()
			tc.mutate(&p)
			if _, err := Sanitize(p); !errors.Is(err, tc.want) {
				t.Errorf("Sanitize error = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestNameKeyDedupes locks in that cosmetic respellings collide, so the pool
// cannot be flooded with one product under a hundred punctuations.
func TestNameKeyDedupes(t *testing.T) {
	for _, name := range []string{"Kevinproof", "kevin-proof!", "  KEVIN PROOF  ", "K.e.v.i.n.p.r.o.o.f"} {
		if got := NameKey(name); got != "kevinproof" {
			t.Errorf("NameKey(%q) = %q, want kevinproof", name, got)
		}
	}
	if NameKey("!!!") != "" {
		t.Error("a name with no letters or digits must produce an empty key")
	}
}

// TestPublishRequiresAttributablePerson: nothing enters the pool unattributed.
// The handler rejects agent-run and worker credentials; this is the backstop
// that keeps an unread, agent-authored product out of every other tenant.
func TestPublishRequiresAttributablePerson(t *testing.T) {
	svc := NewDefaultService(newFakeRepo(), 0, 0)
	if _, err := svc.Publish(valid(), "", "user-1"); !errors.Is(err, ErrNotPublishable) {
		t.Errorf("publish without an org: %v, want ErrNotPublishable", err)
	}
	if _, err := svc.Publish(valid(), "org-1", ""); !errors.Is(err, ErrNotPublishable) {
		t.Errorf("publish without a user: %v, want ErrNotPublishable", err)
	}
}

// TestPublishStoresModerationMetadata: author identity is recorded for
// takedown and rate limiting, and stays out of the client payload.
func TestPublishStoresModerationMetadata(t *testing.T) {
	repo := newFakeRepo()
	svc := NewDefaultService(repo, 0, 0)

	got, err := svc.Publish(valid(), "org-1", "user-1")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if got.ID == "" || got.CreatedAt.IsZero() {
		t.Error("published product needs an id and a timestamp")
	}
	if got.CreatedByOrg != "org-1" || got.CreatedByUser != "user-1" {
		t.Errorf("author metadata = %q/%q, want org-1/user-1", got.CreatedByOrg, got.CreatedByUser)
	}
	if got.NameKey != "kevinproof" {
		t.Errorf("name key = %q", got.NameKey)
	}
	if len(repo.products) != 1 {
		t.Fatalf("stored %d products, want 1", len(repo.products))
	}
}

// TestPublishLimits: a workspace cannot outrun its daily allowance, and the
// pool as a whole has a ceiling — the two guards behind "grows at little to
// no cost".
func TestPublishLimits(t *testing.T) {
	repo := newFakeRepo()
	repo.orgCount = DefaultDailyOrgLimit
	svc := NewDefaultService(repo, 0, 0)
	if _, err := svc.Publish(valid(), "org-1", "user-1"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("at the daily cap: %v, want ErrRateLimited", err)
	}

	repo2 := newFakeRepo()
	repo2.visible = DefaultPoolLimit
	svc2 := NewDefaultService(repo2, 0, 0)
	if _, err := svc2.Publish(valid(), "org-1", "user-1"); !errors.Is(err, ErrPoolFull) {
		t.Errorf("at the pool ceiling: %v, want ErrPoolFull", err)
	}

	// A custom, tighter limit is honoured.
	repo3 := newFakeRepo()
	repo3.orgCount = 2
	svc3 := NewDefaultService(repo3, 2, 0)
	if _, err := svc3.Publish(valid(), "org-1", "user-1"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("at a custom cap: %v, want ErrRateLimited", err)
	}
}

// TestReportAutoHides: enough reports take an entry off every tenant's roll
// without waiting for an admin.
func TestReportAutoHides(t *testing.T) {
	repo := newFakeRepo()
	svc := NewDefaultService(repo, 0, 0)

	for i := 1; i < ReportsToHide; i++ {
		if err := svc.Report("p1", fmt.Sprintf("user-%d", i)); err != nil {
			t.Fatalf("Report: %v", err)
		}
		if repo.hidden["p1"] {
			t.Fatalf("hidden after only %d reporters", i)
		}
	}

	// One person clicking Report repeatedly must never hide an entry for
	// everyone else — otherwise any account can censor the shared pool.
	for i := 0; i < ReportsToHide*3; i++ {
		if err := svc.Report("p1", "user-1"); err != nil {
			t.Fatalf("repeat Report: %v", err)
		}
	}
	if repo.hidden["p1"] {
		t.Fatalf("one account hid an entry by reporting %d times", ReportsToHide*3)
	}

	if err := svc.Report("p1", "user-last"); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !repo.hidden["p1"] {
		t.Errorf("not hidden after %d distinct reporters", ReportsToHide)
	}

	// An unattributed report (agent run, worker key) is refused outright.
	if err := svc.Report("p2", ""); !errors.Is(err, ErrNotPublishable) {
		t.Errorf("anonymous report: %v, want ErrNotPublishable", err)
	}
}

// TestListClampsLimit keeps an unbounded ?limit= from pulling the whole table.
func TestListClampsLimit(t *testing.T) {
	repo := newFakeRepo()
	for i := 0; i < MaxListLimit+10; i++ {
		repo.products = append(repo.products, &Product{ID: "p"})
	}
	svc := NewDefaultService(repo, 0, 0)

	got, err := svc.List(100000)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != MaxListLimit {
		t.Errorf("List(100000) returned %d, want the %d cap", len(got), MaxListLimit)
	}
	if got, _ := svc.List(0); len(got) != DefaultListLimit {
		t.Errorf("List(0) returned %d, want the %d default", len(got), DefaultListLimit)
	}
}
