package interviews

import "testing"

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
