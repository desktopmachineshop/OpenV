@README.md

Before you finish a change here or in a service README.md covers, run:
- `go test -count=1 ./internal/notify ./internal/events ./internal/scheduler ./internal/automation ./internal/orchestration ./internal/billing/... ./internal/domain/notifications`
- after a change to a notification or its link: `cd frontend && npx vitest run src/components/NotificationBell.paths.test.tsx`
- `go test -count=1 -run '^TestBootSteps$' ./cmd/server` (where each service starts and subscribes)

Don't:
- reorder bus subscribers or move a service's start (I10, I17); there is no generic job runner
- change a notification's text, recipients or deep link without regenerating the S10 goldens (I18)
- give the bell and the email or push link different pages: change `notificationPath` and `pathForNotification` together (Q7)
- turn an event payload map into a struct: subscribers read its Go value types (I10)
- hand-edit `testdata/`; regenerate with the command the failing test prints
