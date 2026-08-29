package postgres

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/events"
)

// TestEventListCursorPagination exercises the issue-#150 keyset cursor:
// pages ordered (created_at, id) DESC partition the stream with no
// duplicates and no gaps, even across identical timestamps, and an unknown
// cursor yields an empty page instead of an error.
func TestEventListCursorPagination(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewEventRepository(db)

	orgID := uuid.New().String()
	projectID := uuid.New().String()

	base := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	const total = 7
	for i := 0; i < total; i++ {
		e := events.New("artifact.updated", projectID, uuid.New().String(), "system", map[string]interface{}{"i": i})
		e.OrgID = orgID
		// Two colliding timestamps (i=2,3) force the id tiebreaker.
		if i == 3 {
			e.CreatedAt = base.Add(2 * time.Minute)
		} else {
			e.CreatedAt = base.Add(time.Duration(i) * time.Minute)
		}
		if err := repo.Save(e); err != nil {
			t.Fatalf("Save(%d): %v", i, err)
		}
	}

	// Walk the stream in pages of 3.
	var got []events.Event
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > total {
			t.Fatal("cursor pagination did not terminate")
		}
		page, err := repo.List(orgID, "", "", cursor, 3)
		if err != nil {
			t.Fatalf("List(before=%q): %v", cursor, err)
		}
		if len(page) == 0 {
			break
		}
		got = append(got, page...)
		cursor = page[len(page)-1].ID
	}

	if len(got) != total {
		t.Fatalf("paged walk returned %d events, want %d", len(got), total)
	}
	seen := map[string]bool{}
	for i, e := range got {
		if seen[e.ID] {
			t.Errorf("event %s returned twice", e.ID)
		}
		seen[e.ID] = true
		if i > 0 {
			prev := got[i-1]
			if e.CreatedAt.After(prev.CreatedAt) {
				t.Errorf("events out of order at %d: %v after %v", i, e.CreatedAt, prev.CreatedAt)
			}
			if e.CreatedAt.Equal(prev.CreatedAt) && e.ID >= prev.ID {
				t.Errorf("tie at %d not broken by id DESC: %s then %s", i, prev.ID, e.ID)
			}
		}
	}

	// Filters still apply under a cursor.
	filtered, err := repo.List(orgID, projectID, "artifact.updated", got[0].ID, 10)
	if err != nil {
		t.Fatalf("List with filters: %v", err)
	}
	if len(filtered) != total-1 {
		t.Errorf("filtered page after first event = %d rows, want %d", len(filtered), total-1)
	}

	// Unknown cursor: empty page, no error.
	empty, err := repo.List(orgID, "", "", uuid.New().String(), 10)
	if err != nil {
		t.Fatalf("List with unknown cursor: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("unknown cursor returned %d rows, want 0", len(empty))
	}

	// No cursor still returns the newest page.
	first, err := repo.List(orgID, "", "", "", 2)
	if err != nil {
		t.Fatalf("List first page: %v", err)
	}
	if len(first) != 2 || first[0].ID != got[0].ID {
		t.Errorf("first page = %v, want the two newest events", first)
	}
}

// TestEventListOrgScope guards the mandatory org filter alongside the new
// cursor parameter.
func TestEventListOrgScope(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewEventRepository(db)

	orgA, orgB := uuid.New().String(), uuid.New().String()
	for i, org := range []string{orgA, orgA, orgB} {
		e := events.New(fmt.Sprintf("artifact.type%d", i), "", "", "system", nil)
		e.OrgID = org
		if err := repo.Save(e); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	listA, err := repo.List(orgA, "", "", "", 10)
	if err != nil {
		t.Fatalf("List(orgA): %v", err)
	}
	if len(listA) != 2 {
		t.Errorf("orgA sees %d events, want 2", len(listA))
	}
}
