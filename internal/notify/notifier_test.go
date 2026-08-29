package notify

import (
	"errors"
	"sort"
	"testing"

	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/notifications"
)

// fakeStore records created notifications.
type fakeStore struct {
	notifications.Service
	created []*notifications.Notification
	err     error
}

func (f *fakeStore) Create(n *notifications.Notification) error {
	if f.err != nil {
		return f.err
	}
	f.created = append(f.created, n)
	return nil
}

// fakeMembers answers a fixed member list.
type fakeMembers struct {
	list []*members.Member
	err  error
}

func (f *fakeMembers) ListMembers(projectID string) ([]*members.Member, error) {
	return f.list, f.err
}

// fakeBroadcaster records SSE pushes.
type fakeBroadcaster struct {
	keys   []string
	events []string
}

func (f *fakeBroadcaster) BroadcastSession(key, event string, data interface{}) {
	f.keys = append(f.keys, key)
	f.events = append(f.events, event)
}

func member(userID, role, name, email string) *members.Member {
	return &members.Member{UserID: userID, Role: role, UserName: name, UserEmail: email}
}

func recipients(t *testing.T, store *fakeStore) []string {
	t.Helper()
	var ids []string
	for _, n := range store.created {
		ids = append(ids, n.UserID)
	}
	sort.Strings(ids)
	return ids
}

// TestProposalPendingFansOutToEditors locks in the proposal rule: editors
// and owners get a row, viewers do not, and the SSE push mirrors storage.
func TestProposalPendingFansOutToEditors(t *testing.T) {
	store := &fakeStore{}
	bc := &fakeBroadcaster{}
	n := NewNotifier(store, &fakeMembers{list: []*members.Member{
		member("u-owner", members.RoleOwner, "Olive Owner", "olive@example.com"),
		member("u-editor", members.RoleEditor, "Ed Editor", "ed@example.com"),
		member("u-viewer", members.RoleViewer, "Vi Viewer", "vi@example.com"),
	}}, bc)

	n.Handle(domainevents.Event{
		EventType: domainevents.ProposalCreated,
		OrgID:     "org-1",
		ProjectID: "p-1",
		EntityID:  "prop-1",
		Actor:     "agent:run-1",
		Payload:   map[string]interface{}{"op": "create_artifact", "run_id": "run-1"},
	})

	want := []string{"u-editor", "u-owner"}
	got := recipients(t, store)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("recipients = %v, want %v", got, want)
	}
	for _, created := range store.created {
		if created.Type != notifications.TypeProposalPending {
			t.Errorf("type = %q, want %q", created.Type, notifications.TypeProposalPending)
		}
		if created.EntityRef["kind"] != "proposal" || created.EntityRef["proposal_id"] != "prop-1" {
			t.Errorf("entity_ref = %v, want kind=proposal proposal_id=prop-1", created.EntityRef)
		}
		if created.OrgID != "org-1" {
			t.Errorf("org_id = %q, want org-1", created.OrgID)
		}
	}
	if len(bc.keys) != 2 {
		t.Fatalf("broadcasts = %v, want 2 pushes", bc.keys)
	}
	sort.Strings(bc.keys)
	if bc.keys[0] != "notify:u-editor" || bc.keys[1] != "notify:u-owner" {
		t.Errorf("broadcast keys = %v, want notify:<user> keys", bc.keys)
	}
	if bc.events[0] != "notification" {
		t.Errorf("broadcast event = %q, want notification", bc.events[0])
	}
}

// TestRunFailedNotifiesLauncherOnly locks in the run rule: only failed runs
// notify, only the launcher receives it, and system runs (no launcher) are
// silent.
func TestRunFailedNotifiesLauncherOnly(t *testing.T) {
	cases := []struct {
		name       string
		status     string
		launchedBy string
		wantCount  int
	}{
		{"failed run notifies launcher", "failed", "u-1", 1},
		{"succeeded run is silent", "succeeded", "u-1", 0},
		{"cancelled run is silent", "cancelled", "u-1", 0},
		{"failed automation run without launcher is silent", "failed", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{}
			n := NewNotifier(store, &fakeMembers{}, nil)
			n.Handle(domainevents.Event{
				EventType: domainevents.RunFinished,
				ProjectID: "p-1",
				EntityID:  "run-9",
				Actor:     "agent:run-9",
				Payload: map[string]interface{}{
					"status":      tc.status,
					"launched_by": tc.launchedBy,
				},
			})
			if len(store.created) != tc.wantCount {
				t.Fatalf("created %d notifications, want %d", len(store.created), tc.wantCount)
			}
			if tc.wantCount == 1 {
				got := store.created[0]
				if got.UserID != tc.launchedBy {
					t.Errorf("recipient = %q, want %q", got.UserID, tc.launchedBy)
				}
				if got.Type != notifications.TypeRunFailed {
					t.Errorf("type = %q, want %q", got.Type, notifications.TypeRunFailed)
				}
				if got.EntityRef["kind"] != "run" || got.EntityRef["run_id"] != "run-9" {
					t.Errorf("entity_ref = %v, want kind=run run_id=run-9", got.EntityRef)
				}
			}
		})
	}
}

// TestInterviewCompletedNotifiesEditors locks in the interview rule, which
// rides on the chatter.created event with kind "interview-completed".
func TestInterviewCompletedNotifiesEditors(t *testing.T) {
	store := &fakeStore{}
	n := NewNotifier(store, &fakeMembers{list: []*members.Member{
		member("u-editor", members.RoleEditor, "Ed", "ed@example.com"),
		member("u-viewer", members.RoleViewer, "Vi", "vi@example.com"),
	}}, nil)

	n.Handle(domainevents.Event{
		EventType: domainevents.ChatterCreated,
		ProjectID: "p-1",
		EntityID:  "sess-1",
		Actor:     "system",
		Payload:   map[string]interface{}{"kind": "interview-completed"},
	})

	if len(store.created) != 1 || store.created[0].UserID != "u-editor" {
		t.Fatalf("recipients = %v, want exactly u-editor", recipients(t, store))
	}
	got := store.created[0]
	if got.Type != notifications.TypeInterviewCompleted {
		t.Errorf("type = %q, want %q", got.Type, notifications.TypeInterviewCompleted)
	}
	if got.EntityRef["kind"] != "interview" || got.EntityRef["session_id"] != "sess-1" {
		t.Errorf("entity_ref = %v, want kind=interview session_id=sess-1", got.EntityRef)
	}
}

// TestMentionsNotifyMatchedMembers locks in mention matching: first name,
// full name without spaces, and email local part all match; the comment
// author never notifies themselves; unrelated members stay silent.
func TestMentionsNotifyMatchedMembers(t *testing.T) {
	store := &fakeStore{}
	n := NewNotifier(store, &fakeMembers{list: []*members.Member{
		member("u-ada", members.RoleEditor, "Ada Lovelace", "ada@example.com"),
		member("u-grace", members.RoleViewer, "Grace Hopper", "grace.hopper@example.com"),
		member("u-alan", members.RoleEditor, "Alan Turing", "alan@example.com"),
		member("u-self", members.RoleOwner, "Self Author", "self@example.com"),
	}}, nil)

	n.Handle(domainevents.Event{
		EventType: domainevents.ChatterCreated,
		ProjectID: "p-1",
		EntityID:  "chat-1",
		Actor:     "user:u-self",
		Payload: map[string]interface{}{
			"entry_type":  "comment",
			"artifact_id": "art-1",
			"message":     "@ada and @grace.hopper please review; cc @self",
		},
	})

	want := []string{"u-ada", "u-grace"}
	got := recipients(t, store)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("recipients = %v, want %v (author excluded, alan unmatched)", got, want)
	}
	for _, created := range store.created {
		if created.Type != notifications.TypeMention {
			t.Errorf("type = %q, want %q", created.Type, notifications.TypeMention)
		}
		if created.EntityRef["kind"] != "artifact" || created.EntityRef["artifact_id"] != "art-1" {
			t.Errorf("entity_ref = %v, want kind=artifact artifact_id=art-1", created.EntityRef)
		}
	}
}

// TestActorNeverNotifiedAndAutoEntriesSkipped: a user-actor event never
// notifies the actor themselves, and non-comment chatter (auto entries,
// agent notes) never triggers mention scans.
func TestActorNeverNotifiedAndAutoEntriesSkipped(t *testing.T) {
	store := &fakeStore{}
	n := NewNotifier(store, &fakeMembers{list: []*members.Member{
		member("u-1", members.RoleOwner, "Solo Owner", "solo@example.com"),
	}}, nil)

	// Owner triggers a proposal-like event themselves: no self-notification.
	n.Handle(domainevents.Event{
		EventType: domainevents.ProposalCreated,
		ProjectID: "p-1",
		EntityID:  "prop-2",
		Actor:     "user:u-1",
	})
	// Auto entry mentioning the owner: mention scan must not run.
	n.Handle(domainevents.Event{
		EventType: domainevents.ChatterCreated,
		ProjectID: "p-1",
		EntityID:  "chat-2",
		Actor:     "system",
		Payload: map[string]interface{}{
			"entry_type": "link-change",
			"message":    "@solo auto-updated to version 2",
		},
	})
	if len(store.created) != 0 {
		t.Fatalf("created %d notifications, want 0", len(store.created))
	}
}

// TestMemberListFailureIsSwallowed: a repository error aborts the fan-out
// without panicking (the bus subscriber must never take down dispatch).
func TestMemberListFailureIsSwallowed(t *testing.T) {
	store := &fakeStore{}
	n := NewNotifier(store, &fakeMembers{err: errors.New("db down")}, nil)
	n.Handle(domainevents.Event{
		EventType: domainevents.ProposalCreated,
		ProjectID: "p-1",
		EntityID:  "prop-3",
		Actor:     "agent:run-1",
	})
	if len(store.created) != 0 {
		t.Fatalf("created %d notifications, want 0", len(store.created))
	}
}
