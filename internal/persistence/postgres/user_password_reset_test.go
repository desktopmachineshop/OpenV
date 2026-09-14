package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openv/requirements-platform/internal/domain/users"
)

func saveReset(t *testing.T, repo *UserRepository, userID, token, delivery string, issuedBy *string, expires time.Time) {
	t.Helper()
	err := repo.SavePasswordReset(&users.PasswordReset{
		ID: uuid.New().String(), UserID: userID, TokenHash: users.HashToken(token),
		Delivery: delivery, IssuedBy: issuedBy, ExpiresAt: expires, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("SavePasswordReset(%s): %v", token, err)
	}
}

// Reset links (REQ-158): one live per account, single use, dead once
// expired, and the row says how the link was delivered and by whom.
func TestPasswordResetRoundTrip(t *testing.T) {
	db := testDB(t)
	initTestSchema(t, db)
	repo := NewUserRepository(db)
	owner := saveTestUser(t, repo, "owner@example.com", users.ProviderPassword, true)
	admin := saveTestUser(t, repo, "admin@example.com", users.ProviderPassword, true)

	saveReset(t, repo, owner.ID, "first", users.ResetDeliveryEmail, nil, time.Now().Add(time.Hour))
	saveReset(t, repo, owner.ID, "second", users.ResetDeliveryAdmin, &admin.ID, time.Now().Add(24*time.Hour))
	if v, err := repo.ConsumePasswordReset(users.HashToken("first"), time.Now()); err != nil || v != nil {
		t.Fatalf("superseded link: %+v %v, want nil, nil", v, err)
	}
	v, err := repo.ConsumePasswordReset(users.HashToken("second"), time.Now())
	if err != nil {
		t.Fatalf("ConsumePasswordReset: %v", err)
	}
	if v == nil || v.UserID != owner.ID || v.Delivery != users.ResetDeliveryAdmin || v.IssuedBy == nil || *v.IssuedBy != admin.ID || !v.Used {
		t.Fatalf("consume returned %+v", v)
	}
	if v, err := repo.ConsumePasswordReset(users.HashToken("second"), time.Now()); err != nil || v != nil {
		t.Fatalf("second consume: %+v %v, want nil, nil", v, err)
	}

	saveReset(t, repo, owner.ID, "late", users.ResetDeliveryEmail, nil, time.Now().Add(-time.Minute))
	if v, err := repo.ConsumePasswordReset(users.HashToken("late"), time.Now()); err != nil || v != nil {
		t.Fatalf("expired link: %+v %v, want nil, nil", v, err)
	}
	if v, err := repo.ConsumePasswordReset(users.HashToken("never"), time.Now()); err != nil || v != nil {
		t.Fatalf("unknown link: %+v %v, want nil, nil", v, err)
	}
}
