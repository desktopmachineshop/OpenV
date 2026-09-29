package providers

import "testing"

// memLogins is an in-memory LoginRepository holding copies, so that a
// service change is seen only once UpdateLogin stores it.
type memLogins struct {
	rows    map[string]LoginRequest
	updates int
}

func (m *memLogins) SaveLogin(l *LoginRequest) error { m.rows[l.ID] = *l; return nil }
func (m *memLogins) UpdateLogin(l *LoginRequest) error {
	m.updates++
	m.rows[l.ID] = *l
	return nil
}
func (m *memLogins) FindLoginByID(id string) (*LoginRequest, error) {
	l, ok := m.rows[id]
	if !ok {
		return nil, nil
	}
	return &l, nil
}
func (m *memLogins) FindActiveLogin(orgID, provider, target string, userID *string) (*LoginRequest, error) {
	return nil, nil
}
func (m *memLogins) ClaimPendingLogin(orgID, workerUserID string) (*LoginRequest, error) {
	return nil, nil
}

// TestProgressLeavesAFinishedSignInAsItIs pins that a worker's late report
// changes neither a completed nor a cancelled sign-in: the answer is the
// request as stored, and nothing is written. A failure reported after
// completion used to be stored, so a CLI that was signed in showed as a
// failed sign-in.
func TestProgressLeavesAFinishedSignInAsItIs(t *testing.T) {
	for _, final := range []struct{ status, detail string }{
		{LoginCompleted, "Signed in."},
		{LoginCancelled, "Cancelled by user."},
	} {
		for _, late := range []string{LoginFailed, LoginCompleted, LoginURLReady, LoginClaimed} {
			repo := &memLogins{rows: map[string]LoginRequest{
				"l1": {ID: "l1", OrgID: "org", Provider: ProviderClaudeCode, Target: LoginTargetWorkspace,
					Status: final.status, Detail: final.detail},
			}}
			svc := NewLoginService(repo)
			got, err := svc.Progress("l1", late, "https://late.example", "The CLI exited.", PasteKindCode)
			if err != nil {
				t.Fatalf("%s, then %s: %v", final.status, late, err)
			}
			if got.Status != final.status || got.Detail != final.detail || got.AuthURL != "" || got.PasteKind != "" {
				t.Fatalf("%s, then %s: answered %+v, want the request as it was", final.status, late, got)
			}
			if stored := repo.rows["l1"]; stored.Status != final.status || repo.updates != 0 {
				t.Fatalf("%s, then %s: stored %q after %d updates, want %q untouched", final.status, late,
					stored.Status, repo.updates, final.status)
			}
		}
	}
}

// TestProgressRecordsAFailureInFlight keeps the other side: a sign-in still
// in flight takes the worker's failure.
func TestProgressRecordsAFailureInFlight(t *testing.T) {
	repo := &memLogins{rows: map[string]LoginRequest{
		"l1": {ID: "l1", OrgID: "org", Provider: ProviderClaudeCode, Target: LoginTargetWorkspace, Status: LoginURLReady},
	}}
	got, err := NewLoginService(repo).Progress("l1", LoginFailed, "", "The CLI exited.", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != LoginFailed || repo.rows["l1"].Status != LoginFailed || got.Detail != "The CLI exited." {
		t.Fatalf("answered %+v, stored %q; want failed", got, repo.rows["l1"].Status)
	}
}
