package postgres

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/agentruns"
	"github.com/openv/requirements-platform/internal/domain/events"
)

// Refactor plan X3a (docs/plans/codebase-refactor.md §6.7, quirk Q8,
// pain point persistence-v3): the event and agent-run repositories reset a
// limit <= 0 or above 500 to 100 rather than capping it at 500
// (event_repository.go and agent_run_repository.go, List). X3c's
// limitPolicy absorbs the rule wherever it sits, so it is pinned here, at
// the SQL LIMIT, with every value the API's parsers can hand down
// (internal/api/limit_parse_test.go): ListAgentRuns passes whatever
// strconv.Atoi answered, saturated overflow included; ListDomainEvents
// clamps first, so only 1 to 500 reach the event repository from there.
//
// Each table seeds limitClampRows rows, enough to tell 500 from 501 and
// both from 100, and a row's want is how many come back.

const limitClampRows = 502

// limitClampCase is one limit handed to a repository's List and the number
// of rows the SQL LIMIT let through.
type limitClampCase struct {
	limit int
	want  int
}

var limitClampCases = []limitClampCase{
	{math.MinInt, 100},
	{-1, 100},
	{0, 100},
	{1, 1},
	{5, 5},
	{99, 99},
	{100, 100}, // the default
	{101, 101},
	{499, 499},
	{500, 500}, // the maximum
	{501, 100}, // reset to the default, not capped at 500
	{1000, 100},
	{math.MaxInt, 100},
}

func TestLimitClampEventList(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewEventRepository(db)
	orgID := uuid.New().String()
	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < limitClampRows; i++ {
		e := events.New("artifact.updated", "", uuid.New().String(), "system", map[string]interface{}{"i": i})
		e.OrgID = orgID
		e.CreatedAt = base.Add(time.Duration(i) * time.Second)
		if err := repo.Save(e); err != nil {
			t.Fatalf("Save(%d): %v", i, err)
		}
	}
	for _, tc := range limitClampCases {
		got, err := repo.List(orgID, "", "", "", tc.limit)
		if err != nil {
			t.Fatalf("List(limit %d): %v", tc.limit, err)
		}
		if len(got) != tc.want {
			t.Errorf("List(limit %d) returned %d events, want %d", tc.limit, len(got), tc.want)
		}
	}
}

func TestLimitClampAgentRunList(t *testing.T) {
	f := newClaimFixture(t)
	for i := 0; i < limitClampRows; i++ {
		f.queueRun(t, runSpec{age: time.Duration(i) * time.Second})
	}
	for _, tc := range limitClampCases {
		got, err := f.repo.List(agentruns.ListFilter{OrgID: f.orgID, Limit: tc.limit})
		if err != nil {
			t.Fatalf("List(limit %d): %v", tc.limit, err)
		}
		if len(got) != tc.want {
			t.Errorf("List(limit %d) returned %d runs, want %d", tc.limit, len(got), tc.want)
		}
	}
}
