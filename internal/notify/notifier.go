// Package notify subscribes to the domain event bus and fans events out into
// per-user notification rows (issue #132), mirroring how the automation
// trigger matcher consumes the same bus. Each stored row is also pushed over
// the SSE hub on the recipient's personal stream key ("notify:<user_id>") so
// open tabs update their bell badge live.
package notify

import (
	"fmt"
	"log/slog"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	domainevents "github.com/openv/requirements-platform/internal/domain/events"
	"github.com/openv/requirements-platform/internal/domain/members"
	"github.com/openv/requirements-platform/internal/domain/mentions"
	"github.com/openv/requirements-platform/internal/domain/notifications"
)

// StreamKey returns the SSE hub key carrying a user's live notifications.
func StreamKey(userID string) string { return "notify:" + userID }

// MemberLister is the slice of members.Service the notifier needs.
type MemberLister interface {
	ListMembers(projectID string) ([]*members.Member, error)
}

// Broadcaster pushes live SSE frames; *api.SSEHub satisfies it.
type Broadcaster interface {
	BroadcastSession(key string, event string, data interface{})
}

// Notifier is the bus subscriber that materializes notifications.
type Notifier struct {
	// delivery holds the store, the SSE broadcaster and the optional email
	// and web push side channels (nil means that channel is off).
	delivery  Delivery
	memberSvc MemberLister
	// orgSvc and userNamer serve the membership fan-out (see membership.go).
	// Both are optional: without orgSvc the membership events are ignored
	// rather than half-delivered.
	orgSvc    OrgMemberLister
	userNamer UserNamer
}

// NewNotifier creates a notifier. broadcaster may be nil (store-only mode,
// used in tests).
func NewNotifier(store notifications.Service, memberSvc MemberLister, broadcaster Broadcaster) *Notifier {
	return &Notifier{delivery: Delivery{Store: store, Broadcaster: broadcaster}, memberSvc: memberSvc}
}

// SetChannels attaches the email and web push side channels at once; a nil
// one is left off.
func (n *Notifier) SetChannels(c Channels) *Notifier {
	n.delivery.Channels = c
	return n
}

// SetEmailDispatcher attaches an email side channel. Passing nil (or never
// calling this) leaves email off.
func (n *Notifier) SetEmailDispatcher(d *EmailDispatcher) *Notifier {
	n.delivery.Email = d
	return n
}

// SetPushDispatcher attaches a web push side channel. Passing nil (or never
// calling this) leaves push off.
func (n *Notifier) SetPushDispatcher(d *PushDispatcher) *Notifier {
	n.delivery.Push = d
	return n
}

// Start subscribes to the bus.
func (n *Notifier) Start(bus domainevents.Bus) {
	bus.Subscribe(n.Handle)
}

// Handle routes one event. Exported so tests can drive it without a bus.
//
// Fan-out rules:
//   - proposal.created            -> project editors and owners
//   - agentrun.finished (failed)  -> the user who launched the run
//   - artifact.status_changed to in_review -> project editors and owners (reviewers)
//   - chatter.created with kind "interview-completed" -> project editors and owners
//   - chatter.created comments containing @mentions   -> mentioned project members
//   - membership and role changes -> the affected person, and the
//     workspace's admins (see membership.go)
//
// The acting user (actor "user:<id>") never receives a notification for
// their own action.
func (n *Notifier) Handle(e domainevents.Event) {
	// Membership first: it covers eight event types and answers whether it
	// took the event, which keeps the switch below about content rather than
	// access.
	if n.handleMembership(e) {
		return
	}
	switch e.EventType {
	case domainevents.ProposalCreated:
		n.fanOutToEditors(e, notifications.TypeProposalPending,
			"Agent proposal pending review",
			"An agent submitted a change that needs your approval.",
			map[string]interface{}{
				"kind":        "proposal",
				"proposal_id": e.EntityID,
				"project_id":  e.ProjectID,
				"run_id":      payloadString(e, "run_id"),
			})

	case domainevents.RunFinished:
		if payloadString(e, "status") != "failed" {
			return
		}
		launcher := payloadString(e, "launched_by")
		if launcher == "" {
			return // system/automation-launched run; nobody to tell
		}
		n.deliver(e, launcher, notifications.TypeRunFailed,
			"Agent run failed",
			"An agent run you launched finished with an error.",
			map[string]interface{}{
				"kind":       "run",
				"run_id":     e.EntityID,
				"project_id": e.ProjectID,
			})

	case domainevents.ArtifactStatusChanged:
		// Only entering review is worth a reviewer's attention; every other
		// transition (draft, approved, superseded) is not a review request.
		if payloadString(e, "to") != artifacts.StatusInReview {
			return
		}
		title := memberText(payloadString(e, "title"))
		body := "An artifact is waiting in the review queue."
		if title != "" {
			// Quoted as written: %q would escape the title's own quotes and
			// backslashes for Go, which a member then reads in the bell, the
			// email and the push.
			body = `"` + title + `" is waiting in the review queue.`
		}
		n.fanOutToEditors(e, notifications.TypeReviewRequested,
			"Artifact ready for review",
			body,
			map[string]interface{}{
				"kind":        "artifact",
				"artifact_id": e.EntityID,
				"project_id":  e.ProjectID,
			})

	case domainevents.ChatterCreated:
		if payloadString(e, "kind") == "interview-completed" {
			n.fanOutToEditors(e, notifications.TypeInterviewCompleted,
				"Interview session finished",
				"An interview participant completed their session.",
				map[string]interface{}{
					"kind":       "interview",
					"session_id": e.EntityID,
					"project_id": e.ProjectID,
				})
			return
		}
		if payloadString(e, "entry_type") == "comment" {
			n.handleMentions(e)
		}
	}
}

// fanOutToEditors notifies every project member with editor rights or above,
// skipping the actor.
func (n *Notifier) fanOutToEditors(e domainevents.Event, ntype, title, body string, ref map[string]interface{}) {
	list, err := n.memberSvc.ListMembers(e.ProjectID)
	if err != nil {
		slog.Error("notify: failed to list project members",
			"event_type", e.EventType, "project_id", e.ProjectID, "error", err)
		return
	}
	for _, m := range list {
		if !members.RoleAtLeast(m.Role, members.RoleEditor) {
			continue
		}
		n.deliver(e, m.UserID, ntype, title, body, ref)
	}
}

// handleMentions scans a comment for @name tokens and notifies matched
// project members. The matching rules live in the mentions package, which
// the notes panel also uses to work out who a note is addressed to.
func (n *Notifier) handleMentions(e domainevents.Event) {
	message := payloadString(e, "message")
	tokens := mentions.Tokens(message)
	if len(tokens) == 0 {
		return
	}

	list, err := n.memberSvc.ListMembers(e.ProjectID)
	if err != nil {
		slog.Error("notify: failed to list project members for mentions",
			"project_id", e.ProjectID, "error", err)
		return
	}
	ref := map[string]interface{}{
		"kind":        "artifact",
		"artifact_id": payloadString(e, "artifact_id"),
		"chatter_id":  e.EntityID,
		"project_id":  e.ProjectID,
	}
	for _, m := range list {
		if !mentions.Matches(tokens, m) {
			continue
		}
		n.deliver(e, m.UserID, notifications.TypeMention,
			"You were mentioned in a comment",
			truncate(message, 160), ref)
	}
}

// deliver stores one notification and sends it on every channel
// (Delivery.Deliver) — unless the recipient is the event's own actor.
func (n *Notifier) deliver(e domainevents.Event, userID, ntype, title, body string, ref map[string]interface{}) {
	if e.Actor == "user:"+userID {
		return
	}
	notification := notifications.New(e.OrgID, userID, ntype, title, body, ref)
	if err := n.delivery.Deliver(notification); err != nil {
		slog.Error("notify: failed to store notification",
			"event_type", e.EventType, "user_id", userID, "error", err)
	}
}

func payloadString(e domainevents.Event, key string) string {
	v, ok := e.Payload[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
