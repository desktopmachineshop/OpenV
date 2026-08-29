package interviews

import "testing"

// sessionRepo is a minimal in-memory repository for exercising session
// creation, resume, and participant-name backfill. It embeds Repository so
// only the session methods StartOrResumeSession touches need bodies.
type sessionRepo struct {
	Repository
	active      *Session
	saved       *Session
	updatedName string
	updates     int
}

func (r *sessionRepo) FindActiveSessionByInvite(inviteID string) (*Session, error) {
	return r.active, nil
}

func (r *sessionRepo) SaveSession(s *Session) error {
	r.saved = s
	r.active = s
	return nil
}

// SetParticipantName is the targeted single-column backfill write (#212). The
// backfill uses this rather than a full-row UpdateSession so a concurrent
// CompleteSession's status/summary/ended_at cannot be reverted.
func (r *sessionRepo) SetParticipantName(sessionID, name string) error {
	r.updates++
	r.updatedName = name
	if r.active != nil {
		r.active.ParticipantName = name
	}
	return nil
}

// TestStartOrResumeSessionBackfillsParticipantName locks in the #205 fix: the
// SSE stream creates the active session anonymously before the name gate
// submits, so the first message's participant name must be persisted onto the
// still-anonymous session rather than dropped.
func TestStartOrResumeSessionBackfillsParticipantName(t *testing.T) {
	t.Run("fills a blank name on resume", func(t *testing.T) {
		repo := &sessionRepo{active: &Session{ID: "sess-1", ParticipantName: ""}}
		svc := NewDefaultService(repo)

		got, err := svc.StartOrResumeSession("inv-1", "int-1", "  Ada  ")
		if err != nil {
			t.Fatalf("StartOrResumeSession: %v", err)
		}
		if got.ParticipantName != "Ada" {
			t.Fatalf("participant name = %q, want %q (trimmed)", got.ParticipantName, "Ada")
		}
		if repo.updates != 1 || repo.updatedName != "Ada" {
			t.Fatalf("name not persisted via SetParticipantName (updates=%d, updatedName=%q)", repo.updates, repo.updatedName)
		}
	})

	t.Run("never overwrites an existing name", func(t *testing.T) {
		repo := &sessionRepo{active: &Session{ID: "sess-1", ParticipantName: "Grace"}}
		svc := NewDefaultService(repo)

		got, err := svc.StartOrResumeSession("inv-1", "int-1", "Ada")
		if err != nil {
			t.Fatalf("StartOrResumeSession: %v", err)
		}
		if got.ParticipantName != "Grace" {
			t.Fatalf("participant name = %q, want it left as %q", got.ParticipantName, "Grace")
		}
		if repo.updates != 0 {
			t.Fatalf("SetParticipantName called %d times; must not touch a named session", repo.updates)
		}
	})

	t.Run("blank name on a blank session writes nothing", func(t *testing.T) {
		repo := &sessionRepo{active: &Session{ID: "sess-1", ParticipantName: ""}}
		svc := NewDefaultService(repo)

		if _, err := svc.StartOrResumeSession("inv-1", "int-1", "   "); err != nil {
			t.Fatalf("StartOrResumeSession: %v", err)
		}
		if repo.updates != 0 {
			t.Fatalf("SetParticipantName called %d times for a blank name", repo.updates)
		}
	})

	t.Run("new session keeps the provided name", func(t *testing.T) {
		repo := &sessionRepo{active: nil}
		svc := NewDefaultService(repo)

		got, err := svc.StartOrResumeSession("inv-1", "int-1", "Ada")
		if err != nil {
			t.Fatalf("StartOrResumeSession: %v", err)
		}
		if got.ParticipantName != "Ada" {
			t.Fatalf("new session participant name = %q, want %q", got.ParticipantName, "Ada")
		}
		if repo.saved == nil {
			t.Fatal("new session was not saved")
		}
	})
}

// limitRecordingRepo embeds the repository interface so only the
// project-session listing needs an implementation.
type limitRecordingRepo struct {
	Repository
	gotLimit int
}

func (r *limitRecordingRepo) ListSessionsByProject(projectID string, limit int) ([]*Session, error) {
	r.gotLimit = limit
	return nil, nil
}

// TestListProjectSessionsLimitClamping locks in the default and cap applied
// to the project-wide session listing.
func TestListProjectSessionsLimitClamping(t *testing.T) {
	repo := &limitRecordingRepo{}
	svc := NewDefaultService(repo)

	cases := []struct {
		name  string
		limit int
		want  int
	}{
		{"zero falls back to default", 0, DefaultProjectSessionLimit},
		{"negative falls back to default", -3, DefaultProjectSessionLimit},
		{"in-range passes through", 7, 7},
		{"over the cap is capped", 5000, MaxProjectSessionLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.ListProjectSessions("proj-1", tc.limit); err != nil {
				t.Fatalf("ListProjectSessions: %v", err)
			}
			if repo.gotLimit != tc.want {
				t.Fatalf("repo got limit %d, want %d", repo.gotLimit, tc.want)
			}
		})
	}
}
