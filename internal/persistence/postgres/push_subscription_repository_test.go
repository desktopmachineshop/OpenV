package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openv/requirements-platform/internal/domain/pushsubs"
)

// TestPushSubscriptionRepository exercises the repository against a real
// postgres (gated on OPENV_TEST_DATABASE_URL): upsert idempotency on the
// endpoint, user-scoped listing and deletion, the "gone" delete keyed on the
// endpoint alone, and the used/failed marks.
func TestPushSubscriptionRepository(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewPushSubscriptionRepository(db)

	mkUser := func(email string) string {
		id := uuid.New().String()
		if _, err := db.Exec(`
			INSERT INTO users (id, email, name, avatar_url, auth_provider, is_admin, created_at, updated_at)
			VALUES ($1, $2, $3, '', 'password', FALSE, NOW(), NOW())
		`, id, email, email); err != nil {
			t.Fatalf("seed user %s: %v", email, err)
		}
		return id
	}
	alice := mkUser("alice-" + uuid.New().String() + "@example.com")
	bob := mkUser("bob-" + uuid.New().String() + "@example.com")

	phone := pushsubs.New(alice, "https://push.example.com/alice-phone", "p256-1", "auth-1", "Pixel")
	tablet := pushsubs.New(alice, "https://push.example.com/alice-tablet", "p256-2", "auth-2", "iPad")
	bobs := pushsubs.New(bob, "https://push.example.com/bob-phone", "p256-3", "auth-3", "Bob's phone")
	// Distinct creation times so the newest-first ordering is unambiguous.
	phone.CreatedAt = time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	tablet.CreatedAt = time.Now().Truncate(time.Microsecond)
	for _, s := range []*pushsubs.Subscription{phone, tablet, bobs} {
		if err := repo.Upsert(s); err != nil {
			t.Fatalf("Upsert(%s): %v", s.UserAgent, err)
		}
	}

	// Listing is newest-first and scoped to the owner.
	list, err := repo.ListForUser(alice)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(list) != 2 || list[0].ID != tablet.ID || list[1].ID != phone.ID {
		t.Fatalf("list = %d rows (want alice's 2, newest first)", len(list))
	}
	if list[0].P256dh != "p256-2" || list[0].Auth != "auth-2" || list[0].UserAgent != "iPad" {
		t.Fatalf("row did not round-trip: %+v", list[0])
	}
	if list[0].LastUsedAt != nil || list[0].FailedAt != nil {
		t.Fatalf("fresh subscription has timestamps: %+v", list[0])
	}

	// Re-posting the SAME endpoint updates the keys instead of adding a row
	// (this is what makes POST idempotent).
	again := pushsubs.New(alice, phone.Endpoint, "p256-rotated", "auth-rotated", "Pixel 9")
	if err := repo.Upsert(again); err != nil {
		t.Fatalf("Upsert (re-subscribe): %v", err)
	}
	list, err = repo.ListForUser(alice)
	if err != nil {
		t.Fatalf("ListForUser after re-subscribe: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("re-subscribe created a second row: %d rows", len(list))
	}
	var rotated *pushsubs.Subscription
	for _, s := range list {
		if s.Endpoint == phone.Endpoint {
			rotated = s
		}
	}
	if rotated == nil || rotated.P256dh != "p256-rotated" || rotated.UserAgent != "Pixel 9" {
		t.Fatalf("keys were not refreshed: %+v", rotated)
	}
	if rotated.ID != phone.ID {
		t.Fatalf("re-subscribe changed the row id: %s -> %s", phone.ID, rotated.ID)
	}
	// Upsert fills the CANDIDATE in from the persisted row (RETURNING), so
	// the handler's 201 body describes the device on file rather than the row
	// it would have inserted had the endpoint been new.
	if again.ID != phone.ID {
		t.Fatalf("Upsert left the candidate id at %s, want the persisted %s", again.ID, phone.ID)
	}
	if !again.CreatedAt.Equal(phone.CreatedAt) {
		t.Fatalf("Upsert left created_at at %v, want the persisted %v", again.CreatedAt, phone.CreatedAt)
	}

	// Marks: a failure stamps failed_at; a later success clears it.
	failedAt := time.Now().Truncate(time.Microsecond)
	if err := repo.MarkFailed(phone.ID, failedAt); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if got := mustFind(t, repo, alice, phone.Endpoint); got.FailedAt == nil {
		t.Fatal("failed_at not recorded")
	}
	usedAt := failedAt.Add(time.Minute)
	if err := repo.MarkUsed(phone.ID, usedAt); err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}
	got := mustFind(t, repo, alice, phone.Endpoint)
	if got.FailedAt != nil {
		t.Fatal("a successful send must clear failed_at")
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(usedAt) {
		t.Fatalf("last_used_at = %v, want %v", got.LastUsedAt, usedAt)
	}

	// Bob cannot withdraw Alice's device.
	deleted, err := repo.DeleteForUser(bob, phone.Endpoint)
	if err != nil {
		t.Fatalf("DeleteForUser as bob: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("bob deleted %d of alice's subscriptions, want 0", deleted)
	}

	// Alice can withdraw her own.
	deleted, err = repo.DeleteForUser(alice, phone.Endpoint)
	if err != nil {
		t.Fatalf("DeleteForUser: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("DeleteForUser deleted %d rows, want 1", deleted)
	}

	// The sender's "gone" path is keyed on the endpoint alone.
	deleted, err = repo.DeleteByEndpoint(bobs.Endpoint)
	if err != nil {
		t.Fatalf("DeleteByEndpoint: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("DeleteByEndpoint deleted %d rows, want 1", deleted)
	}
	if rest, err := repo.ListForUser(bob); err != nil || len(rest) != 0 {
		t.Fatalf("bob still has %d subscriptions (err %v)", len(rest), err)
	}

	// Deleting the user cascades to their devices.
	if _, err := db.Exec(`DELETE FROM users WHERE id = $1`, alice); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if rest, err := repo.ListForUser(alice); err != nil || len(rest) != 0 {
		t.Fatalf("subscriptions survived the user: %d rows (err %v)", len(rest), err)
	}
}

// mustFind returns the user's subscription with the given endpoint.
func mustFind(t *testing.T, repo *PushSubscriptionRepository, userID, endpoint string) *pushsubs.Subscription {
	t.Helper()
	list, err := repo.ListForUser(userID)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	for _, s := range list {
		if s.Endpoint == endpoint {
			return s
		}
	}
	t.Fatalf("no subscription for endpoint %s", endpoint)
	return nil
}
