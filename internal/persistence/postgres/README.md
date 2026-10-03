# internal/persistence/postgres: storage

The Postgres side of every domain `Repository` interface, the schema and its
migration ledger. It imports only `internal/domain` packages (K7);
`cmd/server/wire_storage.go` opens the connection, migrates, and builds every
repository. Plan §7.4 (`docs/plans/codebase-refactor.md`) has the history;
`docs/DEVELOPMENT.md` ("Adding a schema migration", "Tests and CI") the
long form. `go run ./internal/tools/areas which <path>` names a file's area.

## Areas

| Area (`docs/areas.json`) | Globs here |
|---|---|
| requirements-core | `artifact_*.go`, `attribute_*.go`, `chatter_*.go`, `embedding_*.go`, `link_*.go`, `product_profile_*.go`, `project_*.go`, `settings_*.go`, `workitem_*.go` |
| verification | `evidence_*.go`, `vv_*.go` |
| documents | `attachment_*.go`, `baseline_*.go`, `template_*.go` |
| tenancy-identity | `invitation_*.go`, `member_*.go`, `org_*.go`, `user_*.go` |
| agent-suite | `agent_*.go`, `automation_*.go`, `guided_*.go`, `interview_*.go`, `proposal_*.go`, `provider_*.go`, `repo_connection_*.go`, `team_*.go` |
| runner-fleet | `hosted_worker_*.go`, `runner_session_*.go`, `worker_key_*.go` |
| events-notifications | `event_*.go`, `notification_*.go`, `push_subscription_*.go` |
| community | `shared_product_*.go`, `sharelink_*.go` |
| platform-http | `connstring*.go`, `db*.go`, `ids*.go`, `migrat*.go`, `release_*.go`, `schema_*.go`, `timestamptz*.go`, `testdata/**`, `*.md` |

## Map

| Glob | What it holds |
|---|---|
| `*_repository*.go` | one repository per domain `Repository` interface, built by `New<Name>Repository(db)`; a large one is split by concern (`org_repository_*.go`) |
| `db.go`, `connstring.go` | `Connect` and the connection string (`DATABASE_URL`, else the quoted `DB_*` parts) |
| `schema_*.go` and `InitSchema` in `db.go` | migration 0001, the baseline re-run on every boot; frozen |
| `migrations.go` | the `Migration` type and `migrations`, the explicit, ordered registry: one line per version |
| `migration_00*_*.go` | one numbered migration each: `func m00NN<Name>(tx *sql.Tx) error` |
| `migrate_runner.go`, `migration_helpers.go` | `Migrate`, `MigrateAndBackfill`, the ledger, the boot lock and the extension reconcile; helpers the migrations call |
| `org_backfill.go` | `BackfillOrgs`, the idempotent boot-time data step outside the ledger |
| `migration_freeze*_test.go`, `migration_files_test.go`, `migrations_test.go` | S3's freeze, schema and purge goldens; M10's one-file-per-migration layout; the runner |
| `*_roundtrip_test.go`, `repository_roundtrip_helpers_test.go` | S15b round trips of five repositories, and S9's `docs/exports` round trip |
| `testdata/freeze/*.txt`, `testdata/schema/*.txt`, `testdata/purge/*.txt` | S3 goldens: migration and every-boot hashes, the schema after both boot paths, the purge catalogue |
| `testdata/formats/roundtrip/**` | S9 golden of `TestExportDocsRoundTrip` |

Most tests here need a database: set `OPENV_TEST_DATABASE_URL` to a
throwaway server. Without it they skip, and the layout tests still run.

## Invariants (plan §3) that bind here

- **I16 stored data.** Migration bodies and their order, the every-boot
  baseline SQL, and the schema after `Migrate` and after
  `MigrateAndBackfill` are frozen. Never edit, reorder or renumber a
  migration that has shipped; follow up with a new one. No DDL is added to
  `InitSchema` or `schema_*.go`.
- **K9.** One `migration_00NN_<name>.go` per version plus one registry line
  in `migrations.go`; no `init()` anywhere.
- **I13.** `DATABASE_URL` wins; otherwise `sslmode=disable` with each
  `DB_*` value quoted, so a password arrives exactly as set.
- **Q17.** The purge misses some tables; that list may only shrink. A new
  table with an `org_id`, `project_id` or `artifact_id` column goes when its
  workspace is purged, through `PurgeOrg`'s list or a `NOT NULL` foreign key
  with `ON DELETE CASCADE`.
- **Q2 and S15b.** Each repository keeps its not-found answer, its `nil` or
  `[]` for an empty list, and its `ORDER BY` and tie-breaks as pinned.

## Recipes

**Add a migration.**
1. Create `migration_00NN_<name>.go` (the next free version) declaring
   `func m00NN<Name>(tx *sql.Tx) error`, with a comment saying why. Plain
   DDL; no `CREATE INDEX CONCURRENTLY`, since it runs in a transaction.
2. Append one line to `migrations` in `migrations.go`:
   `{Version: NN, Name: "<name>", Run: m00NN<Name>},`. An entry written as a
   function literal is moved into its file by
   `go run ./internal/tools/liftmigrations` (`-n` previews).
3. Regenerate the S3 goldens on a server with the vector and pg_trgm
   extensions (`pgvector/pgvector:pg15`, CI's pgvector leg):
   `OPENV_TEST_DATABASE_URL=<server URL> UPDATE_GOLDEN=1 go test ./internal/persistence/postgres -count=1 -run '^(TestMigrationFreeze|TestEveryBootFreeze|TestSchemaGolden|TestPurgeCatalog)$'`.
   The freeze appends and refuses to rewrite a shipped migration.
Scaffold: `go run ./internal/tools/scaffold migration <name>`

**Add a field to an entity.** The migration above, then every SELECT,
INSERT and Scan of the repository that reads the table (there is no shared
column list until X13), the domain type, and the import mapping in
`internal/domain/exports` if an import should carry it: `TestImportFields`
fails until `import_fields.txt` marks the field carried or dropped (see
`internal/domain/README.md`). The repository edit has no scaffold.

**Add a repository.** Implement the domain package's `Repository` in a new
`<name>_repository.go` with `New<Name>Repository(db *sql.DB)`, build it in
stage `storage` (`cmd/server/wire_storage.go`), and claim the file name with
a glob in `docs/areas.json` if none does.

## Guards

Each line: the guard, what it pins, and the command that runs it.

- **S3** `TestMigrationFreeze`, `TestEveryBootFreeze`, `TestSchemaGolden`
  and `TestPurgeCatalog`: the freeze, schema and purge goldens (I16, Q17).
  `OPENV_TEST_DATABASE_URL=<server URL> go test ./internal/persistence/postgres -count=1 -run '^(TestMigrationFreeze|TestEveryBootFreeze|TestSchemaGolden|TestPurgeCatalog)$'`
- **K9, M10** `TestRegistryIsOrderedWithoutDB` and
  `TestEachMigrationFileRegistersItsVersion`, with no database.
  `go test ./internal/persistence/postgres -count=1 -run '^(TestRegistryIsOrderedWithoutDB|TestEachMigrationFileRegistersItsVersion)$'`
- **S15b** the round trips of the team, work item, project, agent and member
  repositories (`*_repository_roundtrip_test.go`): fields, time zones, list
  order, not-found and empty-list answers.
  `OPENV_TEST_DATABASE_URL=<server URL> go test ./internal/persistence/postgres -count=1 -run '^Test(Team|WorkItem|Project|ProjectInfo|Agent|Member)Repository'`
- **S9** `TestExportDocsRoundTrip`: each `docs/exports/*.json` imported and
  exported again. Same command with `-run '^TestExportDocsRoundTrip$'`.
- **S11** `TestSchedulersShareTheRealClaim`: two schedulers racing on the
  real claim SQL. Same command with `-run '^TestSchedulersShareTheRealClaim$'`.
- **S1** archtest: K7 layering, no `init()`, K14 sizes.
  `go test ./internal/archtest`

`make check` runs the vector and no-vector tests and fails if they only
skipped; CI runs both legs.
