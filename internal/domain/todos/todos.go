// Package todos implements to-dos: one paragraph of text, small enough to
// just do and tick off (issue #731).
//
// A to-do is not a board task. It has no column, no discussion thread, no
// linked artifacts to collaborate on and no agent runs; those belong to a
// work item (internal/domain/workitems). A to-do belongs to a project, where
// every project member sees it, or to nobody but the person who wrote it (a
// personal to-do, with no project). It may remember the artifact or the note
// it was raised from, as a back-reference only.
//
// When a to-do turns out to be bigger, Promote makes a board task of it: a
// work item carrying the text, raised from the same note, and the to-do is
// ticked off with the note that it was promoted, pointing at the new item.
package todos

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/workitems"
)

// MaxTextLength is the most characters a to-do's text may hold: a paragraph.
const MaxTextLength = 1000

// DoneNotePromoted is the done note of a to-do that became a board task.
const DoneNotePromoted = "promoted"

// Errors.
var (
	ErrNotFound     = errors.New("to-do not found")
	ErrTextRequired = errors.New("a to-do needs some text")
	ErrTextTooLong  = errors.New("a to-do is one paragraph: at most 1000 characters")
	ErrParagraphs   = errors.New("a to-do is one paragraph: put a longer piece of work on the board instead")
	ErrNoProject    = errors.New("a personal to-do has no board to go on: only a project's to-do can be promoted")
	ErrPromoted     = errors.New("this to-do is already a board task")
	ErrNoWorkspace  = errors.New("a to-do needs a workspace")
)

// Todo is one to-do.
type Todo struct {
	ID    string `json:"id"`
	OrgID string `json:"org_id"`
	// ProjectID is nil for a personal to-do, which only its writer sees.
	ProjectID *string    `json:"project_id"`
	Text      string     `json:"text"`
	Done      bool       `json:"done"`
	DoneAt    *time.Time `json:"done_at"`
	// DoneNote says why a to-do was ticked off when it was not simply done:
	// DoneNotePromoted for one that became a board task.
	DoneNote   string     `json:"done_note"`
	AssigneeID *string    `json:"assignee_id"`
	DueDate    *time.Time `json:"due_date"`
	// SourceArtifactID and SourceChatterID are where the to-do was raised:
	// back-references, so a note can show the to-do's status beside it.
	SourceArtifactID *string `json:"source_artifact_id"`
	SourceChatterID  *string `json:"source_chatter_id"`
	// PromotedWorkItemID is the board task the to-do became.
	PromotedWorkItemID *string   `json:"promoted_work_item_id"`
	CreatedBy          *string   `json:"created_by"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Status is the to-do's state in the words a note's chip shows.
func (t *Todo) Status() string {
	if t.Done {
		return "done"
	}
	return "open"
}

// CreateRequest is the payload for creating a to-do. OrgID, ProjectID and
// the creator are set by the caller from the route, not the body.
type CreateRequest struct {
	OrgID            string     `json:"-"`
	ProjectID        *string    `json:"-"`
	Text             string     `json:"text"`
	AssigneeID       *string    `json:"assignee_id,omitempty"`
	DueDate          *time.Time `json:"due_date,omitempty"`
	SourceArtifactID *string    `json:"source_artifact_id,omitempty"`
	SourceChatterID  *string    `json:"source_chatter_id,omitempty"`
}

// UpdateRequest replaces a to-do's editable fields: its text, who has it
// and when it is due. Ticking it off is SetDone.
type UpdateRequest struct {
	Text       string     `json:"text"`
	AssigneeID *string    `json:"assignee_id,omitempty"`
	DueDate    *time.Time `json:"due_date,omitempty"`
}

// DoneRequest ticks a to-do off, or opens it again.
type DoneRequest struct {
	Done bool `json:"done"`
}

// NormalizeText checks a to-do's text and returns it trimmed.
func NormalizeText(text string) (string, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return "", ErrTextRequired
	}
	if utf8.RuneCountInString(text) > MaxTextLength {
		return "", ErrTextTooLong
	}
	// A blank line starts a second paragraph.
	for _, line := range strings.Split(text, "\n")[1:] {
		if strings.TrimSpace(line) == "" {
			return "", ErrParagraphs
		}
	}
	return text, nil
}

// Repository defines persistence operations for to-dos.
type Repository interface {
	Save(t *Todo) error
	Update(t *Todo) error
	FindByID(id string) (*Todo, error)
	// ListByProject lists a project's to-dos: open ones first, soonest due
	// first, then the done ones, most recently done first.
	ListByProject(projectID string) ([]*Todo, error)
	// ListForUser lists, in one workspace, the to-dos assigned to a person
	// and their own personal ones, in ListByProject's order.
	ListForUser(orgID, userID string) ([]*Todo, error)
	ListBySourceChatterIDs(chatterIDs []string) ([]*Todo, error)
	Delete(id string) error
}

// BoardCreator makes the work item a promoted to-do becomes; the work item
// service is one.
type BoardCreator interface {
	Create(req workitems.CreateWorkItemRequest, createdBy *string, actor string) (*workitems.WorkItem, error)
}

// Service defines to-do domain logic.
type Service interface {
	Create(req CreateRequest, createdBy *string) (*Todo, error)
	Get(id string) (*Todo, error)
	ListByProject(projectID string) ([]*Todo, error)
	ListForUser(orgID, userID string) ([]*Todo, error)
	ListBySourceChatterIDs(chatterIDs []string) ([]*Todo, error)
	Update(id string, req UpdateRequest) (*Todo, error)
	SetDone(id string, done bool) (*Todo, error)
	Promote(id string, createdBy *string, actor string) (*Todo, *workitems.WorkItem, error)
	Delete(id string) error
}

// DefaultService implements Service.
type DefaultService struct {
	repo  Repository
	board BoardCreator
	now   func() time.Time
}

// NewDefaultService creates a to-do service. board may be nil, when
// promoting is refused.
func NewDefaultService(repo Repository, board BoardCreator) *DefaultService {
	return &DefaultService{repo: repo, board: board, now: func() time.Time { return time.Now().UTC() }}
}

// Create adds an open to-do. A personal to-do is always its writer's own.
func (s *DefaultService) Create(req CreateRequest, createdBy *string) (*Todo, error) {
	if req.OrgID == "" {
		return nil, ErrNoWorkspace
	}
	text, err := NormalizeText(req.Text)
	if err != nil {
		return nil, err
	}
	now := s.now()
	t := &Todo{
		ID:               uuid.New().String(),
		OrgID:            req.OrgID,
		ProjectID:        blankNil(req.ProjectID),
		Text:             text,
		AssigneeID:       blankNil(req.AssigneeID),
		DueDate:          req.DueDate,
		SourceArtifactID: blankNil(req.SourceArtifactID),
		SourceChatterID:  blankNil(req.SourceChatterID),
		CreatedBy:        createdBy,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if t.ProjectID == nil {
		t.AssigneeID = createdBy
	}
	if err := s.repo.Save(t); err != nil {
		return nil, err
	}
	return t, nil
}

// Get returns one to-do.
func (s *DefaultService) Get(id string) (*Todo, error) {
	return s.repo.FindByID(id)
}

// ListByProject lists a project's to-dos.
func (s *DefaultService) ListByProject(projectID string) ([]*Todo, error) {
	return s.repo.ListByProject(projectID)
}

// ListForUser lists a person's to-dos in a workspace.
func (s *DefaultService) ListForUser(orgID, userID string) ([]*Todo, error) {
	return s.repo.ListForUser(orgID, userID)
}

// ListBySourceChatterIDs returns the to-dos raised from the given notes.
func (s *DefaultService) ListBySourceChatterIDs(chatterIDs []string) ([]*Todo, error) {
	if len(chatterIDs) == 0 {
		return nil, nil
	}
	return s.repo.ListBySourceChatterIDs(chatterIDs)
}

// Update replaces a to-do's text, assignee and due date. A personal to-do
// stays its writer's.
func (s *DefaultService) Update(id string, req UpdateRequest) (*Todo, error) {
	t, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	text, err := NormalizeText(req.Text)
	if err != nil {
		return nil, err
	}
	t.Text = text
	if t.ProjectID != nil {
		t.AssigneeID = blankNil(req.AssigneeID)
	}
	t.DueDate = req.DueDate
	t.UpdatedAt = s.now()
	if err := s.repo.Update(t); err != nil {
		return nil, err
	}
	return t, nil
}

// SetDone ticks a to-do off or opens it again. Opening one again clears
// its done note; a promoted to-do keeps pointing at its board task.
func (s *DefaultService) SetDone(id string, done bool) (*Todo, error) {
	t, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if t.Done == done {
		return t, nil
	}
	now := s.now()
	t.Done = done
	if done {
		t.DoneAt = &now
	} else {
		t.DoneAt = nil
		t.DoneNote = ""
	}
	t.UpdatedAt = now
	if err := s.repo.Update(t); err != nil {
		return nil, err
	}
	return t, nil
}

// Promote makes a board task of a project's to-do: a work item in the To Do
// column carrying the text, assigned to the same person, due the same day,
// linked to the artifact and raised from the note the to-do came from. The
// to-do is then ticked off, noted as promoted and pointed at the item.
func (s *DefaultService) Promote(id string, createdBy *string, actor string) (*Todo, *workitems.WorkItem, error) {
	t, err := s.repo.FindByID(id)
	if err != nil {
		return nil, nil, err
	}
	if t.ProjectID == nil {
		return nil, nil, ErrNoProject
	}
	if t.PromotedWorkItemID != nil {
		return nil, nil, ErrPromoted
	}
	if s.board == nil {
		return nil, nil, errors.New("the board is not available")
	}
	req := workitems.CreateWorkItemRequest{
		ProjectID:    *t.ProjectID,
		Title:        PromotedTitle(t.Text),
		Description:  t.Text,
		Column:       workitems.ColumnTodo,
		AssigneeType: workitems.AssigneeUser,
		AssigneeID:   t.AssigneeID,
		ArtifactIDs:  []string{},
		DueDate:      t.DueDate,
	}
	if t.SourceArtifactID != nil {
		req.ArtifactIDs = []string{*t.SourceArtifactID}
	}
	item, err := s.board.Create(req, createdBy, actor)
	if err != nil {
		return nil, nil, err
	}
	now := s.now()
	t.Done = true
	t.DoneAt = &now
	t.DoneNote = DoneNotePromoted
	t.PromotedWorkItemID = &item.ID
	t.UpdatedAt = now
	if err := s.repo.Update(t); err != nil {
		return nil, item, err
	}
	return t, item, nil
}

// Delete removes a to-do.
func (s *DefaultService) Delete(id string) error {
	return s.repo.Delete(id)
}

// promotedTitleLimit is how much of a to-do becomes its board task's title.
const promotedTitleLimit = 120

// PromotedTitle is the title a promoted to-do's work item gets: the text,
// cut at a word before promotedTitleLimit characters.
func PromotedTitle(text string) string {
	line := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	runes := []rune(line)
	if len(runes) <= promotedTitleLimit {
		return line
	}
	cut := string(runes[:promotedTitleLimit])
	if i := strings.LastIndex(cut, " "); i > promotedTitleLimit/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ") + "…"
}

func blankNil(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	v := strings.TrimSpace(*s)
	return &v
}
