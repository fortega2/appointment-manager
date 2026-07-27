---
name: db-migration
description: Create, apply, or roll back PostgreSQL schema migrations in appointment-manager using golang-migrate. Use whenever a task requires adding/changing a table, column, index, or constraint.
tools: Bash, Read
---

# DB Migration (golang-migrate)

`appointment-manager` uses `golang-migrate` against PostgreSQL (`pgx` driver
for the app itself). Migration files live under `internal/db/migrations`, as
sequential up/down pairs (e.g. `000002_lookup_tables.up.sql` /
`000002_lookup_tables.down.sql`).

## Create a new migration

```bash
migrate create -ext sql -dir internal/db/migrations -seq <migration_name>
```

This generates an empty `.up.sql` / `.down.sql` pair with the next sequence
number. Write the forward change in `.up.sql` and the exact inverse in
`.down.sql` — every `up` must have a working `down`.

## Apply pending migrations

```bash
migrate -path internal/db/migrations -database "$DATABASE_URL" up
```

## Roll back the last migration

```bash
migrate -path internal/db/migrations -database "$DATABASE_URL" down 1
```

## Conventions to follow

- Keep each migration focused (one logical schema change per sequence number)
  rather than bundling unrelated changes.
- Appointment status columns must remain concurrency-safe: status transitions
  are done via atomic updates with an expected current status (or equivalent
  locking) — don't design a migration that would require read-then-write
  status updates in application code.
- `$DATABASE_URL` must be exported in the shell before running `up`/`down`;
  it is not read from a project `.env` file automatically.
- After writing a migration, prefer running it against a local/dev database
  and confirming both `up` and `down` succeed before committing.
