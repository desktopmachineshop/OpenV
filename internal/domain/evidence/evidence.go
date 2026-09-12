// Package evidence records what a physical or manual test actually produced.
//
// An automated test case verifies itself: the run is the evidence. A hardware
// test does not. Somebody goes to a rig, runs it, and comes back with files —
// a capture, a log, a photograph of the setup, a spreadsheet of readings — and
// those files are the only reason anyone should believe the result.
//
// The unit here is the CAPTURE SESSION, not the outcome. One long run on a rig
// commonly answers several test cases at once: a single noise recording taken
// across a sweep of conditions is the evidence for every condition it covers.
// So a bundle is an entity of its own, owned by the project rather than by any
// run, and results CITE it. That is the whole reason this is not simply a
// longer list of files hanging off a test result:
//
//   - one capture, many results — including results in different runs, when a
//     campaign is repeated against a new baseline and the same rig data still
//     stands;
//   - the capture outlives the result. Re-recording an outcome must not
//     disturb the evidence behind it.
//
// A bundle with no files is valid and deliberate. Sometimes the evidence is a
// written account of what was observed — an inspection, a demonstration
// somebody witnessed — and forcing a file upload would only produce a
// screenshot of a sentence.
package evidence

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RefPrefix is the citable prefix for a bundle reference ("EVD-1"). Bundles
// are cited in reports and conversations the way artifacts and figures are, so
// they are numbered the same way, from the same per-project counter table.
const RefPrefix = "EVD"

// Field bounds. Generous, because a rig description is genuinely long, but
// bounded, because these are rendered into reports and exports.
const (
	MaxTitleLen      = 200
	MaxSummaryLen    = 20000
	MaxCapturedByLen = 200
	MaxNoteLen       = 2000
	// MaxConditionKeys bounds the free-form conditions map so one bundle
	// cannot carry an unbounded document in a jsonb column.
	MaxConditionKeys = 50
	// MaxConditionValueLen bounds one condition's rendered value.
	MaxConditionValueLen = 1000
)

var (
	ErrNotFound      = errors.New("evidence bundle not found")
	ErrFileNotFound  = errors.New("evidence file not found")
	ErrInvalid       = errors.New("invalid evidence bundle")
	ErrQuotaExceeded = errors.New("the workspace's evidence storage limit is full")
	// ErrAlreadyCited is not a failure: a result citing a bundle it already
	// cites is the state the caller asked for. Handlers answer it as success.
	ErrAlreadyCited = errors.New("this result already cites that evidence")
)

// Bundle is one capture session: what was done, when, by whom, under what
// conditions, and the files it produced.
type Bundle struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	// Ref is the citable reference, "EVD-1", unique within the project and
	// never reissued.
	Ref     string `json:"ref"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	// CapturedAt is when the test was actually carried out, which is not when
	// somebody got round to uploading it. Nil when it was not recorded.
	CapturedAt *time.Time `json:"captured_at,omitempty"`
	// CapturedBy is free text on purpose. The person on the rig is often not
	// an OpenV user — a lab, a contractor, a colleague from another team —
	// and a user id would either exclude them or misattribute the work.
	CapturedBy string `json:"captured_by"`
	// Conditions is open-ended metadata: rig, serial numbers, calibration
	// date, ambient temperature. Free-form because this vocabulary differs
	// per product and per discipline, and pinning it to columns would mean a
	// migration every time somebody measures something new.
	Conditions map[string]interface{} `json:"conditions"`
	CreatedBy  *string                `json:"created_by,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`

	// Files and Citations are populated by Get, not by List: a project's
	// bundle list would otherwise carry every file row in the project.
	Files     []*File     `json:"files,omitempty"`
	Citations []*Citation `json:"citations,omitempty"`
	// FileCount and TotalSize are the list-view summary of Files.
	FileCount int   `json:"file_count"`
	TotalSize int64 `json:"total_size"`
}

// File is one uploaded artefact of a capture session.
//
// Deliberately not an attachments.Attachment: an attachment is a numbered
// FIGURE on an artifact, it must have an artifact to hang off, and its upload
// path accepts images only. None of that fits a dataset.
type File struct {
	ID       string `json:"id"`
	BundleID string `json:"bundle_id"`
	// Filename is the name the uploader's file had. Unlike a figure, an
	// evidence file keeps its own name: "sweep-20260912-run3.csv" is
	// information about the capture, and renaming it to a reference would
	// throw that away.
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	FilePath string `json:"-"`
	FileSize int64  `json:"file_size"`
	// SHA256 is recorded at upload so the file can be checked against the
	// record later. Evidence nobody can verify the integrity of is a weaker
	// claim than evidence they can.
	SHA256     string    `json:"sha256"`
	UploadedBy *string   `json:"uploaded_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Citation is one result's claim on one bundle: "this outcome rests on that
// capture". The many-to-many is the point — the same bundle is cited by every
// result it supports.
type Citation struct {
	ID       string `json:"id"`
	BundleID string `json:"bundle_id"`
	// TestResultID targets the RESULT rather than the test case. A capture
	// supports a particular recorded outcome; the same case verified again
	// later is a different claim, which may rest on a different capture.
	TestResultID string    `json:"test_result_id"`
	Note         string    `json:"note"`
	CreatedAt    time.Time `json:"created_at"`

	// Denormalized for display, so a bundle can say which results cite it
	// without the caller resolving four more ids.
	BundleRef     string `json:"bundle_ref,omitempty"`
	BundleTitle   string `json:"bundle_title,omitempty"`
	TestCaseID    string `json:"test_case_id,omitempty"`
	TestCaseTitle string `json:"test_case_title,omitempty"`
	TestCaseRef   string `json:"test_case_ref,omitempty"`
	RunID         string `json:"run_id,omitempty"`
	RunName       string `json:"run_name,omitempty"`
}

// CreateRequest is the payload for a new bundle.
type CreateRequest struct {
	Title      string                 `json:"title"`
	Summary    string                 `json:"summary"`
	CapturedAt *time.Time             `json:"captured_at,omitempty"`
	CapturedBy string                 `json:"captured_by"`
	Conditions map[string]interface{} `json:"conditions"`
}

// UpdateRequest is the payload for editing a bundle. Every field is replaced,
// so a caller sends the whole record back.
type UpdateRequest struct {
	Title      string                 `json:"title"`
	Summary    string                 `json:"summary"`
	CapturedAt *time.Time             `json:"captured_at,omitempty"`
	CapturedBy string                 `json:"captured_by"`
	Conditions map[string]interface{} `json:"conditions"`
}

// Repository is persistence for bundles, their files and their citations.
// Find methods return (nil, nil) when no row matches.
type Repository interface {
	// Create assigns the bundle's Ref from the project's counter and stores
	// it.
	Create(b *Bundle) error
	FindByID(id string) (*Bundle, error)
	// ListByProject returns the project's bundles, newest capture first,
	// each carrying FileCount and TotalSize but not Files.
	ListByProject(projectID string) ([]*Bundle, error)
	Update(b *Bundle) error
	// Delete removes the bundle, its files' rows and its citations. The
	// caller is responsible for removing the files from disk, because only
	// it knows whether the database transaction committed.
	Delete(id string) ([]*File, error)

	AddFile(f *File) error
	FindFileByID(id string) (*File, error)
	DeleteFile(id string) (*File, error)
	ListFiles(bundleID string) ([]*File, error)

	AddCitation(c *Citation) error
	RemoveCitation(bundleID, testResultID string) error
	// ListCitationsForBundle returns the results citing a bundle.
	ListCitationsForBundle(bundleID string) ([]*Citation, error)
	// ListCitationsForRun returns every citation against a run's results,
	// keyed by test result id, so the run view can render them in one query.
	ListCitationsForRun(runID string) (map[string][]*Citation, error)
	// ListCitationsForResult returns the bundles one result cites.
	ListCitationsForResult(testResultID string) ([]*Citation, error)

	// StorageUsedByOrg totals every evidence file in the workspace, which is
	// what the storage quota is measured against.
	StorageUsedByOrg(orgID string) (int64, error)
	// ProjectOrg resolves the workspace a project belongs to.
	ProjectOrg(projectID string) (string, error)
	// ProjectForResult resolves the project a recorded test result sits in,
	// which is what an authorization check on a citation needs. Returns
	// ("", nil) when no such result exists.
	ProjectForResult(testResultID string) (string, error)
}

// Service is the evidence domain logic.
type Service interface {
	Create(projectID string, req CreateRequest, createdBy *string) (*Bundle, error)
	Get(id string) (*Bundle, error)
	List(projectID string) ([]*Bundle, error)
	Update(id string, req UpdateRequest) (*Bundle, error)
	Delete(id string) ([]*File, error)

	// StorageUsedByOrg totals the workspace's evidence, so a limits view can
	// show usage beside the ceiling rather than only refusing at it.
	StorageUsedByOrg(orgID string) (int64, error)
	// CheckQuota reports whether the project's workspace can accept another
	// incoming bytes of evidence, returning ErrQuotaExceeded when it cannot.
	// limitBytes <= 0 means unlimited.
	CheckQuota(projectID string, incoming, limitBytes int64) error
	AddFile(bundleID string, f *File) error
	GetFile(id string) (*File, error)
	DeleteFile(id string) (*File, error)

	// ProjectForResult resolves a result's project, for authorization.
	ProjectForResult(testResultID string) (string, error)
	Cite(testResultID, bundleID, note string) (*Citation, error)
	Uncite(testResultID, bundleID string) error
	CitationsForRun(runID string) (map[string][]*Citation, error)
	CitationsForResult(testResultID string) ([]*Citation, error)
}

// Validate checks a bundle's user-supplied fields and normalizes whitespace.
// It is the one door every write goes through, so a bundle created by the API,
// by an import, or by a test cannot differ in what it is allowed to hold.
func Validate(title, summary, capturedBy string, conditions map[string]interface{}) (string, string, string, map[string]interface{}, error) {
	title = strings.TrimSpace(title)
	summary = strings.TrimSpace(summary)
	capturedBy = strings.TrimSpace(capturedBy)

	if title == "" {
		return "", "", "", nil, fmt.Errorf("%w: a title is required", ErrInvalid)
	}
	if len(title) > MaxTitleLen {
		return "", "", "", nil, fmt.Errorf("%w: the title is longer than %d characters", ErrInvalid, MaxTitleLen)
	}
	if len(summary) > MaxSummaryLen {
		return "", "", "", nil, fmt.Errorf("%w: the summary is longer than %d characters", ErrInvalid, MaxSummaryLen)
	}
	if len(capturedBy) > MaxCapturedByLen {
		return "", "", "", nil, fmt.Errorf("%w: the captured-by name is longer than %d characters", ErrInvalid, MaxCapturedByLen)
	}

	clean := map[string]interface{}{}
	if len(conditions) > MaxConditionKeys {
		return "", "", "", nil, fmt.Errorf("%w: more than %d conditions", ErrInvalid, MaxConditionKeys)
	}
	for k, v := range conditions {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		// Conditions are rendered as text wherever they are shown, so a
		// value is bounded by its rendered length whatever its JSON type.
		if s, ok := v.(string); ok {
			s = strings.TrimSpace(s)
			if len(s) > MaxConditionValueLen {
				return "", "", "", nil, fmt.Errorf("%w: the value of condition %q is longer than %d characters",
					ErrInvalid, k, MaxConditionValueLen)
			}
			v = s
		}
		clean[k] = v
	}
	return title, summary, capturedBy, clean, nil
}

// DefaultService implements Service.
type DefaultService struct {
	repo Repository
}

// NewDefaultService creates the evidence service.
func NewDefaultService(repo Repository) *DefaultService {
	return &DefaultService{repo: repo}
}

// Create validates and stores a new bundle. The Ref is assigned by the
// repository, which owns the project's counter.
func (s *DefaultService) Create(projectID string, req CreateRequest, createdBy *string) (*Bundle, error) {
	title, summary, capturedBy, conditions, err := Validate(req.Title, req.Summary, req.CapturedBy, req.Conditions)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	b := &Bundle{
		ID:         uuid.New().String(),
		ProjectID:  projectID,
		Title:      title,
		Summary:    summary,
		CapturedAt: req.CapturedAt,
		CapturedBy: capturedBy,
		Conditions: conditions,
		CreatedBy:  createdBy,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.repo.Create(b); err != nil {
		return nil, err
	}
	return b, nil
}

// Get returns one bundle with its files and the results citing it.
func (s *DefaultService) Get(id string) (*Bundle, error) {
	b, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, ErrNotFound
	}
	if b.Files, err = s.repo.ListFiles(id); err != nil {
		return nil, err
	}
	if b.Citations, err = s.repo.ListCitationsForBundle(id); err != nil {
		return nil, err
	}
	b.FileCount = len(b.Files)
	b.TotalSize = 0
	for _, f := range b.Files {
		b.TotalSize += f.FileSize
	}
	return b, nil
}

// List returns the project's bundles, newest capture first.
func (s *DefaultService) List(projectID string) ([]*Bundle, error) {
	return s.repo.ListByProject(projectID)
}

// Update replaces a bundle's editable fields.
func (s *DefaultService) Update(id string, req UpdateRequest) (*Bundle, error) {
	b, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, ErrNotFound
	}
	title, summary, capturedBy, conditions, err := Validate(req.Title, req.Summary, req.CapturedBy, req.Conditions)
	if err != nil {
		return nil, err
	}
	b.Title, b.Summary, b.CapturedBy, b.Conditions = title, summary, capturedBy, conditions
	b.CapturedAt = req.CapturedAt
	b.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(b); err != nil {
		return nil, err
	}
	return b, nil
}

// Delete removes a bundle and returns the files whose bytes the caller must
// now remove from disk.
func (s *DefaultService) Delete(id string) ([]*File, error) {
	b, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, ErrNotFound
	}
	return s.repo.Delete(id)
}

// StorageUsedByOrg totals the workspace's evidence files.
func (s *DefaultService) StorageUsedByOrg(orgID string) (int64, error) {
	return s.repo.StorageUsedByOrg(orgID)
}

// CheckQuota refuses an upload that would take the workspace over its evidence
// storage limit. The limit is measured across the whole workspace rather than
// per project, because the disk it protects is one shared volume.
func (s *DefaultService) CheckQuota(projectID string, incoming, limitBytes int64) error {
	if limitBytes <= 0 {
		return nil
	}
	orgID, err := s.repo.ProjectOrg(projectID)
	if err != nil {
		return err
	}
	used, err := s.repo.StorageUsedByOrg(orgID)
	if err != nil {
		return err
	}
	if used+incoming > limitBytes {
		return fmt.Errorf("%w: %s of %s used", ErrQuotaExceeded,
			HumanBytes(used), HumanBytes(limitBytes))
	}
	return nil
}

// AddFile records an uploaded file against a bundle.
func (s *DefaultService) AddFile(bundleID string, f *File) error {
	b, err := s.repo.FindByID(bundleID)
	if err != nil {
		return err
	}
	if b == nil {
		return ErrNotFound
	}
	f.BundleID = bundleID
	if f.ID == "" {
		f.ID = uuid.New().String()
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now().UTC()
	}
	return s.repo.AddFile(f)
}

// GetFile returns one file's record.
func (s *DefaultService) GetFile(id string) (*File, error) {
	f, err := s.repo.FindFileByID(id)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, ErrFileNotFound
	}
	return f, nil
}

// DeleteFile removes a file's row and returns it so the caller can remove the
// bytes.
func (s *DefaultService) DeleteFile(id string) (*File, error) {
	f, err := s.repo.DeleteFile(id)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, ErrFileNotFound
	}
	return f, nil
}

// ProjectForResult resolves a result's project, so a caller can be checked
// against it before citing or reading evidence.
func (s *DefaultService) ProjectForResult(testResultID string) (string, error) {
	return s.repo.ProjectForResult(testResultID)
}

// Cite records that a result rests on a bundle. Citing twice is not an error:
// the caller asked for a state that already holds.
func (s *DefaultService) Cite(testResultID, bundleID, note string) (*Citation, error) {
	b, err := s.repo.FindByID(bundleID)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, ErrNotFound
	}
	note = strings.TrimSpace(note)
	if len(note) > MaxNoteLen {
		return nil, fmt.Errorf("%w: the note is longer than %d characters", ErrInvalid, MaxNoteLen)
	}
	c := &Citation{
		ID:           uuid.New().String(),
		BundleID:     bundleID,
		TestResultID: testResultID,
		Note:         note,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.repo.AddCitation(c); err != nil {
		if errors.Is(err, ErrAlreadyCited) {
			return c, nil
		}
		return nil, err
	}
	return c, nil
}

// Uncite drops a result's claim on a bundle. The bundle itself is untouched —
// removing a citation is not deleting evidence.
func (s *DefaultService) Uncite(testResultID, bundleID string) error {
	return s.repo.RemoveCitation(bundleID, testResultID)
}

// CitationsForRun returns every citation in a run keyed by test result id.
func (s *DefaultService) CitationsForRun(runID string) (map[string][]*Citation, error) {
	return s.repo.ListCitationsForRun(runID)
}

// CitationsForResult returns the bundles one result cites.
func (s *DefaultService) CitationsForResult(testResultID string) ([]*Citation, error) {
	return s.repo.ListCitationsForResult(testResultID)
}

// HumanBytes renders a byte count for a message a person reads, which is the
// only place it is used — quota refusals and storage summaries.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 3; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
