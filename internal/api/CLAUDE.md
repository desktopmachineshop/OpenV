@README.md

Before you finish a change here, run:
- `go test -count=1 ./internal/api ./internal/archtest`
- after adding, removing or renaming a route: `cd frontend && npx vitest run src/arch/clientRoutes.test.ts`
- with a Postgres server, the tour: `OPENV_TEST_DATABASE_URL=<server URL> go test -count=1 -run '^TestTour' ./cmd/server`

Don't:
- register a route outside a `register<Area>Routes` registrar, or reorder `RegisterRoutes` (K1, I2)
- leave a helper two area files share in one of them; it moves to its K3 home (`respond.go`, `httperr.go`, `authz.go`)
- build a `Handler` literal in a test; use `newTestHandler` (K6)
- import `internal/persistence` (K7), or give a bare encode a `Content-Type` (quirk Q1)
- hand-edit `testdata/`; regenerate with the command the failing test prints, in a behavior-changing PR
