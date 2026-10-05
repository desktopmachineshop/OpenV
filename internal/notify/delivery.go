package notify

import (
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// Channels are the side channels a stored notification also leaves by:
// email (issue #187) and web push (REQ-109). Either may be nil, which leaves
// that channel off; both dispatchers are nil-safe. cmd/server builds one
// value and hands it to every producer through its SetChannels.
type Channels struct {
	Email *EmailDispatcher
	Push  *PushDispatcher
}

// Delivery is how every producer in this package hands a notification to
// its recipient: the notifier, the budget and hosted-minutes monitors, the
// release announcer, the stable scheduler and the support-window watcher.
type Delivery struct {
	Store notifications.Service
	// Broadcaster may be nil (store-only mode, used in tests).
	Broadcaster Broadcaster
	Channels
}

// Deliver stores n, then sends it on the recipient's SSE stream, then by
// email, then by web push, in that order. When the store refuses the row it
// sends nothing and returns the store's error, for the caller to log as it
// does; every channel after the store is best effort and reports nothing.
//
// The email is sent on the caller's goroutine (the bus dispatch goroutine,
// for the notifier and the budget monitor): a no-op unless SMTP is
// configured, the type is eligible and the recipient is opted in. The push
// has the same gates, needs a subscribed device too, and is queued to the
// push dispatcher's workers, so it does not wait on a push service.
func (d Delivery) Deliver(n *notifications.Notification) error {
	if err := d.Store.Create(n); err != nil {
		return err
	}
	if d.Broadcaster != nil {
		d.Broadcaster.BroadcastSession(StreamKey(n.UserID), "notification", n)
	}
	d.Email.Dispatch(n)
	d.Push.Dispatch(n)
	return nil
}

// ToOrgAdmins is the one fan-out to a workspace's admins: it calls deliver
// with the user id of each admin in members, in list order, and skips every
// other member. Listing the members, and what a failed list logs, stay with
// the caller.
func ToOrgAdmins(members []*orgs.Member, deliver func(userID string)) {
	for _, m := range members {
		if m.Role != orgs.RoleAdmin {
			continue
		}
		deliver(m.UserID)
	}
}
