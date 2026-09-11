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
// Votes (the "top" filters in the roller) follow the same per-person rule as
// reports: one account, one vote, stored as a (product, user) pair so the
// count cannot be clicked up, and never served back as identity. A hidden
// entry can be neither listed nor voted for.
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
	// VoteWindowDays is the "this week" window the weekly leaderboard counts
	// over. It is a rolling window computed by the database, not a calendar
	// week, so a product that was popular last Tuesday falls out of the list
	// on its own rather than at a weekly reset everyone has to wait for.
	VoteWindowDays = 7
)

// Sort names an ordering for a list request.
type Sort string

// The orderings a caller may ask for. Anything else is refused rather than
// silently treated as recent: a typo in a filter should not look like an
// answer.
const (
	// SortRecent is the pool's default: newest first, which is what the
	// roller has always seen.
	SortRecent Sort = "recent"
	// SortTop is most-voted first, over the whole life of the pool.
	SortTop Sort = "top"
	// SortTopWeek is most-voted first, counting only the last VoteWindowDays.
	SortTopWeek Sort = "top_week"
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
	ErrNotVotable      = errors.New("only a signed-in person can vote for a shared product")
	ErrBadSort         = errors.New("sort must be one of: recent, top, top_week")
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

	// Votes is the all-time count of distinct people who voted for this
	// entry; VotesWeek counts only the last VoteWindowDays. Voted is the
	// calling person's own vote — false for a caller with no session user
	// (a runner key), who cannot vote at all.
	//
	// Votes are a popularity signal, not identity: like reports, who voted
	// is stored per person so one account cannot vote twice, and is never
	// served to anyone.
	Votes     int  `json:"votes"`
	VotesWeek int  `json:"votes_week"`
	Voted     bool `json:"voted"`

	// NameKey is the normalized name the pool dedupes on.
	NameKey string `json:"-"`
	// CreatedByOrg / CreatedByUser are moderation metadata, never published.
	CreatedByOrg  string `json:"-"`
	CreatedByUser string `json:"-"`
	Reports       int    `json:"-"`
	Hidden        bool   `json:"-"`
}

// ListOptions is one read of the pool: how many rows, in what order, and on
// whose behalf (so each row can carry that person's own vote back).
type ListOptions struct {
	// Limit caps the rows returned; the service bounds it.
	Limit int
	// Sort is the ordering. The empty value means SortRecent.
	Sort Sort
	// ViewerID is the signed-in person, or "" for a caller with no session
	// user — who then sees Voted false on every row.
	ViewerID string
}

// VoteCounts is what a vote or unvote settles on: the entry's totals and
// whether the voter's own vote now stands. It is the vote endpoints' payload.
type VoteCounts struct {
	Votes     int  `json:"votes"`
	VotesWeek int  `json:"votes_week"`
	Voted     bool `json:"voted"`
}

// Repository is the storage port.
type Repository interface {
	// ListVisible returns unhidden products in the requested order, each
	// carrying its vote counts and the viewer's own vote.
	ListVisible(opts ListOptions) ([]*Product, error)
	Create(p *Product) error
	// CountByOrgSince counts an org's publications in a window (rate limit).
	CountByOrgSince(orgID string, since time.Time) (int, error)
	// CountVisible is the pool ceiling check.
	CountVisible() (int, error)
	// AddReport records one person's report and returns how many distinct
	// people have now reported the entry.
	AddReport(id, userID string) (int, error)
	// AddVote records one person's vote for a visible entry and returns how
	// many distinct people have now voted for it. Voting twice is a no-op.
	// A hidden or missing entry is ErrNotFound.
	AddVote(id, userID string) (int, error)
	// RemoveVote withdraws one person's vote and returns the new total.
	// Withdrawing a vote that was never cast is a no-op.
	RemoveVote(id, userID string) (int, error)
	// CountVotesWeek counts votes cast for an entry inside the rolling
	// VoteWindowDays window, as the database reckons "now".
	CountVotesWeek(id string) (int, error)
	// SetHidden hides or unhides an entry.
	SetHidden(id string, hidden bool) error
	Delete(id string) error
}

// Service is the shared-pool use case surface.
type Service interface {
	List(opts ListOptions) ([]*Product, error)
	// Publish stores a product on behalf of a signed-in user in a workspace.
	Publish(in Product, orgID, userID string) (*Product, error)
	// Report flags an entry on one person's behalf; it auto-hides once
	// ReportsToHide distinct people have flagged it.
	Report(id, userID string) error
	// Vote records one person's vote for an entry. It is idempotent: voting
	// again returns the same counts rather than inflating them.
	Vote(id, userID string) (VoteCounts, error)
	// Unvote withdraws one person's vote, and is likewise idempotent.
	Unvote(id, userID string) (VoteCounts, error)
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

// List returns the visible pool in the requested order — newest first by
// default, or most-voted first for the leaderboards. An unknown sort is
// refused (ErrBadSort) rather than quietly answered with the default.
func (s *DefaultService) List(opts ListOptions) ([]*Product, error) {
	if opts.Limit <= 0 {
		opts.Limit = DefaultListLimit
	}
	if opts.Limit > MaxListLimit {
		opts.Limit = MaxListLimit
	}
	switch opts.Sort {
	case "":
		opts.Sort = SortRecent
	case SortRecent, SortTop, SortTopWeek:
	default:
		return nil, ErrBadSort
	}
	return s.repo.ListVisible(opts)
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

// Vote records one person's vote for an entry.
//
// Votes are per person, like reports: the repository dedupes on
// (product, user), so pressing the button twice settles on the same count
// and nobody can lift their own invention up the leaderboard alone. A hidden
// entry is not votable — it is not listed either, and the repository reports
// it as ErrNotFound rather than letting votes accrue to something nobody can
// see.
func (s *DefaultService) Vote(id, userID string) (VoteCounts, error) {
	if strings.TrimSpace(userID) == "" {
		return VoteCounts{}, ErrNotVotable
	}
	total, err := s.repo.AddVote(id, userID)
	if err != nil {
		return VoteCounts{}, err
	}
	return s.voteCounts(id, total, true)
}

// Unvote withdraws one person's vote. Withdrawing a vote that was never cast
// is a no-op that reports the entry's current counts.
func (s *DefaultService) Unvote(id, userID string) (VoteCounts, error) {
	if strings.TrimSpace(userID) == "" {
		return VoteCounts{}, ErrNotVotable
	}
	total, err := s.repo.RemoveVote(id, userID)
	if err != nil {
		return VoteCounts{}, err
	}
	return s.voteCounts(id, total, false)
}

// voteCounts pairs an all-time total with the weekly count the database
// computes over its own clock, so both numbers a client renders come from
// the same place the leaderboard is ordered by.
func (s *DefaultService) voteCounts(id string, total int, voted bool) (VoteCounts, error) {
	week, err := s.repo.CountVotesWeek(id)
	if err != nil {
		return VoteCounts{}, err
	}
	return VoteCounts{Votes: total, VotesWeek: week, Voted: voted}, nil
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
