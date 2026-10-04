@README.md

Before you finish a change here, run:
- `go test -count=1 ./internal/persistence/postgres ./internal/archtest` (layout tests; the rest skip without a database)
- with a Postgres server (pgvector for the full schema): `OPENV_TEST_DATABASE_URL=<server URL> go test -count=1 ./internal/persistence/postgres`
- after a migration: the S3 regenerate command in README.md, and commit the goldens it writes

Don't:
- edit, reorder or renumber a migration that has shipped, or add DDL to `InitSchema` or `schema_*.go` (I16)
- write a migration as a literal in `migrations.go`: one `migration_00NN_<name>.go` and one registry line (K9)
- add an `init()` function, or import a package of this module outside `internal/domain` (K7)
- leave a new workspace-scoped table out of the purge (Q17)
- hand-edit `testdata/`; regenerate with the command the failing test prints
