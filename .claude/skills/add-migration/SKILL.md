---
name: add-migration
description: Use when changing the OpenV Postgres schema, by adding a numbered migration, or when adding a field to an entity, whose schema part is a migration.
---

# Add a migration

The recipes are in the area guide; follow them there.

- [internal/persistence/postgres/README.md, Recipes](../../../internal/persistence/postgres/README.md#recipes),
  "Add a migration", and "Add a field to an entity" for the repository
  edit, which has no scaffold.
- The long form: [docs/DEVELOPMENT.md](../../../docs/DEVELOPMENT.md),
  "Adding a schema migration".

Scaffold (`-n` previews; it prints the steps left when it is done):

- `go run ./internal/tools/scaffold migration <name>`

Before you finish: [internal/persistence/postgres/CLAUDE.md](../../../internal/persistence/postgres/CLAUDE.md),
then "Before you finish" in the root [CLAUDE.md](../../../CLAUDE.md).
