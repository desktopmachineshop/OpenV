// Package savedsearches holds a member's saved searches (issue #722): a named
// set of search-bar chips a page's search bar can put back with one click.
//
// A saved search belongs to one member in one workspace and one scope, the
// key a page's search bar stores them under ("requirements",
// "global-search"). The value is the bar's own state, opaque here: the
// frontend writes it and reads it back, so a page can change what its chips
// hold without a migration. A member can share one with the workspace; the
// others then see it listed, but only its owner can replace or delete it.
//
// The shape mirrors internal/domain/pushsubs: a Repository the postgres
// package implements and a Service façade the handlers depend on. Every
// read and write is scoped by the user and the workspace in the query
// itself.
package savedsearches

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Limits on what a member may store.
const (
	MaxNameLen  = 120
	MaxScopeLen = 64
	// MaxValueBytes bounds the stored chips. A bar holds a few dozen chips at
	// most; anything larger is not a search.
	MaxValueBytes = 16 * 1024
	// MaxPerScope is how many searches a member keeps in one scope.
	MaxPerScope = 100
)

// SavedSearch is one named search.
type SavedSearch struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	OrgID  string `json:"org_id"`
	Scope  string `json:"scope"`
	Name   string `json:"name"`
	// Value is the search bar's state as the page wrote it: a JSON object.
	Value json.RawMessage `json:"value"`
	// Shared lists it for every member of the workspace, not just its owner.
	Shared    bool      `json:"shared"`
	CreatedAt time.Time `json:"created_at"`
}

// Validation errors, each a message a client can show.
var (
	ErrScope   = errors.New("scope must be 1 to 64 letters, digits, '.', '_', ':' or '-'")
	ErrName    = errors.New("name is required and at most 120 characters")
	ErrValue   = errors.New("value must be a JSON object of at most 16 KB")
	ErrTooMany = errors.New("this scope already holds 100 saved searches; delete one first")
)

var scopePattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// ValidScope reports whether scope is a key a search may be stored under.
func ValidScope(scope string) bool { return scopePattern.MatchString(scope) }

// New builds a saved search with a fresh id and creation time, validating
// what the caller sent.
func New(userID, orgID, scope, name string, value json.RawMessage, shared bool) (*SavedSearch, error) {
	scope = strings.TrimSpace(scope)
	name = strings.TrimSpace(name)
	if !ValidScope(scope) {
		return nil, ErrScope
	}
	if name == "" || utf8.RuneCountInString(name) > MaxNameLen {
		return nil, ErrName
	}
	if len(value) == 0 || len(value) > MaxValueBytes || !isObject(value) {
		return nil, ErrValue
	}
	return &SavedSearch{
		ID:        uuid.New().String(),
		UserID:    userID,
		OrgID:     orgID,
		Scope:     scope,
		Name:      name,
		Value:     value,
		Shared:    shared,
		CreatedAt: time.Now().UTC(), // a TIMESTAMP holding a UTC wall clock (#379 bug 162)
	}, nil
}

func isObject(raw json.RawMessage) bool {
	var obj map[string]json.RawMessage
	return json.Unmarshal(raw, &obj) == nil && obj != nil
}

// Repository persists saved searches.
type Repository interface {
	// Upsert stores s, keyed on (user, workspace, scope, name): saving under
	// a name the member already used replaces that search's value and
	// sharing. s is filled in from the persisted row, so after a replace its
	// ID and CreatedAt are the existing row's.
	Upsert(s *SavedSearch) error
	// ListVisible returns the member's own searches in a scope and those
	// other members of the workspace shared, the member's own first, then
	// by name.
	ListVisible(userID, orgID, scope string) ([]*SavedSearch, error)
	// CountForUser counts the member's own searches in a scope.
	CountForUser(userID, orgID, scope string) (int, error)
	// DeleteForUser removes one of the member's own searches. Rows of other
	// members are untouched. Returns rows deleted.
	DeleteForUser(userID, orgID, id string) (int64, error)
}

// Service is the domain façade the handlers use.
type Service interface {
	List(userID, orgID, scope string) ([]*SavedSearch, error)
	// Save stores or replaces one of the member's searches (see Upsert).
	Save(s *SavedSearch) error
	Delete(userID, orgID, id string) (int64, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates the service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

func (s *DefaultService) List(userID, orgID, scope string) ([]*SavedSearch, error) {
	return s.repo.ListVisible(userID, orgID, scope)
}

// Save refuses a new name once the scope holds MaxPerScope of the member's
// searches; replacing one by its name is always allowed.
func (s *DefaultService) Save(search *SavedSearch) error {
	n, err := s.repo.CountForUser(search.UserID, search.OrgID, search.Scope)
	if err != nil {
		return err
	}
	if n >= MaxPerScope {
		own, err := s.repo.ListVisible(search.UserID, search.OrgID, search.Scope)
		if err != nil {
			return err
		}
		replacing := false
		for _, o := range own {
			if o.UserID == search.UserID && o.Name == search.Name {
				replacing = true
				break
			}
		}
		if !replacing {
			return ErrTooMany
		}
	}
	return s.repo.Upsert(search)
}

func (s *DefaultService) Delete(userID, orgID, id string) (int64, error) {
	return s.repo.DeleteForUser(userID, orgID, id)
}
