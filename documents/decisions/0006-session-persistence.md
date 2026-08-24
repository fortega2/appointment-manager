# ADR 0006 — Sessions live in Postgres, keyed by a token digest

- **Date:** 2026-08-05
- **Status:** Accepted
- **Scope:** `internal/session` (`Store`, `Storer`, `PostgresRepository`), `internal/token`
  (`Generate`, `Hash`), the `public.session` table in
  `internal/db/migrations/000012_session_table.up.sql`, and the `expire-sessions` job in
  `cmd/server/worker.go`

## Context

`session.Store` used to hold a `map[string]*Session` guarded by a mutex, swept every 10 minutes by
a goroutine that nothing could stop. It worked, and it had exactly one flaw that mattered: the map
died with the process. Every deploy, every OOM kill, every `SIGTERM` logged out every assistant
mid-shift. Closing that was one of the last items before the first production release.

Moving the sessions into Postgres is the obvious half. The decisions worth recording are the ones
that were live options and are no longer visible in the code once the choice was made.

## Decision 1 — No in-memory cache in front of the table

The intermediate design was a write-through cache: keep the map, write to Postgres too, and fall
back to the table on a miss. It was rejected.

The cache buys one avoided round trip on the hot path. What it costs is a second source of truth,
and with it a list of questions that all have to be answered and none of which have a good answer:
what happens when the row is written but the map insert is not, what wins when the two disagree,
whether a `Delete` that fails in Postgres should still evict from memory, and what a second process
is supposed to do when it logs a user out of a session another process has cached.

What it buys is close to nothing. `Get` is a primary-key lookup on a table that holds one row per
logged-in assistant — for this clinic, single digits. That is an index scan measured in
microseconds, against an HTTP request that already does far more work than that. There is no load
here to justify a cache.

So `Store` holds no state at all. Every read goes to the `Storer`. A logout or an expiry takes
effect on the very next request, everywhere, with nothing to invalidate.

## Decision 2 — The table stores a digest of the token, never the token

`token.Generate` produces 32 bytes of `crypto/rand`, base64-URL encoded; that string is what the
cookie carries. What `public.session.id` stores is `token.Hash` of it, `hex(sha256(token))`. The
two live in `internal/token` because ADR 0010's reset links need exactly the same primitive, and
one copy of this reasoning is enough.

The property this buys: read access to the `session` table is not enough to impersonate anyone.
A database dump, a stray backup, a read replica, a `SELECT` by an operator — none of them yield a
value that can be pasted into a cookie. The same reason `assistant.password_hash` does not hold a
password.

An unsalted SHA-256 is the right primitive here, and a password KDF would be the wrong one. Salting
and stretching exist to make low-entropy, human-chosen, reused secrets expensive to guess. A
session token is none of those: it is 256 bits of uniform randomness, so there is no dictionary to
precompute and nothing to slow an attacker down against. Argon2id on this path would only add
tens of milliseconds to every authenticated request.

`Store` is the only layer that knows about the token. `PostgresRepository` receives digests and
has no way to recover the token from one, which is what keeps the property from eroding as the
code grows.

## Decision 3 — A session carries an assistant id and nothing else about the assistant

The first draft of the table had an `assistant_email` column, and the draft after that dropped the
column but kept a `Session.Email` field filled by a `JOIN public.assistant`. Both are gone. The
session row is `(id, assistant_id, created_at, expires_at)` and `Get` is a plain primary-key read.

The reason is that nothing needs it. `session.FromContext` has exactly one production caller,
`internal/appointment/ui_handler.go`, and it reads `UserID` to stamp `AssistantID` on a booking.
No handler, template or log line reads the session's email — anything that needs assistant detail
already has the assistant repository, which is where that detail belongs.

Carrying it anyway would have cost something real in each form. Denormalized onto the row, it goes
stale: a session lives 24 hours, so for up to a day after an email change the requests carry the
old address. Resolved through a join, it is a second table on the hot path of every authenticated
request to populate a field with no readers.

The FK is `assistant_id` rather than `assistant_email` for the same reason the field is gone: the
identity of an assistant is their id, not a mutable contact field.

If a future screen needs "who am I logged in as", the fix is to look the assistant up by
`Session.UserID`, not to widen the session.

## Decision 4 — `created_at` is written by the application, not defaulted by Postgres

Every other table in this schema uses `created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP`.
`session` does not, and the deviation is deliberate.

`Store.Create` computes `now` and `now + SessionDuration` from Go's clock, and `Store.Get` decides
whether a session has expired by comparing `time.Now()` against `expires_at`. If `created_at` came
from the database's clock while `expires_at` came from Go's, any skew between the two hosts would
show up as sessions that appear to have been created after they expire. Writing both marks from
the one clock that also does the comparison removes the question.

The same reasoning is why `DeleteExpired` takes the cutoff as a parameter instead of using
Postgres's `now()`.

## Decision 5 — No index on `expires_at`, and none on `assistant_id`

Following ADR 0001: an index is a write cost paid on every insert, justified by a read that
actually needs it.

`expires_at` is read by exactly one query, the `expire-sessions` sweep, which runs once per
`WORKER_INTERVAL` against a table holding one row per logged-in assistant. A sequential scan over
single-digit rows is faster than the index lookup would be, and the index would be paid for on
every login.

`assistant_id` is read by nothing at all. Its only user would be the `ON DELETE CASCADE`, and
deleting an assistant is not an operation the product offers.

## Decision 6 — The sweep is a `worker.JobFunc`, not a goroutine in the package

`Store.DeleteExpired(ctx) (int64, error)` is shaped like `worker.JobFunc` on purpose, and is
registered in `startBackgroundWorkers` next to the two appointment sweeps.

The goroutine it replaces was started inside `NewStore`, had no context, logged through
`context.Background()`, and had no way to be stopped — it outlived the pool it would now be
querying. Reusing the worker machinery gets the ticker, the per-job logging and the ordered
shutdown for free, and puts every periodic job in this process in one place.

## Revisit when

- **A second instance of the process runs.** Nothing here breaks — that is the point of Decision 1
  — but `SessionDuration` and the sweep interval should be reviewed against the real session count.
- **The session table stops being small.** If `SELECT COUNT(*) FROM public.session` reaches the
  thousands, revisit Decision 5: the `expires_at` index starts paying for itself somewhere around
  there.
- **Sessions need to be revoked in bulk** (a "log out everywhere" action, or invalidation on
  password change). That is a `DELETE ... WHERE assistant_id = $1`, and it is the read that would
  justify the `assistant_id` index.
- **Sliding expiry is wanted.** It was considered and rejected as out of scope here: it turns every
  authenticated `GET` into an `UPDATE` unless a renewal threshold is added first.
