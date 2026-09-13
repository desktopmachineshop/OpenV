package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/orgs"
	"github.com/openv/requirements-platform/internal/domain/release"
)

// Dedicated instances and the support window (REQ-139).
//
// A dedicated instance runs the stable release its customer chose and is
// supported for SupportWindowDays after the next stable is designated. It learns
// about newer stables from the shared service's public release feed
// (GET /api/v1/public/release) and warns every workspace's admins when the
// window is about to close, and once more when it has closed. Each warning
// is claimed in release_announcements so a restart never repeats it.

// SupportWindowDays is how long a dedicated instance is supported after
// the next stable release is cut.
const SupportWindowDays = 90

// warningDays are the days-before-close at which admins are warned,
// nearest first so the tightest threshold that applies is the one claimed.
var warningDays = []int{7, 30}

// ReleaseFeed is the shared service's public answer.
type ReleaseFeed struct {
	// Version is the release the shared service runs (its newest nightly).
	Version string `json:"version"`
	Stable  string `json:"stable"`
	// StableSince is the day the stable release was designated (YYYY-MM-DD).
	StableSince string `json:"stable_since"`
}

// WindowOrgs is the orgs slice: every workspace and its members.
type WindowOrgs interface {
	ListAll() ([]string, error)
	ListMembers(orgID string) ([]*orgs.Member, error)
}

// SupportWindowWatcher polls the feed and warns admins.
type SupportWindowWatcher struct {
	feedURL     string
	client      *http.Client
	releases    release.Service
	orgs        WindowOrgs
	claims      ReleaseClaimer
	store       notifications.Service
	broadcaster Broadcaster
	email       *EmailDispatcher
	push        *PushDispatcher
	now         func() time.Time
}

// NewSupportWindowWatcher creates a watcher for a dedicated instance.
func NewSupportWindowWatcher(feedURL string, releases release.Service, orgSvc WindowOrgs, claims ReleaseClaimer, store notifications.Service, broadcaster Broadcaster) *SupportWindowWatcher {
	return &SupportWindowWatcher{
		feedURL:     feedURL,
		client:      &http.Client{Timeout: 20 * time.Second},
		releases:    releases,
		orgs:        orgSvc,
		claims:      claims,
		store:       store,
		broadcaster: broadcaster,
		now:         time.Now,
	}
}

// SetEmailDispatcher attaches the email side channel (nil leaves it off).
func (w *SupportWindowWatcher) SetEmailDispatcher(d *EmailDispatcher) *SupportWindowWatcher {
	w.email = d
	return w
}

// SetPushDispatcher attaches the web push side channel (nil leaves it off).
func (w *SupportWindowWatcher) SetPushDispatcher(d *PushDispatcher) *SupportWindowWatcher {
	w.push = d
	return w
}

// Start checks now and then every interval until ctx ends.
func (w *SupportWindowWatcher) Start(ctx context.Context, interval time.Duration) {
	go func() {
		w.Run()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.Run()
			}
		}
	}()
}

// Fetch reads the feed.
func (w *SupportWindowWatcher) Fetch() (*ReleaseFeed, error) {
	resp, err := w.client.Get(w.feedURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release feed answered %d", resp.StatusCode)
	}
	var feed ReleaseFeed
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, err
	}
	return &feed, nil
}

// Run compares the running stable with the feed's and warns as needed.
func (w *SupportWindowWatcher) Run() {
	own := w.releases.CurrentStable()
	if own == nil {
		slog.Info("release: dedicated instance runs no stable release; support window not tracked")
		return
	}
	feed, err := w.Fetch()
	if err != nil {
		slog.Warn("release: could not read the release feed", "url", w.feedURL, "error", err)
		return
	}
	if feed.Stable == "" || !release.Newer(feed.Stable, own.Version) {
		return
	}
	cutOn, err := time.Parse("2006-01-02", feed.StableSince)
	if err != nil {
		slog.Warn("release: feed stable has no designation date", "stable", feed.Stable)
		return
	}
	closes := cutOn.Add(SupportWindowDays * 24 * time.Hour)
	left := int(closes.Sub(w.now()).Hours() / 24)
	for _, days := range warningDays {
		if left <= days && left >= 0 {
			w.warn(feed, own.Version, closes, fmt.Sprintf("support-window:%s:%d", feed.Stable, days),
				fmt.Sprintf("Upgrade OpenV within %d days", left),
				fmt.Sprintf("This instance runs stable release %s. Stable release %s is available, and support for %s ends on %s. Preview the new release on your staging copy, then schedule the upgrade.",
					own.Version, feed.Stable, own.Version, closes.Format("2 Jan 2006")))
			break
		}
	}
	if left < 0 {
		w.warn(feed, own.Version, closes, "support-window:"+feed.Stable+":closed",
			"OpenV support window has closed",
			fmt.Sprintf("This instance runs stable release %s, whose support ended on %s. Stable release %s is available; upgrade to keep receiving fixes.",
				own.Version, closes.Format("2 Jan 2006"), feed.Stable))
	}
}

func (w *SupportWindowWatcher) warn(feed *ReleaseFeed, own string, closes time.Time, key, title, body string) {
	won, err := w.claims.ClaimReleaseAnnouncement(key, w.now())
	if err != nil || !won {
		return
	}
	ids, err := w.orgs.ListAll()
	if err != nil {
		slog.Error("release: failed to list workspaces for the support warning", "error", err)
		return
	}
	ref := map[string]interface{}{"kind": "support_window", "running": own, "available": feed.Stable, "closes": closes.Format("2006-01-02")}
	for _, orgID := range ids {
		members, err := w.orgs.ListMembers(orgID)
		if err != nil {
			continue
		}
		for _, m := range members {
			if m.Role != orgs.RoleAdmin {
				continue
			}
			n := notifications.New(orgID, m.UserID, notifications.TypeReleaseSupportWindow, title, body, ref)
			if err := w.store.Create(n); err != nil {
				continue
			}
			if w.broadcaster != nil {
				w.broadcaster.BroadcastSession(StreamKey(m.UserID), "notification", n)
			}
			w.email.Dispatch(n)
			w.push.Dispatch(n)
		}
	}
	slog.Warn("release: support window warning sent", "running", own, "available", feed.Stable, "closes", closes.Format("2006-01-02"))
}
