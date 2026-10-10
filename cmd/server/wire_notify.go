package main

import (
	"log/slog"
	"time"

	openv "github.com/openv/requirements-platform"
	"github.com/openv/requirements-platform/internal/domain/notifications"
	"github.com/openv/requirements-platform/internal/domain/pushsubs"
	"github.com/openv/requirements-platform/internal/domain/release"
	"github.com/openv/requirements-platform/internal/notify"
	"github.com/openv/requirements-platform/internal/persistence/postgres"
)

// notify builds the mail and web-push side channels and the session and
// registration policies, and starts the notifier, the budget monitor and the
// hosted-minutes alerts.
func (a *app) notify() {
	cfg := a.env()
	// Optional email side channel for high-signal notifications (issue #187).
	// Strictly opt-in: with OPENV_SMTP_HOST unset the mailer is a no-op, so
	// in-app + SSE delivery (and dev/compose) are unaffected. Deep links point
	// at the frontend (FRONTEND_URL), falling back to PUBLIC_URL.
	smtp := cfg.SMTP()
	a.emailMailer = notify.NewMailer(notify.SMTPSettings{
		Host: smtp.Host, Port: smtp.Port, User: smtp.User, Password: smtp.Password, From: smtp.From,
	})
	// Links in mail point at the SPA, which the API itself never serves, so
	// the last fallback is the dev frontend, not this process.
	a.emailLinkBase = cfg.EmailLinkBase()
	// Sign-up email verification (SEC-15 / REQ-95): enforced only when the
	// mailer can send and the operator has not switched it off, so a stack
	// with no SMTP is unchanged. The policy reaches the user service (new
	// accounts start unverified), the handler (sends the link) and the auth
	// middleware (walls unverified sessions).
	a.emailVerification = cfg.EmailVerification(a.emailMailer)
	a.userService.SetEmailVerificationPolicy(a.emailVerification)
	// Session lifetime (REQ-99): an absolute deadline and an idle one, both
	// operator-shortenable, neither extendable past the defaults.
	a.sessionPolicy = cfg.SessionPolicy()
	a.userService.SetSessionPolicy(a.sessionPolicy)
	// Registration policy (REQ-95): open unless the operator closes it.
	a.registrationPolicy = cfg.Registration()
	// The email and web push side channels, one value handed to every
	// notification producer below and in stage release (SetChannels).
	a.notifyChannels.Email = notify.NewEmailDispatcher(a.emailMailer, a.userService, a.emailLinkBase, cfg.EmailTypes())

	// Optional web push side channel for the same high-signal types (REQ-109).
	// Also strictly opt-in: with no OPENV_VAPID_* key pair the dispatcher has
	// no sender, /api/v1/me/push/config reports enabled=false and nothing is
	// ever sent. Deep links use the same frontend base as the emails.
	a.vapid = cfg.VAPID()
	a.pushSubService = pushsubs.NewDefaultService(a.pushSubRepo)
	var pushSender notify.PushSender
	if a.vapid.Enabled() {
		// An explicit client: webpush-go's fallback is a bare http.Client
		// with no timeout, which would let a push service that stops
		// answering hold a dispatcher worker indefinitely.
		pushSender = notify.NewWebPushSender(a.vapid, notify.DefaultPushHTTPClient())
	}
	// Push deep links are same-origin paths resolved by the service worker,
	// so unlike the emails above the dispatcher needs no base URL.
	a.notifyChannels.Push = notify.NewPushDispatcher(pushSender, a.pushSubService, a.userService, cfg.PushTypes())

	// Notification fan-out: bus events become per-user inbox rows plus live
	// SSE pushes on notify:<user_id> (issue #132), plus a best-effort email
	// for eligible types when the recipient is opted in and SMTP is on (#187).
	a.notificationService = notifications.NewDefaultService(a.notificationRepo)
	notify.NewNotifier(a.notificationService, a.memberService, a.sseHub).
		SetChannels(a.notifyChannels).
		// Membership and privilege changes: the affected member hears what
		// changed about their own access, and the workspace's admins hear who
		// joined and who left.
		SetOrgService(a.orgService).
		SetUserNamer(notify.UserNamerFunc(func(userID string) string {
			user, err := a.userService.GetByID(userID)
			if err != nil || user == nil {
				return ""
			}
			return user.Name
		})).
		Start(a.bus)

	// Workspace budget alerts (issue #186): a finishing run's cost can push
	// month-to-date spend across 80%/100% of the org's monthly budget; the
	// monitor alerts org admins once per threshold per month. Warn-only.
	notify.NewBudgetMonitor(a.orgService, a.runService, a.notificationService, a.sseHub).
		SetChannels(a.notifyChannels).
		Start(a.bus)

	// Hosted-minutes alerts: the lease handlers ask the monitor after every
	// lease starts or extends; nil where there is no runner pool to lease
	// from, and the handlers are nil-safe.
	if a.runnerSessionService != nil {
		a.minutesMonitor = notify.NewMinutesMonitor(a.orgService, a.runnerSessionService, a.notificationService, a.sseHub).
			SetChannels(a.notifyChannels)
	}
}

// release reads the running release and, when there is one, announces it
// and starts the stable-channel scheduler and the support-window watcher.
func (a *app) release() {
	cfg := a.env()
	// The running release: RELEASE_NOTES.md as built into this binary. Its
	// top dated section is what GET /api/v1/release reports and what every
	// account is told about, once per release, when a server first boots on
	// it. A notes file that fails to parse is logged and serves an empty
	// release rather than keeping the API down over documentation.
	a.deploymentKind = cfg.Deployment()
	releaseFeedURL := cfg.ReleaseFeedURL()
	// The commit this binary was built from, reported by /health so a running
	// deployment can be matched to a revision (REQ-141). Railway injects
	// RAILWAY_GIT_COMMIT_SHA; OPENV_BUILD_SHA overrides it for platforms that
	// do not, and both being unset simply leaves the commit out of /health.
	a.buildSHA = cfg.BuildSHA()
	var err error
	a.releaseService, err = release.NewService(openv.ReleaseNotesMarkdown)
	if err != nil {
		slog.Error("release notes failed to parse; serving no release", "error", err)
		a.releaseService = release.Empty()
	}
	if cur := a.releaseService.Current(); cur != nil {
		slog.Info("release", "version", cur.Version)
		releaseRepo := postgres.NewReleaseRepository(a.db)
		announcer := notify.NewReleaseAnnouncer(releaseRepo, a.orgService, a.notificationService, a.sseHub).
			SetChannels(a.notifyChannels)
		go announcer.Announce(cur)
		// Stable-channel workspaces move to a stable release at their own
		// upgrade time: the scheduler tells their admins at the cut, reminds
		// them a day before, and turns the release on (REQ-138, REQ-140).
		notify.NewStableScheduler(a.releaseService, a.orgService, releaseRepo, a.notificationService, a.sseHub).
			SetChannels(a.notifyChannels).
			Start(a.ctx, time.Hour)
		// A dedicated instance (OPENV_DEPLOYMENT=dedicated) is supported for
		// 90 days after the next stable is cut on the shared service; it
		// reads the public release feed daily and warns admins (REQ-139).
		if a.deploymentKind == "dedicated" {
			notify.NewSupportWindowWatcher(releaseFeedURL, a.releaseService, a.orgService, releaseRepo, a.notificationService, a.sseHub).
				SetChannels(a.notifyChannels).
				Start(a.ctx, 24*time.Hour)
		}
	}
}
