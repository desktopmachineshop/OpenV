@README.md

Before you finish a change here, run:
- `go test -count=1 ./cmd/server ./internal/archtest` (boot harness and tour skip without a database)
- `go run ./internal/tools/movecheck -flatten main ./cmd/server` (stages still inline into `main()`)
- with a Postgres server: `OPENV_TEST_DATABASE_URL=<server URL> go test -count=1 ./cmd/server`

Don't:
- add, regroup or reorder stages; new wiring joins the stage that owns its concern (I17, R9)
- put a `defer` in a stage, or move a fatal check across `storage`'s migration (I13)
- read an env var except through the getters in `config.go` (K8)
- hand-edit `testdata/`; regenerate with the command the failing test prints
