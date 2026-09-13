package notify

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
)

// Stable releases and the upgrade window (REQ-138, REQ-140).
//
// A stable release exists once the notes the binary was built with name it.
// For every stable-channel workspace the scheduler then does three things,
// each once (the release_schedule table holds the claims): tells the
// admins the release is cut and when it turns on for them, reminds them a
// day before, and at the turn-on time records the release on the workspace
// and tells every member with its notes. It runs at boot and on a timer.

// StableSteps is the persistence slice: one claim per step.
type StableSteps interface {
	ClaimStableStep(orgID, version, step string, at time.Time) (bool, error)
}

// StableOrgs is the orgs slice: the stable-channel workspaces, their
// members, and the write that turns a release on.
type StableOrgs interface {
	ListOrgsByChannel(channel string) ([]*orgs.Org, error)
	ListMembers(orgID string) ([]*orgs.Member, error)
	SetStableRelease(id, version string) (*orgs.Org, error)
}

// StableScheduler moves stable-channel workspaces to the newest stable.
type StableScheduler struct {
	releases    release.Service
	orgs        StableOrgs
	steps       StableSteps
	store       notifications.Service
	broadcaster Broadcaster
	email       *EmailDispatcher
	push        *PushDispatcher
	now         func() time.Time
}

// NewStableScheduler creates a scheduler. broadcaster may be nil.
func NewStableScheduler(releases release.Service, orgSvc StableOrgs, steps StableSteps, store notifications.Service, broadcaster Broadcaster) *StableScheduler {
	return &StableScheduler{releases: releases, orgs: orgSvc, steps: steps, store: store, broadcaster: broadcaster, now: time.Now}
}

// SetEmailDispatcher attaches the email side channel (nil leaves it off).
func (s *StableScheduler) SetEmailDispatcher(d *EmailDispatcher) *StableScheduler {
	s.email = d
	return s
}

// SetPushDispatcher attaches the web push side channel (nil leaves it off).
func (s *StableScheduler) SetPushDispatcher(d *PushDispatcher) *StableScheduler {
	s.push = d
	return s
}

// Start runs the scheduler now and then every interval until ctx ends.
func (s *StableScheduler) Start(ctx context.Context, interval time.Duration) {
	go func() {
		s.Run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.Run()
			}
		}
	}()
}

// reminderLead is how long before the turn-on the admins are reminded.
const reminderLead = 24 * time.Hour

// Run takes every step that is due for every stable-channel workspace.
func (s *StableScheduler) Run() {
	stable := s.releases.CurrentStable()
	if stable == nil {
		return
	}
	cutOn, err := time.Parse("2006-01-02", stable.CutOn)
	if err != nil {
		slog.Error("release: stable has no usable cut date", "version", stable.Version, "cut_on", stable.CutOn)
		return
	}
	list, err := s.orgs.ListOrgsByChannel(orgs.ChannelStable)
	if err != nil {
		slog.Error("release: failed to list stable-channel workspaces", "error", err)
		return
	}
	now := s.now()
	for _, org := range list {
		if !release.StableNewer(stable.Version, org.StableRelease) {
			continue
		}
		turnOn := orgs.UpgradeTimeFor(cutOn, org.UpgradeDay, org.UpgradeHour, org.UpgradeTimezone)
		s.announceCut(org, stable, turnOn, now)
		if now.Before(turnOn) {
			if !turnOn.Add(-reminderLead).After(now) && !isImmediate(org) {
				s.remind(org, stable, turnOn, now)
			}
			continue
		}
		s.turnOn(org, stable, now)
	}
}

// isImmediate reports whether a workspace has no window, so its releases
// turn on at the cut and a reminder would be noise.
func isImmediate(org *orgs.Org) bool { return org.UpgradeDay < 1 }

func (s *StableScheduler) announceCut(org *orgs.Org, stable *release.Stable, turnOn, now time.Time) {
	won, err := s.steps.ClaimStableStep(org.ID, stable.Version, "announced", now)
	if err != nil || !won {
		return
	}
	title := fmt.Sprintf("Stable release %s is ready for %s", stable.Version, org.Name)
	var body string
	if isImmediate(org) || !turnOn.After(now) {
		body = "It turns on for the workspace now. " + previewLine(stable)
	} else {
		body = fmt.Sprintf("It turns on for the workspace on %s. Change the upgrade window in workspace settings, or try it early for yourself. %s",
			formatWhen(turnOn, org.UpgradeTimezone), previewLine(stable))
	}
	s.notifyAdmins(org, notifications.TypeReleaseScheduled, title, body, stable.Version)
}

func (s *StableScheduler) remind(org *orgs.Org, stable *release.Stable, turnOn, now time.Time) {
	won, err := s.steps.ClaimStableStep(org.ID, stable.Version, "reminded", now)
	if err != nil || !won {
		return
	}
	title := fmt.Sprintf("Stable release %s turns on for %s tomorrow", stable.Version, org.Name)
	body := fmt.Sprintf("Scheduled for %s. %s", formatWhen(turnOn, org.UpgradeTimezone), previewLine(stable))
	s.notifyAdmins(org, notifications.TypeReleaseScheduled, title, body, stable.Version)
}

func (s *StableScheduler) turnOn(org *orgs.Org, stable *release.Stable, now time.Time) {
	won, err := s.steps.ClaimStableStep(org.ID, stable.Version, "turned_on", now)
	if err != nil || !won {
		return
	}
	if _, err := s.orgs.SetStableRelease(org.ID, stable.Version); err != nil {
		slog.Error("release: failed to turn a stable release on", "org_id", org.ID, "version", stable.Version, "error", err)
		return
	}
	members, err := s.orgs.ListMembers(org.ID)
	if err != nil {
		slog.Error("release: failed to list members for a stable release", "org_id", org.ID, "error", err)
		return
	}
	title, body := ReleaseMessage(stable.Version, stable.Notes)
	title = fmt.Sprintf("%s moved to stable release %s", org.Name, stable.Version)
	ref := map[string]interface{}{"kind": "release", "version": stable.Version, "org_id": org.ID}
	count := 0
	for _, m := range members {
		n := notifications.New(org.ID, m.UserID, notifications.TypeReleasePublished, title, body, ref)
		if err := s.store.Create(n); err != nil {
			slog.Error("release: failed to store notification", "user_id", m.UserID, "error", err)
			continue
		}
		count++
		if s.broadcaster != nil {
			s.broadcaster.BroadcastSession(StreamKey(m.UserID), "notification", n)
		}
		s.email.Dispatch(n)
		s.push.Dispatch(n)
	}
	slog.Info("release: stable turned on", "org_id", org.ID, "version", stable.Version, "members", count)
}

func (s *StableScheduler) notifyAdmins(org *orgs.Org, ntype, title, body, version string) {
	members, err := s.orgs.ListMembers(org.ID)
	if err != nil {
		slog.Error("release: failed to list admins", "org_id", org.ID, "error", err)
		return
	}
	ref := map[string]interface{}{"kind": "release", "version": version, "org_id": org.ID}
	for _, m := range members {
		if m.Role != orgs.RoleAdmin {
			continue
		}
		n := notifications.New(org.ID, m.UserID, ntype, title, body, ref)
		if err := s.store.Create(n); err != nil {
			slog.Error("release: failed to store notification", "user_id", m.UserID, "error", err)
			continue
		}
		if s.broadcaster != nil {
			s.broadcaster.BroadcastSession(StreamKey(m.UserID), "notification", n)
		}
		s.email.Dispatch(n)
		s.push.Dispatch(n)
	}
}

// previewLine is the one-line summary of what a stable release brings.
func previewLine(stable *release.Stable) string {
	changes := 0
	fixes := 0
	for _, n := range stable.Notes {
		if n.Fix {
			fixes++
		} else {
			changes++
		}
	}
	parts := []string{}
	if changes > 0 {
		parts = append(parts, fmt.Sprintf("%d change(s)", changes))
	}
	if fixes > 0 {
		parts = append(parts, fmt.Sprintf("%d fix(es)", fixes))
	}
	if len(parts) == 0 {
		return "See What's new for details."
	}
	return "It brings " + strings.Join(parts, " and ") + "; see What's new."
}

// formatWhen renders a time in the workspace's zone (UTC when none).
func formatWhen(t time.Time, timezone string) string {
	loc := time.UTC
	if timezone != "" {
		if l, err := time.LoadLocation(timezone); err == nil {
			loc = l
		}
	}
	return t.In(loc).Format("Mon 2 Jan 2006 at 15:04 MST")
}
