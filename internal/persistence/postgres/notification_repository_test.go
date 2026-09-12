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
	list, err := repo.List(alice, notifications.ListQuery{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
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
	unreadList, err := repo.List(alice, notifications.ListQuery{UnreadOnly: true, Limit: 10})
	if err != nil {
		t.Fatalf("List unread: %v", err)
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

// TestNotificationRepositoryClearArchivesRatherThanDeletes: clearing moves one
// user's inbox into the cleared view and leaves everyone else alone. The
// scoping is the security story — every statement here is keyed by user_id,
// and the delete is the only one that destroys anything.
func TestNotificationRepositoryClearArchivesRatherThanDeletes(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewNotificationRepository(db)

	alice := uuid.New().String()
	bob := uuid.New().String()
	mk := func(userID, title string) *notifications.Notification {
		return notifications.New("", userID, notifications.TypeRunFailed, title, "body", nil)
	}

	aliceRead := mk(alice, "already read")
	all := []*notifications.Notification{aliceRead, mk(alice, "unread one"), mk(alice, "unread two"), mk(bob, "bobs")}
	for _, n := range all {
		if err := repo.Insert(n); err != nil {
			t.Fatalf("Insert(%s): %v", n.Title, err)
		}
	}
	if _, err := repo.MarkRead(alice, []string{aliceRead.ID}); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	cleared, err := repo.ClearInbox(alice)
	if err != nil {
		t.Fatalf("ClearInbox: %v", err)
	}
	if cleared != 3 {
		t.Fatalf("cleared = %d, want 3 (two unread and one read)", cleared)
	}

	// The inbox is empty and the badge is zero...
	inbox, err := repo.List(alice, notifications.ListQuery{Limit: 10})
	if err != nil {
		t.Fatalf("List inbox: %v", err)
	}
	if len(inbox) != 0 {
		t.Fatalf("alice's inbox still holds %d rows after a clear", len(inbox))
	}
	unread, err := repo.CountUnread(alice)
	if err != nil {
		t.Fatalf("CountUnread: %v", err)
	}
	if unread != 0 {
		t.Fatalf("unread = %d after a clear, want 0", unread)
	}

	// ...but nothing was destroyed: the history has all three, stamped.
	history, err := repo.List(alice, notifications.ListQuery{View: notifications.ViewCleared, Limit: 10})
	if err != nil {
		t.Fatalf("List cleared: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("cleared view holds %d rows, want 3", len(history))
	}
	for _, n := range history {
		if n.ClearedAt == nil {
			t.Errorf("%q is in the cleared view with no cleared_at", n.Title)
		}
		if !n.Read {
			t.Errorf("%q was cleared but is still unread", n.Title)
		}
	}

	// Bob is untouched.
	bobs, err := repo.List(bob, notifications.ListQuery{Limit: 10})
	if err != nil {
		t.Fatalf("List(bob): %v", err)
	}
	if len(bobs) != 1 || bobs[0].Title != "bobs" {
		t.Fatalf("bob's inbox = %+v, want his one notification intact", bobs)
	}

	// Clearing an empty inbox is a no-op.
	again, err := repo.ClearInbox(alice)
	if err != nil {
		t.Fatalf("second ClearInbox: %v", err)
	}
	if again != 0 {
		t.Fatalf("second clear moved %d rows, want 0", again)
	}

	// The purge is the only destructive path, and it takes cleared rows only.
	deleted, err := repo.DeleteCleared(alice)
	if err != nil {
		t.Fatalf("DeleteCleared: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("deleted = %d, want 3", deleted)
	}
	gone, err := repo.List(alice, notifications.ListQuery{View: notifications.ViewCleared, Limit: 10})
	if err != nil {
		t.Fatalf("List cleared after purge: %v", err)
	}
	if len(gone) != 0 {
		t.Fatalf("cleared view still holds %d rows after the purge", len(gone))
	}
	stillBobs, err := repo.List(bob, notifications.ListQuery{Limit: 10})
	if err != nil {
		t.Fatalf("List(bob) after purge: %v", err)
	}
	if len(stillBobs) != 1 {
		t.Fatalf("the purge reached bob's rows: %+v", stillBobs)
	}
}

// TestNotificationRepositoryFlagging: a flag is the member's own, survives a
// clear, and is what makes the flagged view findable afterwards.
func TestNotificationRepositoryFlagging(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewNotificationRepository(db)

	alice := uuid.New().String()
	bob := uuid.New().String()
	keep := notifications.New("", alice, notifications.TypeRunFailed, "keep me", "body", nil)
	other := notifications.New("", alice, notifications.TypeRunFailed, "ordinary", "body", nil)
	bobs := notifications.New("", bob, notifications.TypeRunFailed, "bobs", "body", nil)
	for _, n := range []*notifications.Notification{keep, other, bobs} {
		if err := repo.Insert(n); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	found, err := repo.SetFlagged(alice, keep.ID, true)
	if err != nil || !found {
		t.Fatalf("SetFlagged: found = %v, err = %v", found, err)
	}

	// Somebody else's id matches nothing — the same answer a missing id gets,
	// so the endpoint cannot be used to prove a notification exists.
	found, err = repo.SetFlagged(alice, bobs.ID, true)
	if err != nil {
		t.Fatalf("SetFlagged on another user's row: %v", err)
	}
	if found {
		t.Fatal("flagged another user's notification")
	}

	flagged, err := repo.List(alice, notifications.ListQuery{View: notifications.ViewFlagged, Limit: 10})
	if err != nil {
		t.Fatalf("List flagged: %v", err)
	}
	if len(flagged) != 1 || flagged[0].ID != keep.ID || !flagged[0].Flagged {
		t.Fatalf("flagged view = %+v, want just the flagged row", flagged)
	}

	// A clear does not spare the flagged row — but the flagged view still
	// finds it, which is the point of flagging.
	if _, err := repo.ClearInbox(alice); err != nil {
		t.Fatalf("ClearInbox: %v", err)
	}
	stillFlagged, err := repo.List(alice, notifications.ListQuery{View: notifications.ViewFlagged, Limit: 10})
	if err != nil {
		t.Fatalf("List flagged after clear: %v", err)
	}
	if len(stillFlagged) != 1 || stillFlagged[0].ID != keep.ID {
		t.Fatalf("flagged view after a clear = %+v, want the flagged row still there", stillFlagged)
	}
	if stillFlagged[0].ClearedAt == nil {
		t.Error("the flagged row should have been cleared along with the rest")
	}

	// Unflagging takes it back out of the view.
	if _, err := repo.SetFlagged(alice, keep.ID, false); err != nil {
		t.Fatalf("unflag: %v", err)
	}
	empty, err := repo.List(alice, notifications.ListQuery{View: notifications.ViewFlagged, Limit: 10})
	if err != nil {
		t.Fatalf("List flagged after unflag: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("flagged view still holds %d rows after unflagging", len(empty))
	}
}

// TestNotificationRepositoryCursorPaging: the history pages without skipping
// or repeating, including across rows that share a timestamp — which a fan-out
// produces routinely, and which an ORDER BY created_at alone cannot separate.
func TestNotificationRepositoryCursorPaging(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewNotificationRepository(db)

	alice := uuid.New().String()
	same := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	var inserted []*notifications.Notification
	for i := 0; i < 7; i++ {
		n := notifications.New("", alice, notifications.TypeRunFailed, "n", "body", nil)
		// The first three share an instant; the rest are a minute apart.
		if i < 3 {
			n.CreatedAt = same
		} else {
			n.CreatedAt = same.Add(time.Duration(i) * time.Minute)
		}
		if err := repo.Insert(n); err != nil {
			t.Fatalf("Insert: %v", err)
		}
		inserted = append(inserted, n)
	}

	seen := map[string]bool{}
	var cursorTime time.Time
	var cursorID string
	for page := 0; page < 5; page++ {
		q := notifications.ListQuery{Limit: 3, BeforeTime: cursorTime, BeforeID: cursorID}
		rows, err := repo.List(alice, q)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(rows) == 0 {
			break
		}
		for _, n := range rows {
			if seen[n.ID] {
				t.Fatalf("row %s came back on more than one page", n.ID)
			}
			seen[n.ID] = true
		}
		last := rows[len(rows)-1]
		cursorTime, cursorID = last.CreatedAt, last.ID
	}
	if len(seen) != len(inserted) {
		t.Fatalf("paged over %d rows, want all %d", len(seen), len(inserted))
	}
}
