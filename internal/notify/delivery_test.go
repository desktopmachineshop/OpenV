package notify

// Unit tests of Delivery, Channels and ToOrgAdmins (refactor plan step X6).
// The seven producers that call them are pinned end to end by S10
// (notification_content_test.go), which X6 leaves as it was; these tests
// reuse its recording channels.

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
)

// deliveryChannels is S10's recording wiring with run_failed emailed and
// pushed.
func deliveryChannels(t *testing.T) *ncChannels {
	t.Helper()
	for _, k := range ncEnvKeys {
		t.Setenv(k, notifications.TypeRunFailed)
	}
	return newNCChannels(t)
}

func deliveryRow(userID string) *notifications.Notification {
	return notifications.New("org-acme", userID, notifications.TypeRunFailed, "Agent run failed",
		"An agent run you launched finished with an error.",
		map[string]interface{}{"kind": "run", "run_id": "run-1", "project_id": "proj-apollo"})
}

func TestDeliverStoresThenSendsSSEThenEmailThenPush(t *testing.T) {
	ch := deliveryChannels(t)
	d := Delivery{Store: ch.store, Broadcaster: ch.bc, Channels: Channels{Email: ch.email, Push: ch.push}}
	n := deliveryRow("user-ada")
	if err := d.Deliver(n); err != nil {
		t.Fatalf("Deliver = %v, want nil", err)
	}
	deliveries, strays := ch.rec.take()
	if len(strays) > 0 {
		t.Errorf("sent before the row was stored: %v", strays)
	}
	if len(deliveries) != 1 {
		t.Fatalf("%d rows stored, want 1", len(deliveries))
	}
	got := deliveries[0]
	if got.row.ID != n.ID {
		t.Errorf("stored row %s, want %s", got.row.ID, n.ID)
	}
	if want := []string{"sse", "email", "push"}; !reflect.DeepEqual(got.seq, want) {
		t.Errorf("channels in the order %v, want %v", got.seq, want)
	}
	for _, p := range ncAddressProblems(got) {
		t.Error(p)
	}
	want, _ := json.Marshal(n)
	if len(got.frames) != 1 || string(got.frames[0].data) != string(want) {
		t.Errorf("SSE frames %+v, want one carrying the row %s", got.frames, want)
	}
}

func TestDeliverSendsNothingWhenTheStoreRefuses(t *testing.T) {
	ch := deliveryChannels(t)
	ch.store.refuse = map[string]bool{"user-ada": true}
	d := Delivery{Store: ch.store, Broadcaster: ch.bc, Channels: Channels{Email: ch.email, Push: ch.push}}
	if err := d.Deliver(deliveryRow("user-ada")); !errors.Is(err, ncRefused) {
		t.Fatalf("Deliver = %v, want the store's error %v", err, ncRefused)
	}
	deliveries, strays := ch.rec.take()
	if len(deliveries) != 0 || len(strays) != 0 {
		t.Errorf("a refused row was followed by %d rows and %v; want nothing sent", len(deliveries), strays)
	}
}

func TestDeliverLeavesOffWhatIsNotWired(t *testing.T) {
	ch := deliveryChannels(t)

	// No broadcaster: the email and the push still go.
	noSSE := Delivery{Store: ch.store, Channels: Channels{Email: ch.email, Push: ch.push}}
	if err := noSSE.Deliver(deliveryRow("user-ada")); err != nil {
		t.Fatal(err)
	}
	// No channels: the row and the frame only.
	noChannels := Delivery{Store: ch.store, Broadcaster: ch.bc}
	if err := noChannels.Deliver(deliveryRow("user-ben")); err != nil {
		t.Fatal(err)
	}
	deliveries, _ := ch.rec.take()
	if len(deliveries) != 2 {
		t.Fatalf("%d rows stored, want 2", len(deliveries))
	}
	if got := deliveries[0].seq; !reflect.DeepEqual(got, []string{"email", "push"}) {
		t.Errorf("without a broadcaster the channels were %v, want [email push]", got)
	}
	if got := deliveries[1].seq; !reflect.DeepEqual(got, []string{"sse"}) {
		t.Errorf("without email or push the channels were %v, want [sse]", got)
	}
}

// TestSetChannelsAttachesBothChannels: each producer's SetChannels sets the
// two side channels SetEmailDispatcher and SetPushDispatcher set one at a
// time, and a nil one is off.
func TestSetChannelsAttachesBothChannels(t *testing.T) {
	c := Channels{Email: &EmailDispatcher{}, Push: &PushDispatcher{}}
	email, push := c.Email, c.Push
	cases := []struct {
		name               string
		together, oneByOne Channels
		emptied            Channels
	}{
		{"Notifier",
			NewNotifier(nil, nil, nil).SetChannels(c).delivery.Channels,
			NewNotifier(nil, nil, nil).SetEmailDispatcher(email).SetPushDispatcher(push).delivery.Channels,
			NewNotifier(nil, nil, nil).SetChannels(c).SetChannels(Channels{}).delivery.Channels},
		{"BudgetMonitor",
			NewBudgetMonitor(nil, nil, nil, nil).SetChannels(c).delivery.Channels,
			NewBudgetMonitor(nil, nil, nil, nil).SetEmailDispatcher(email).SetPushDispatcher(push).delivery.Channels,
			NewBudgetMonitor(nil, nil, nil, nil).SetChannels(c).SetChannels(Channels{}).delivery.Channels},
		{"MinutesMonitor",
			NewMinutesMonitor(nil, nil, nil, nil).SetChannels(c).delivery.Channels,
			NewMinutesMonitor(nil, nil, nil, nil).SetEmailDispatcher(email).SetPushDispatcher(push).delivery.Channels,
			NewMinutesMonitor(nil, nil, nil, nil).SetChannels(c).SetChannels(Channels{}).delivery.Channels},
		{"ReleaseAnnouncer",
			NewReleaseAnnouncer(nil, nil, nil, nil).SetChannels(c).delivery.Channels,
			NewReleaseAnnouncer(nil, nil, nil, nil).SetEmailDispatcher(email).SetPushDispatcher(push).delivery.Channels,
			NewReleaseAnnouncer(nil, nil, nil, nil).SetChannels(c).SetChannels(Channels{}).delivery.Channels},
		{"StableScheduler",
			NewStableScheduler(nil, nil, nil, nil, nil).SetChannels(c).delivery.Channels,
			NewStableScheduler(nil, nil, nil, nil, nil).SetEmailDispatcher(email).SetPushDispatcher(push).delivery.Channels,
			NewStableScheduler(nil, nil, nil, nil, nil).SetChannels(c).SetChannels(Channels{}).delivery.Channels},
		{"SupportWindowWatcher",
			NewSupportWindowWatcher("", nil, nil, nil, nil, nil).SetChannels(c).delivery.Channels,
			NewSupportWindowWatcher("", nil, nil, nil, nil, nil).SetEmailDispatcher(email).SetPushDispatcher(push).delivery.Channels,
			NewSupportWindowWatcher("", nil, nil, nil, nil, nil).SetChannels(c).SetChannels(Channels{}).delivery.Channels},
	}
	for _, tc := range cases {
		if tc.together.Email != email || tc.together.Push != push {
			t.Errorf("%s.SetChannels attached %+v, want %+v", tc.name, tc.together, c)
		}
		if tc.oneByOne != tc.together {
			t.Errorf("%s: the two setters attach %+v, SetChannels %+v", tc.name, tc.oneByOne, tc.together)
		}
		if tc.emptied != (Channels{}) {
			t.Errorf("%s.SetChannels(Channels{}) left %+v, want both off", tc.name, tc.emptied)
		}
	}
}

func TestToOrgAdminsCallsEachAdminInListOrder(t *testing.T) {
	members := []*orgs.Member{
		{UserID: "user-ada", Role: orgs.RoleAdmin},
		{UserID: "user-ben", Role: orgs.RoleMember},
		{UserID: "user-cy", Role: ""},
		{UserID: "user-dee", Role: orgs.RoleAdmin},
		{UserID: "user-eve", Role: "Admin"},
	}
	var got []string
	ToOrgAdmins(members, func(userID string) { got = append(got, userID) })
	if want := []string{"user-ada", "user-dee"}; !reflect.DeepEqual(got, want) {
		t.Errorf("delivered to %v, want %v", got, want)
	}
	ToOrgAdmins(nil, func(userID string) { t.Errorf("delivered to %s from an empty list", userID) })
}
