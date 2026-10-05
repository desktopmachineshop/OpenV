# internal/notify and the background services

What runs beside the request path. `internal/notify` turns domain events
and schedules into notifications, emails and web pushes. This README also
covers the other background services `cmd/server` starts: the event bus
(`internal/events`), the scheduler (`internal/scheduler`), the trigger
matcher (`internal/automation`), the run hooks (`internal/orchestration`)
and billing (`internal/billing`). The purge loop and the reaper live in
`cmd/server/jobs.go` (`cmd/server/README.md`). Plan §7.6
(`docs/plans/codebase-refactor.md`) has the history.

## Areas

| Area (`docs/areas.json`) | Globs |
|---|---|
| events-notifications | `internal/notify/**` (this file too), `internal/events/**`, `cmd/openv-vapid/**` |
| agent-suite | `internal/scheduler/**`, `internal/automation/**`, `internal/orchestration/**` |
| billing | `internal/billing/**` |

The notification and event types themselves are domain packages
(`internal/domain/README.md`).

## Map

| Glob | What it holds |
|---|---|
| `notifier.go`, `membership.go` | `Notifier`: subscribes to the bus and turns each domain event into notifications for the right members |
| `budgets.go`, `minutes.go` | `BudgetMonitor` (a bus subscriber) and `MinutesMonitor`: budget and cloud-runner minutes alerts |
| `release.go`, `stable.go`, `dedicated.go` | `ReleaseAnnouncer`; `StableScheduler`, stable releases and the upgrade window; `SupportWindowWatcher`, a dedicated instance's support window |
| `delivery.go` | `Delivery.Deliver`, the one store → SSE → email → push every producer above calls (email synchronous on the caller's goroutine, push queued); `Channels{Email, Push}`, which `cmd/server` builds once and hands to each producer's `SetChannels`; `ToOrgAdmins`, the one fan-out to a workspace's admins |
| `email.go`, `push.go` | the side channels: `EmailDispatcher` with `notificationPath`, each notification's deep link; `PushDispatcher` and `WebPushSender` |
| `invitation.go`, `verification.go`, `credentials.go` | the invitation and sign-up verification mails; the SMTP sender |
| `notification_content*_test.go`, `testdata/notifications/*/` | S10: four goldens per notification type (`row.json`, `sse.txt`, `email.txt`, `push.json`) |
| `internal/events/bus.go` | `DefaultBus`: stores each event, then calls every subscriber in subscription order on one dispatch goroutine |
| `internal/scheduler/scheduler.go` | `Scheduler`: due automations; catch-up inside `Start`, and a claim before each run |
| `internal/automation/triggers.go` | `TriggerMatcher`: launches the automations whose event filter matches |
| `internal/orchestration/hooks.go` | `Hooks`: run status to the board, crew follow-ups and hand-offs, interview sessions |
| `internal/billing/**` | the subscription service, its Stripe client (`internal/billing/stripe/`), the price catalogue, checkout, seats and the reconcile loop |
| `cmd/openv-vapid/**` | prints a VAPID key pair for web push (`make vapid-keys`) |

## Invariants (plan §3) that bind here

- **I10 subscriber order** on the bus: orchestration hooks, the notifier,
  the budget monitor, the trigger matcher, as `cmd/server`'s stages
  subscribe them. Payloads are maps whose Go value types
  `membership.go` and the trigger filters read.
- **I17 timing.** `StableScheduler`, `SupportWindowWatcher` and the billing
  reconcile run once at start, then on their interval; the scheduler's
  catch-up finishes before `Start` returns, so before the server listens.
  There is no generic job runner (plan §7.6).
- **I18 content.** Each notification's title, body, recipients and order,
  the email template and footer, the push payload, and the deep link,
  which opens the same page from the bell, the email and the push (quirk Q7,
  resolved).
- **I13.** `OPENV_EMAIL_NOTIFICATION_TYPES` and
  `OPENV_PUSH_NOTIFICATION_TYPES` override the default lists; a credential
  (SMTP, Stripe, VAPID) is used exactly as set. Billing's counts are whole
  numbers or the boot refuses (trial days may be 0).
- **Q11.** `NewHandler` rewires billing after `billing.Start` has started
  its goroutines; the order stays.

## Recipes

**Add a notification type.**
1. Add the type in `internal/domain/notifications/notifications.go` and
   create it from the notifier or the monitor that sees the cause.
2. Give it a deep link on both sides, `notificationPath` (`email.go`) and
   the bell's `pathForNotification`
   (`frontend/src/components/NotificationBellPaths.ts`), and a case in
   `testdata/deep_links.json`, which both sides' tests read
   (`TestEmailAndPushLinkWhereTheBellOpens` and `NotificationBell.test.tsx`).
3. Add a scenario to `ncScenarios` (`notification_content_test.go`), then
   regenerate the Go goldens and, after them, the bell's:
   `UPDATE_GOLDEN=1 go test ./internal/notify -count=1 -run '^TestNotificationContent$'`
   and `cd frontend && npx vitest run src/components/NotificationBell.paths.test.tsx -u`.
   Decide whether it emails or pushes by default (`DefaultEmailTypes`).

**Subscribe to the event bus.** Subscribe in the `cmd/server` stage that
owns the subscriber, after today's subscribers (I10), and regenerate
`cmd/server/testdata/boot_steps.txt` (`cmd/server/README.md`). Subscribers
run one after another on the bus's one dispatch goroutine, so a slow one
delays every later one.

**Change the scheduler, the trigger matcher or run-now.** S11 pins their
behavior with no golden: change the expectation in
`internal/scheduler/scheduler_test.go`, `internal/automation/matcher_test.go`
or `internal/api/automation_run_now_test.go` in the same pull request, and
keep the three launch paths' differences unless the change means to remove
one.

**Change billing.** The service talks to Stripe through the `Provider`
interface (`internal/billing/billing.go`), which its tests fake; the Stripe
client is tested against `httptest`. The billing boot profile and
`TestTourS5cBilling` pin what the server answers (`cmd/server/README.md`).

## Guards

Each line: the guard, what it pins, and the command that runs it.

- **S10** `TestNotificationContent` and `TestNotificationStoreFailure`:
  `testdata/notifications/` and each delivery path's handling of a refused
  row (I18). `go test ./internal/notify -count=1`
- **S10** `TestEveryNotificationTypeHasAContentGolden`: every type has its
  goldens. `go test ./internal/domain/notifications -count=1`
- **S10, Q7** `NotificationBell.paths.test.tsx`: the bell's link beside the
  Go link for each golden, in
  `frontend/src/components/__snapshots__/NotificationBell.paths.txt`.
  `cd frontend && npx vitest run src/components/NotificationBell.paths.test.tsx`
- **S11** the scheduler, matcher and run-now characterizations:
  `go test -count=1 ./internal/scheduler ./internal/automation` and
  `go test ./internal/api -count=1 -run '^TestRunNowCopy$'`; with a
  database, `TestSchedulersShareTheRealClaim`
  (`internal/persistence/postgres/README.md`).
- **S8** `TestEnvParse` for the getters here.
  `go test -count=1 -run '^TestEnvParse$' ./internal/notify ./internal/billing`
- **S4** `cmd/server/testdata/boot_steps.txt`: the subscriber order and
  where each service starts; see `cmd/server/README.md`.

All of it: `go test -count=1 ./internal/notify ./internal/events ./internal/scheduler ./internal/automation ./internal/orchestration ./internal/billing/...`.
