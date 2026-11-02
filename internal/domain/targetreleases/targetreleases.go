// Package targetreleases implements a project's target releases: the named,
// ordered milestones ("MVP", "V1.0", "V2.0") its requirements are planned
// for, and the progress of each one against its requirements' review status
// and verification evidence.
//
// They are not OpenV's own releases (internal/domain/release, the release
// notes and channels of the platform): a target release belongs to one
// project and means what that project's team says it means.
//
// A requirement targets a range of releases rather than one: it is
// introduced in a release (the attribute AttrFrom) and stays in every later
// one, in position order, until the release named by AttrUntil, when it has
// one (a capability retired after that release). An artifact with neither
// attribute inherits its nearest ancestor's range, so targeting a heading
// targets everything under it; a child that sets its own range overrides it.
// Both attributes hold release ids, so renaming a release moves nothing.
package targetreleases

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The attribute keys an artifact's targeting is stored under. They ride the
// artifact's attributes, so a baseline, an export and the version history
// carry them with everything else the artifact says.
const (
	AttrFrom  = "target_release"
	AttrUntil = "target_release_until"
)

// Release statuses.
const (
	StatusPlanned    = "planned"
	StatusInProgress = "in_progress"
	StatusReleased   = "released"
)

// Errors.
var (
	ErrNotFound      = errors.New("release not found")
	ErrNameRequired  = errors.New("release name is required")
	ErrNameTaken     = errors.New("a release with this name already exists in the project")
	ErrInvalidStatus = errors.New("status must be one of: planned, in_progress, released")
	ErrInvalidDate   = errors.New("target_date must be a date written YYYY-MM-DD")
	ErrInUse         = errors.New("artifacts still target this release; move them to another release first")
	ErrReorder       = errors.New("the order must name every release of the project exactly once")
	ErrUnknownTarget = errors.New("the target release is not a release of this project")
	ErrRangeOrder    = errors.New("the last release must not come before the first release")
	ErrUntilWithout  = errors.New("a last release needs a first release")
)

var validStatuses = map[string]bool{StatusPlanned: true, StatusInProgress: true, StatusReleased: true}

// ValidStatus reports whether s is a release status.
func ValidStatus(s string) bool { return validStatuses[s] }

// Statuses lists the release statuses in lifecycle order.
func Statuses() []string { return []string{StatusPlanned, StatusInProgress, StatusReleased} }

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Release is one target release of a project.
type Release struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// TargetDate is the planned date, YYYY-MM-DD, or nil when none is set.
	TargetDate *string `json:"target_date"`
	Status     string  `json:"status"`
	// Position orders the project's releases: a requirement introduced in a
	// release is in every release with a higher position too.
	Position int `json:"position"`
	// ReleasedAt is when the status last became released.
	ReleasedAt *time.Time `json:"released_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// CreateRequest is the payload for a new release. A new release goes last
// in the order.
type CreateRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	TargetDate  *string `json:"target_date"`
	Status      string  `json:"status"`
}

// UpdateRequest changes a release: an omitted (nil) field is left as it is.
// An empty TargetDate clears the date.
type UpdateRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	TargetDate  *string `json:"target_date"`
	Status      *string `json:"status"`
}

// Repository stores releases.
type Repository interface {
	Create(r *Release) error
	Update(r *Release) error
	Delete(id string) error
	// Get returns ErrNotFound when no release has the id.
	Get(id string) (*Release, error)
	// ListByProject returns the project's releases in position order.
	ListByProject(projectID string) ([]*Release, error)
	// SetPositions gives each release id its index in ids as its position.
	SetPositions(projectID string, ids []string) error
}

// TargetCounter counts a project's current artifacts that name a release in
// either targeting attribute. cmd/server wires it to the artifact store.
type TargetCounter func(projectID, releaseID string) (int, error)

// Service is the release domain logic.
type Service interface {
	List(projectID string) ([]*Release, error)
	Get(id string) (*Release, error)
	Create(projectID string, req CreateRequest) (*Release, error)
	Update(id string, req UpdateRequest) (*Release, error)
	// Delete refuses a release an artifact still targets with ErrInUse.
	Delete(id string) error
	// Reorder sets the project's release order; ids must name each of its
	// releases once.
	Reorder(projectID string, ids []string) ([]*Release, error)
}

// DefaultService implements Service.
type DefaultService struct {
	repo    Repository
	targets TargetCounter
	now     func() time.Time
}

// NewDefaultService builds the service. targets may be nil, in which case
// Delete does not check for targeting artifacts.
func NewDefaultService(repo Repository, targets TargetCounter) *DefaultService {
	return &DefaultService{repo: repo, targets: targets, now: func() time.Time { return time.Now().UTC() }}
}

// List returns the project's releases in order.
func (s *DefaultService) List(projectID string) ([]*Release, error) {
	list, err := s.repo.ListByProject(projectID)
	if list == nil && err == nil {
		list = []*Release{}
	}
	return list, err
}

// Get returns one release.
func (s *DefaultService) Get(id string) (*Release, error) { return s.repo.Get(id) }

func cleanDate(d *string) (*string, error) {
	if d == nil {
		return nil, nil
	}
	v := strings.TrimSpace(*d)
	if v == "" {
		return nil, nil
	}
	if !datePattern.MatchString(v) {
		return nil, ErrInvalidDate
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return nil, ErrInvalidDate
	}
	return &v, nil
}

func (s *DefaultService) nameTaken(projectID, name, exceptID string) (bool, error) {
	list, err := s.repo.ListByProject(projectID)
	if err != nil {
		return false, err
	}
	for _, r := range list {
		if r.ID != exceptID && strings.EqualFold(r.Name, name) {
			return true, nil
		}
	}
	return false, nil
}

// Create adds a release at the end of the project's order.
func (s *DefaultService) Create(projectID string, req CreateRequest) (*Release, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrNameRequired
	}
	status := req.Status
	if status == "" {
		status = StatusPlanned
	}
	if !ValidStatus(status) {
		return nil, ErrInvalidStatus
	}
	date, err := cleanDate(req.TargetDate)
	if err != nil {
		return nil, err
	}
	list, err := s.repo.ListByProject(projectID)
	if err != nil {
		return nil, err
	}
	position := 0
	for _, r := range list {
		if strings.EqualFold(r.Name, name) {
			return nil, ErrNameTaken
		}
		if r.Position >= position {
			position = r.Position + 1
		}
	}
	now := s.now()
	rel := &Release{
		ID:          uuid.New().String(),
		ProjectID:   projectID,
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		TargetDate:  date,
		Status:      status,
		Position:    position,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if status == StatusReleased {
		rel.ReleasedAt = &now
	}
	if err := s.repo.Create(rel); err != nil {
		return nil, err
	}
	return rel, nil
}

// Update changes the fields the request carries.
func (s *DefaultService) Update(id string, req UpdateRequest) (*Release, error) {
	rel, err := s.repo.Get(id)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, ErrNameRequired
		}
		taken, err := s.nameTaken(rel.ProjectID, name, rel.ID)
		if err != nil {
			return nil, err
		}
		if taken {
			return nil, ErrNameTaken
		}
		rel.Name = name
	}
	if req.Description != nil {
		rel.Description = strings.TrimSpace(*req.Description)
	}
	if req.TargetDate != nil {
		date, err := cleanDate(req.TargetDate)
		if err != nil {
			return nil, err
		}
		rel.TargetDate = date
	}
	now := s.now()
	if req.Status != nil {
		if !ValidStatus(*req.Status) {
			return nil, ErrInvalidStatus
		}
		if *req.Status == StatusReleased && rel.Status != StatusReleased {
			rel.ReleasedAt = &now
		}
		if *req.Status != StatusReleased {
			rel.ReleasedAt = nil
		}
		rel.Status = *req.Status
	}
	rel.UpdatedAt = now
	if err := s.repo.Update(rel); err != nil {
		return nil, err
	}
	return rel, nil
}

// Delete removes a release no artifact targets.
func (s *DefaultService) Delete(id string) error {
	rel, err := s.repo.Get(id)
	if err != nil {
		return err
	}
	if s.targets != nil {
		n, err := s.targets(rel.ProjectID, rel.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrInUse
		}
	}
	return s.repo.Delete(id)
}

// Reorder sets the project's release order.
func (s *DefaultService) Reorder(projectID string, ids []string) ([]*Release, error) {
	list, err := s.repo.ListByProject(projectID)
	if err != nil {
		return nil, err
	}
	if len(ids) != len(list) {
		return nil, ErrReorder
	}
	known := map[string]bool{}
	for _, r := range list {
		known[r.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !known[id] || seen[id] {
			return nil, ErrReorder
		}
		seen[id] = true
	}
	if err := s.repo.SetPositions(projectID, ids); err != nil {
		return nil, err
	}
	return s.List(projectID)
}

// Order indexes a project's releases by id for range checks: the index of
// each release in position order.
type Order map[string]int

// NewOrder indexes releases, which need not be sorted.
func NewOrder(releases []*Release) Order {
	sorted := append([]*Release(nil), releases...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Position < sorted[j].Position })
	o := Order{}
	for i, r := range sorted {
		o[r.ID] = i
	}
	return o
}

// attr reads a string attribute, "" when absent or not a string.
func attr(attrs map[string]interface{}, key string) string {
	if attrs == nil {
		return ""
	}
	if v, ok := attrs[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// ValidateTargets checks the targeting attributes of an artifact about to
// be written: each must name a release of the project (o), and the last
// release must not come before the first. Absent or empty attributes pass.
func ValidateTargets(attrs map[string]interface{}, o Order) error {
	from, until := attr(attrs, AttrFrom), attr(attrs, AttrUntil)
	if from == "" && until == "" {
		return nil
	}
	if from == "" {
		return ErrUntilWithout
	}
	fi, ok := o[from]
	if !ok {
		return ErrUnknownTarget
	}
	if until == "" {
		return nil
	}
	ui, ok := o[until]
	if !ok {
		return ErrUnknownTarget
	}
	if ui < fi {
		return ErrRangeOrder
	}
	return nil
}
