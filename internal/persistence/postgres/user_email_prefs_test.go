package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openv/requirements-platform/internal/domain/users"
)

// TestUserEmailNotificationsColumn exercises migration 0013 and the repository
// round-trip: a freshly-saved user defaults to email_notifications=true, and
// SetEmailNotifications flips the stored value for that user only.
func TestUserEmailNotificationsColumn(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewUserRepository(db)

	now := time.Now().UTC()
	u := &users.User{
		ID:           uuid.New().String(),
		Email:        "prefs@example.com",
		Name:         "Prefs User",
		AuthProvider: users.ProviderPassword,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := repo.SaveUser(u); err != nil {
		t.Fatalf("SaveUser: %v", err)
	}

	got, err := repo.FindUserByID(u.ID)
	if err != nil {
		t.Fatalf("FindUserByID: %v", err)
	}
	if !got.EmailNotifications {
		t.Fatalf("default email_notifications = false, want true (DB default)")
	}

	if err := repo.SetEmailNotifications(u.ID, false); err != nil {
		t.Fatalf("SetEmailNotifications: %v", err)
	}
	got, err = repo.FindUserByID(u.ID)
	if err != nil {
		t.Fatalf("FindUserByID after opt-out: %v", err)
	}
	if got.EmailNotifications {
		t.Fatalf("email_notifications = true after opt-out, want false")
	}

	if err := repo.SetEmailNotifications(u.ID, true); err != nil {
		t.Fatalf("SetEmailNotifications re-enable: %v", err)
	}
	got, _ = repo.FindUserByID(u.ID)
	if !got.EmailNotifications {
		t.Fatalf("email_notifications = false after re-enable, want true")
	}
}
