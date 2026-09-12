package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openv/requirements-platform/internal/domain/notifications"
)

// TestNotificationRepositoryRoundTrip exercises the full repository against
// a real postgres (gated on OPENV_TEST_DATABASE_URL): insert, ordered
// listing with the unread filter, user-scoped mark-read (another user's ids
// must not flip), mark-all, and the unread count.
func TestNotificationRepositoryRoundTrip(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewNotificationRepository(db)

	alice := uuid.New().String()
	bob := uuid.New().String()

	mk := func(userID, title string, at time.Time) *notifications.Notification {
		n := notifications.New("", userID, notifications.TypeRunFailed, title, "body", map[string]interface{}{
			"kind":   "run",
			"run_id": "run-1",
		})
		n.CreatedAt = at
		return n
	}

	base := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	a1 := mk(alice, "oldest", base)
	a2 := mk(alice, "newest", base.Add(time.Minute))
	b1 := mk(bob, "bobs", base)
	for _, n := range []*notifications.Notification{a1, a2, b1} {
		if err := repo.Insert(n); err != nil {
			t.Fatalf("Insert(%s): %v", n.Title, err)
		}
	}

	// Listing is newest-first and scoped to the user.
	list, err := repo.ListForUser(alice, false, 10)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(list) != 2 || list[0].ID != a2.ID || list[1].ID != a1.ID {
		t.Fatalf("list = %d rows (want alice's 2, newest first)", len(list))
	}
	if list[0].EntityRef["kind"] != "run" || list[0].EntityRef["run_id"] != "run-1" {
		t.Fatalf("entity_ref did not round-trip: %v", list[0].EntityRef)
	}

	// Bob cannot mark Alice's rows read.
	updated, err := repo.MarkRead(bob, []string{a1.ID, a2.ID})
	if err != nil {
		t.Fatalf("MarkRead as bob: %v", err)
	}
	if updated != 0 {
		t.Fatalf("bob marked %d of alice's rows read, want 0", updated)
	}
	if count, _ := repo.CountUnread(alice); count != 2 {
		t.Fatalf("alice unread = %d after bob's attempt, want 2", count)
	}

	// Alice marks one read; the unread filter and count follow.
	updated, err = repo.MarkRead(alice, []string{a1.ID})
	if err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if updated != 1 {
		t.Fatalf("MarkRead updated %d, want 1", updated)
	}
	unreadList, err := repo.ListForUser(alice, true, 10)
	if err != nil {
		t.Fatalf("ListForUser unread: %v", err)
	}
	if len(unreadList) != 1 || unreadList[0].ID != a2.ID {
		t.Fatalf("unread list = %d rows, want just a2", len(unreadList))
	}
	// Marking an already-read row again is a no-op.
	if updated, _ = repo.MarkRead(alice, []string{a1.ID}); updated != 0 {
		t.Fatalf("re-marking read updated %d, want 0", updated)
	}

	// Mark-all only touches the caller's rows.
	updated, err = repo.MarkAllRead(alice)
	if err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}
	if updated != 1 {
		t.Fatalf("MarkAllRead updated %d, want 1", updated)
	}
	if count, _ := repo.CountUnread(alice); count != 0 {
		t.Fatalf("alice unread = %d, want 0", count)
	}
	if count, _ := repo.CountUnread(bob); count != 1 {
		t.Fatalf("bob unread = %d, want 1 (untouched by alice's mark-all)", count)
	}
}

// TestNotificationRepositoryDeleteAllForUser: a clear empties one user's list
// and leaves everyone else's alone. The scoping is the whole security story
// here — the statement is a bare DELETE, so if the user_id predicate were ever
// dropped it would take the entire table with it.
func TestNotificationRepositoryDeleteAllForUser(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewNotificationRepository(db)

	alice := uuid.New().String()
	bob := uuid.New().String()
	mk := func(userID, title string) *notifications.Notification {
		return notifications.New("", userID, notifications.TypeRunFailed, title, "body", nil)
	}

	aliceRead := mk(alice, "already read")
	for _, n := range []*notifications.Notification{aliceRead, mk(alice, "unread one"), mk(alice, "unread two"), mk(bob, "bobs")} {
		if err := repo.Insert(n); err != nil {
			t.Fatalf("Insert(%s): %v", n.Title, err)
		}
	}
	// One of Alice's is read, to prove the clear takes read and unread alike.
	if _, err := repo.MarkRead(alice, []string{aliceRead.ID}); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	deleted, err := repo.DeleteAllForUser(alice)
	if err != nil {
		t.Fatalf("DeleteAllForUser: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("deleted = %d, want 3 (two unread and one read)", deleted)
	}

	left, err := repo.ListForUser(alice, false, 10)
	if err != nil {
		t.Fatalf("ListForUser(alice): %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("alice still has %d notifications after a clear", len(left))
	}
	unread, err := repo.CountUnread(alice)
	if err != nil {
		t.Fatalf("CountUnread(alice): %v", err)
	}
	if unread != 0 {
		t.Fatalf("alice's unread count = %d after a clear, want 0", unread)
	}

	// Bob is untouched.
	bobs, err := repo.ListForUser(bob, false, 10)
	if err != nil {
		t.Fatalf("ListForUser(bob): %v", err)
	}
	if len(bobs) != 1 || bobs[0].Title != "bobs" {
		t.Fatalf("bob's list = %+v, want his one notification intact", bobs)
	}

	// Clearing an already-empty list is a no-op rather than an error.
	again, err := repo.DeleteAllForUser(alice)
	if err != nil {
		t.Fatalf("second DeleteAllForUser: %v", err)
	}
	if again != 0 {
		t.Fatalf("second clear deleted %d rows, want 0", again)
	}
}
