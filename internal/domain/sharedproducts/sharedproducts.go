// Package sharedproducts holds the community pool of joke demo products used
// by the new-project wizard's "random product" roller.
//
// It is deliberately the one cross-tenant, user-writable surface in OpenV:
// anything published here is visible to every workspace, which is the whole
// point (the roll list grows as people invent products). That makes it the
// one place where text authored in one tenant reaches another tenant's
// screen — and, because a rolled product seeds the guided wizard, another
// tenant's *agent context*. Three consequences shape this package:
//
//  1. Everything stored is scrubbed to inert plain text on write
//     (Sanitize): no line breaks, no backticks or code fences, no angle
//     brackets, no links, no "openv-suggestion" marker. A shared product is
//     six short phrases and nothing else, so nothing it contains can be
//     mistaken for markup, for a fenced suggestion block, or for a click
//     target. Callers that embed this text in a model prompt must still
//     fence it as untrusted data (see buildGuidedCopilotPrompt).
//  2. Every publication is attributable to a person. A product a member's
//     agent invents is published automatically — that is how the pool grows
//     without anyone doing chores — but the request carries that member's
//     own session, so the row records who published it and the per-workspace
//     daily cap applies to them. Agent-run tokens and host worker keys
//     cannot publish at all, so nothing enters the pool that no account
//     answers for. Note what this deliberately does not promise: nobody
//     reviews an invention before it is visible to other tenants. Removal
//     (below) is the control that covers it.
//  3. Everything is removable: any signed-in user can report an entry, a
//     handful of reports hides it automatically, and a platform admin can
//     delete it outright.
//
// Author identity is stored for rate limiting and takedown only, and is
// never serialized to clients — no tenant learns who published what.
package sharedproducts

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Field length caps. A shared product is a one-line joke, not a document;
// short caps bound the storage, the render, and how much attacker-controlled
// text can ever reach a prompt.
const (
	MaxCategory    = 40
	MaxName        = 60
	MaxDescription = 240
	MaxVision      = 240
	MaxProblem     = 300
	MaxTargetUsers = 240
)

// Pool limits.
const (
	// DefaultDailyOrgLimit caps how many products one workspace may publish
	// per rolling 24 hours.
	DefaultDailyOrgLimit = 20
	// DefaultPoolLimit caps the whole visible pool. The roller only needs
	// variety, not an unbounded table, and a ceiling keeps the "little to no
	// cost" promise true even if publishing is scripted.
	DefaultPoolLimit = 5000
	// ReportsToHide is how many distinct reports auto-hide an entry pending
	// admin review.
	ReportsToHide = 3
	// DefaultListLimit / MaxListLimit bound a list request.
	DefaultListLimit = 200
	MaxListLimit     = 500
)

// Errors.
var (
	ErrNotFound        = errors.New("shared product not found")
	ErrDuplicate       = errors.New("a product with that name has already been shared")
	ErrEmptyField      = errors.New("every field is required: category, name, description, vision, problem, target_users")
	ErrTooLong         = errors.New("a field is longer than the shared pool allows")
	ErrLinksNotAllowed = errors.New("shared products cannot contain links")
	ErrDisallowedText  = errors.New("shared products cannot contain agent instruction markup")
	ErrRateLimited     = errors.New("this workspace has shared too many products today")
	ErrPoolFull        = errors.New("the shared product pool is full")
	ErrNotPublishable  = errors.New("only a signed-in person can share a product")
)

// Product is one community-shared demo product.
//
// The exported JSON is exactly what the wizard card renders. Author columns
// carry no json tags on purpose: they exist for rate limiting and takedown,
// and must never cross a tenant boundary.
type Product struct {
	ID          string    `json:"id"`
	Category    string    `json:"category"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Vision      string    `json:"vision"`
	Problem     string    `json:"problem"`
	TargetUsers string    `json:"target_users"`
	CreatedAt   time.Time `json:"created_at"`

	// NameKey is the normalized name the pool dedupes on.
	NameKey string `json:"-"`
	// CreatedByOrg / CreatedByUser are moderation metadata, never published.
	CreatedByOrg  string `json:"-"`
	CreatedByUser string `json:"-"`
	Reports       int    `json:"-"`
	Hidden        bool   `json:"-"`
}

// Repository is the storage port.
type Repository interface {
	// ListVisible returns unhidden products, newest first.
	ListVisible(limit int) ([]*Product, error)
	Create(p *Product) error
	// CountByOrgSince counts an org's publications in a window (rate limit).
	CountByOrgSince(orgID string, since time.Time) (int, error)
	// CountVisible is the pool ceiling check.
	CountVisible() (int, error)
	// AddReport records one person's report and returns how many distinct
	// people have now reported the entry.
	AddReport(id, userID string) (int, error)
	// SetHidden hides or unhides an entry.
	SetHidden(id string, hidden bool) error
	Delete(id string) error
}

// Service is the shared-pool use case surface.
type Service interface {
	List(limit int) ([]*Product, error)
	// Publish stores a product on behalf of a signed-in user in a workspace.
	Publish(in Product, orgID, userID string) (*Product, error)
	// Report flags an entry on one person's behalf; it auto-hides once
	// ReportsToHide distinct people have flagged it.
	Report(id, userID string) error
	// Delete removes an entry outright (platform admin).
	Delete(id string) error
}

// DefaultService is the standard implementation.
type DefaultService struct {
	repo       Repository
	dailyLimit int
	poolLimit  int
	now        func() time.Time
}

// NewDefaultService builds a service. Non-positive limits fall back to the
// package defaults.
func NewDefaultService(repo Repository, dailyLimit, poolLimit int) *DefaultService {
	if dailyLimit <= 0 {
		dailyLimit = DefaultDailyOrgLimit
	}
	if poolLimit <= 0 {
		poolLimit = DefaultPoolLimit
	}
	return &DefaultService{repo: repo, dailyLimit: dailyLimit, poolLimit: poolLimit, now: time.Now}
}

// List returns the visible pool, newest first.
func (s *DefaultService) List(limit int) ([]*Product, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	if limit > MaxListLimit {
		limit = MaxListLimit
	}
	return s.repo.ListVisible(limit)
}

// Publish sanitizes, rate-limits and stores one product.
func (s *DefaultService) Publish(in Product, orgID, userID string) (*Product, error) {
	// A publication is always attributable to a person in a workspace: the
	// handler rejects agent-run and worker credentials before reaching here,
	// and this is the backstop.
	if strings.TrimSpace(orgID) == "" || strings.TrimSpace(userID) == "" {
		return nil, ErrNotPublishable
	}

	clean, err := Sanitize(in)
	if err != nil {
		return nil, err
	}

	count, err := s.repo.CountByOrgSince(orgID, s.now().Add(-24*time.Hour))
	if err != nil {
		return nil, err
	}
	if count >= s.dailyLimit {
		return nil, ErrRateLimited
	}

	total, err := s.repo.CountVisible()
	if err != nil {
		return nil, err
	}
	if total >= s.poolLimit {
		return nil, ErrPoolFull
	}

	clean.ID = uuid.New().String()
	clean.CreatedAt = s.now().UTC()
	clean.CreatedByOrg = orgID
	clean.CreatedByUser = userID
	if err := s.repo.Create(&clean); err != nil {
		return nil, err
	}
	return &clean, nil
}

// Report flags an entry, hiding it once enough distinct people have flagged
// it. Reporting is per person (the repository dedupes), so hiding something
// from every workspace always takes several accounts, not several clicks.
func (s *DefaultService) Report(id, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return ErrNotPublishable
	}
	total, err := s.repo.AddReport(id, userID)
	if err != nil {
		return err
	}
	if total >= ReportsToHide {
		return s.repo.SetHidden(id, true)
	}
	return nil
}

// Delete removes an entry.
func (s *DefaultService) Delete(id string) error { return s.repo.Delete(id) }

// urlPattern catches the link shapes worth refusing: a scheme, a bare
// www. host, and the common "foo.com/bar" form. A joke product never needs a
// link, and refusing them removes the phishing target a cross-tenant card
// would otherwise offer.
var urlPattern = regexp.MustCompile(`(?i)(https?://|www\.|\b[a-z0-9-]+\.(com|net|org|io|ai|co|dev|app|xyz|ru|cn)\b)`)

// markupChars are stripped outright: backticks (code fences, and with them
// any fenced openv-suggestion block) and angle brackets (HTML-looking text,
// even though the renderer does not execute it).
var markupChars = strings.NewReplacer("`", "", "<", "", ">", "")

// Sanitize scrubs a submitted product to inert single-line plain text and
// validates it. It returns the cleaned product or the first problem found.
//
// Stripping happens before validation so an agent's stray formatting does not
// cost the publisher their invention; the two hard refusals — links and the
// suggestion marker — are things a legitimate joke product never contains.
func Sanitize(in Product) (Product, error) {
	out := Product{
		Category:    sanitizeField(in.Category),
		Name:        sanitizeField(in.Name),
		Description: sanitizeField(in.Description),
		Vision:      sanitizeField(in.Vision),
		Problem:     sanitizeField(in.Problem),
		TargetUsers: sanitizeField(in.TargetUsers),
	}

	fields := []struct {
		value string
		max   int
	}{
		{out.Category, MaxCategory},
		{out.Name, MaxName},
		{out.Description, MaxDescription},
		{out.Vision, MaxVision},
		{out.Problem, MaxProblem},
		{out.TargetUsers, MaxTargetUsers},
	}
	for _, f := range fields {
		if f.value == "" {
			return Product{}, ErrEmptyField
		}
		if len([]rune(f.value)) > f.max {
			return Product{}, ErrTooLong
		}
		if urlPattern.MatchString(f.value) {
			return Product{}, ErrLinksNotAllowed
		}
		if strings.Contains(strings.ToLower(f.value), "openv-suggestion") {
			return Product{}, ErrDisallowedText
		}
	}

	out.NameKey = NameKey(out.Name)
	if out.NameKey == "" {
		return Product{}, ErrEmptyField
	}
	return out, nil
}

// sanitizeField flattens one field to a single line of printable text:
// control characters and line breaks become spaces, markup characters are
// dropped, runs of whitespace collapse, and the result is trimmed.
func sanitizeField(s string) string {
	s = markupChars.Replace(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(' ')
		case unicode.IsControl(r), !unicode.IsPrint(r):
			// Drop control and non-printing runes outright, including the
			// bidi overrides and zero-width joiners used to disguise text.
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// NameKey normalizes a product name for deduplication: lowercase, letters and
// digits only, so "Kevinproof" and "kevin-proof!" collide.
func NameKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
