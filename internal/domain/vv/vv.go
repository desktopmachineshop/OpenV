package vv

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/chatter"
	"github.com/openv/requirements-platform/internal/domain/events"
)

// Test run status values.
const (
	RunStatusInProgress = "in-progress"
	RunStatusCompleted  = "completed"
	RunStatusAborted    = "aborted"
)

// Test result status values.
const (
	ResultPass    = "pass"
	ResultFail    = "fail"
	ResultBlocked = "blocked"
	ResultNotRun  = "not-run"
)

// Verification methods for requirements.
const (
	MethodInspection    = "inspection"
	MethodAnalysis      = "analysis"
	MethodDemonstration = "demonstration"
	MethodTest          = "test"
)

// Error definitions
var (
	ErrRunNotFound       = errors.New("test run not found")
	ErrResultNotFound    = errors.New("test result not found")
	ErrInvalidStatus     = errors.New("invalid status value")
	ErrInvalidTransition = errors.New("invalid run status transition")
	ErrNotTestCase       = errors.New("artifact is not a test case")
)

// TestRun represents a verification test execution campaign.
type TestRun struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"project_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	BaselineID  *string    `json:"baseline_id,omitempty"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedBy   *string    `json:"created_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// TestResult records the outcome of one test case within a run.
type TestResult struct {
	ID              string     `json:"id"`
	RunID           string     `json:"run_id"`
	TestCaseID      string     `json:"test_case_id"`
	TestCaseVersion int        `json:"test_case_version"`
	Status          string     `json:"status"`
	Notes           string     `json:"notes"`
	Evidence        []string   `json:"evidence"` // attachment IDs
	ExecutedAt      *time.Time `json:"executed_at,omitempty"`
	ExecutedBy      *string    `json:"executed_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// CreateRunRequest is the payload for creating a test run.
type CreateRunRequest struct {
	ProjectID   string  `json:"project_id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	BaselineID  *string `json:"baseline_id,omitempty"`
}

// UpsertResultRequest is the payload for recording a test result.
type UpsertResultRequest struct {
	TestCaseID string   `json:"test_case_id"`
	Status     string   `json:"status"`
	Notes      string   `json:"notes"`
	Evidence   []string `json:"evidence"`
}

// Repository defines persistence operations for V&V data.
type Repository interface {
	SaveRun(run *TestRun) error
	UpdateRun(run *TestRun) error
	FindRunByID(id string) (*TestRun, error)
	ListRunsByProject(projectID string) ([]*TestRun, error)
	DeleteRun(id string) error
	UpsertResult(r *TestResult) error
	ListResultsByRun(runID string) ([]*TestResult, error)
	LatestResultPerCase(projectID string) (map[string]*TestResult, error)
}

// Service defines V&V domain logic.
type Service interface {
	CreateRun(req CreateRunRequest, createdBy *string) (*TestRun, error)
	GetRun(id string) (*TestRun, error)
	ListRuns(projectID string) ([]*TestRun, error)
	UpdateRunStatus(id, status string) (*TestRun, error)
	DeleteRun(id string) error
	UpsertResult(runID string, req UpsertResultRequest, executedBy *string, actor string) (*TestResult, error)
	ListResults(runID string) ([]*TestResult, error)
	LatestResults(projectID string) (map[string]*TestResult, error)
}

// DefaultService implements the Service interface.
type DefaultService struct {
	repo            Repository
	artifactService artifacts.Service
	chatterService  chatter.Service
	bus             events.Bus
}

// NewDefaultService creates a new V&V service. bus may be nil.
func NewDefaultService(repo Repository, artifactService artifacts.Service, chatterService chatter.Service, bus events.Bus) *DefaultService {
	return &DefaultService{
		repo:            repo,
		artifactService: artifactService,
		chatterService:  chatterService,
		bus:             bus,
	}
}

// CreateRun creates a new in-progress test run.
func (s *DefaultService) CreateRun(req CreateRunRequest, createdBy *string) (*TestRun, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("run name is required")
	}
	if req.ProjectID == "" {
		return nil, errors.New("project id is required")
	}

	now := time.Now()
	run := &TestRun{
		ID:          uuid.New().String(),
		ProjectID:   req.ProjectID,
		Name:        req.Name,
		Description: req.Description,
		BaselineID:  req.BaselineID,
		Status:      RunStatusInProgress,
		StartedAt:   now,
		CreatedBy:   createdBy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.SaveRun(run); err != nil {
		return nil, err
	}

	return run, nil
}

// GetRun retrieves a test run by ID.
func (s *DefaultService) GetRun(id string) (*TestRun, error) {
	return s.repo.FindRunByID(id)
}

// ListRuns retrieves all test runs for a project.
func (s *DefaultService) ListRuns(projectID string) ([]*TestRun, error) {
	return s.repo.ListRunsByProject(projectID)
}

// UpdateRunStatus transitions a run's status. Only in-progress runs may be
// completed or aborted.
func (s *DefaultService) UpdateRunStatus(id, status string) (*TestRun, error) {
	if status != RunStatusCompleted && status != RunStatusAborted {
		return nil, ErrInvalidStatus
	}

	run, err := s.repo.FindRunByID(id)
	if err != nil {
		return nil, err
	}
	if run.Status != RunStatusInProgress {
		return nil, ErrInvalidTransition
	}

	now := time.Now()
	run.Status = status
	run.CompletedAt = &now
	run.UpdatedAt = now

	if err := s.repo.UpdateRun(run); err != nil {
		return nil, err
	}

	return run, nil
}

// DeleteRun removes a test run and its results.
func (s *DefaultService) DeleteRun(id string) error {
	return s.repo.DeleteRun(id)
}

// UpsertResult records or updates a test result within a run. The test case
// artifact must exist and be of type "test-case"; its current version is
// captured on the result. actor is the event actor ("" for system).
func (s *DefaultService) UpsertResult(runID string, req UpsertResultRequest, executedBy *string, actor string) (*TestResult, error) {
	switch req.Status {
	case ResultPass, ResultFail, ResultBlocked, ResultNotRun:
	default:
		return nil, ErrInvalidStatus
	}

	run, err := s.repo.FindRunByID(runID)
	if err != nil {
		return nil, err
	}

	testCase, err := s.artifactService.GetArtifact(req.TestCaseID)
	if err != nil {
		return nil, err
	}
	if testCase.Type != "test-case" {
		return nil, ErrNotTestCase
	}

	evidence := req.Evidence
	if evidence == nil {
		evidence = []string{}
	}

	now := time.Now()
	result := &TestResult{
		ID:              uuid.New().String(),
		RunID:           runID,
		TestCaseID:      req.TestCaseID,
		TestCaseVersion: testCase.Version,
		Status:          req.Status,
		Notes:           req.Notes,
		Evidence:        evidence,
		ExecutedAt:      &now,
		ExecutedBy:      executedBy,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.repo.UpsertResult(result); err != nil {
		return nil, err
	}

	// Auto-entry on the test case's chatter feed.
	message := fmt.Sprintf("Test result recorded: %s in run '%s'", strings.ToUpper(req.Status), run.Name)
	entry := chatter.NewChatterEntry(req.TestCaseID, message, true, "test-result")
	if err := s.chatterService.CreateEntry(entry); err != nil {
		// Non-fatal: the result itself was persisted.
		fmt.Printf("Warning: failed to create chatter entry for test result: %v\n", err)
	}

	if s.bus != nil {
		s.bus.Publish(events.New(events.TestRunRecorded, run.ProjectID, result.ID, actor, map[string]interface{}{
			"run_id":       runID,
			"test_case_id": req.TestCaseID,
			"status":       req.Status,
		}))
	}

	return result, nil
}

// ListResults retrieves all results recorded in a run.
func (s *DefaultService) ListResults(runID string) ([]*TestResult, error) {
	return s.repo.ListResultsByRun(runID)
}

// LatestResults returns the latest executed result per test case across the
// project's in-progress and completed runs.
func (s *DefaultService) LatestResults(projectID string) (map[string]*TestResult, error) {
	return s.repo.LatestResultPerCase(projectID)
}
