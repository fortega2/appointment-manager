# ADR 0005 — Prescription completion is a cache, maintained on both edges

- **Date:** 2026-08-05
- **Status:** Accepted
- **Scope:** `reopenPrescriptionQuery`, `reopenFreedPrescriptions`, `reopenEachPrescription`,
  `sortedUniqueIDs`, `cancelAndReopen`, `collectCancelledPrescriptions`, `completePrescription`
  and the three cancellation paths (`UpdateStatus`, `CancelBySlot`, `CancelOnBlockedSlots`) in
  `internal/appointment/postgres_repository.go`; the invariant they maintain is read by
  `selectActivePrescriptionForUpdateQuery` and by the `patient_session_balance` view
  (`internal/db/migrations/000009_appointment_prescription.up.sql`).

## Context

`prescription.status = COMPLETED` (2) does not record an independent fact. It is a cached answer
to a question the appointment rows already answer: *are all authorized sessions used up?*
Consumption is defined in exactly one way — appointments in status CONFIRMED (1), ABSENT (3) or
ATTENDED (4) — and that definition is written in `countConsumedSessionsQuery`, in the
`patient_session_balance` view, and now in `reopenPrescriptionQuery`.

The cache exists for a reason and cannot simply be dropped: `idx_prescription_active_per_patient`
allows only one ACTIVE prescription per patient, so a prescription must leave ACTIVE before the
patient can be issued the next one. "Exhausted" is not expressible as a partial index predicate,
since it requires counting rows in another table.

`PostgresRepository.Create` writes the cache when a booking consumes the last session. Nothing
used to clear it. Cancelling that booking returned the session — both the view and
`countConsumedSessions` agreed the patient had one available again — but the prescription row was
no longer visible to either the `AND status = 1` in `selectActivePrescriptionForUpdateQuery` or
the view's `JOIN ... AND pr.status = 1`. The freed session was unreachable: the patient dropped
out of `patient_session_balance` entirely, and therefore out of `EligiblePatients` and the booking
form, while a direct booking attempt was rejected with `ErrNoActivePrescription`.

## Decision

Every cancellation path recomputes the cache. `reopenFreedPrescriptions` runs after the
cancellation statement, in the same transaction, and returns a prescription to ACTIVE when it is
COMPLETED, is no longer exhausted by a real count of its appointments, and the patient has no
other ACTIVE prescription.

### The reopen must be a separate statement, never a CTE

The obvious shape is one statement:

```sql
WITH cancelled AS (UPDATE appointment ... RETURNING prescription_id)
UPDATE prescription ... WHERE (SELECT COUNT(*) FROM appointment ...) < total_sessions
```

**This is wrong, and wrong silently.** Postgres executes the sub-statements of a data-modifying
`WITH` under a single snapshot: they cannot see one another's effects. The `COUNT` would still
observe the appointment as CONFIRMED, consumption would be unchanged, the predicate would be
false, and the reopen would never fire — while every test that only checks that the cancellation
happened would still pass. The reopen's whole premise is reading the cancellation it follows, so
it has to be a later statement in the same transaction.

This is also why the three cancellation paths gained transactions they did not previously need.

### One statement per prescription, over IDs sorted descending

The bulk paths can free several prescriptions at once. They are reopened one statement at a time,
over deduplicated IDs in a fixed order, for two separate reasons:

- **Deadlock.** Two concurrent `CancelBySlot` calls touch disjoint appointment rows but can free
  overlapping prescription sets. A set-based `WHERE id = ANY(...)` gives no ordering guarantee, so
  the two could take prescription locks in opposite orders. A total order shared by every caller
  makes a cycle impossible.
- **The same-patient collision.** `CancelOnBlockedSlots` sweeps many slots, so one patient can
  have two COMPLETED prescriptions freed by a single run. Under one statement both satisfy the
  `NOT EXISTS` guard against the same snapshot, both are set ACTIVE, and
  `idx_prescription_active_per_patient` raises 23505 — **losing the entire cancellation**.
  Separate statements see their predecessors' work, so the second one finds the first already
  ACTIVE and declines. Verified: with a set-based reopen,
  `TestCancelOnBlockedSlotsReopensAtMostOnePrescriptionPerPatient` fails with exactly that error.

Descending order means the newest prescription wins such a tie, since `domain.NewID` mints UUIDv7
values that sort by creation time. The direction is arbitrary; only its uniformity matters.

### A freed session is forfeited rather than risk the cancellation

When the patient already has a newer ACTIVE prescription, the old one stays COMPLETED and the
session it freed is lost. The alternative — reopening it — is forbidden by
`idx_prescription_active_per_patient` and would fail the cancellation over an unrelated
administrative action. The patient has moved on to the newer prescription, so the lost session is
worth far less than a cancellation that refuses to happen.

The same priority drives the savepoint around the reopen. A *concurrent* transaction issuing a new
ACTIVE prescription is invisible to the `NOT EXISTS` guard and surfaces as 23505, which would
abort the outer transaction. The savepoint confines that to the reopen: the conflict is swallowed,
the cancellation commits, and the outcome is the pre-fix behaviour for that one session. Only that
specific conflict is forgiven — `isConcurrentActivePrescriptionConflict` matches on the constraint
name, so a dropped connection or a broken query still fails the caller rather than hiding behind a
cancellation that reports success.

## Consequences

- **Load-bearing:** the consuming set `{CONFIRMED, ABSENT, ATTENDED}` now appears in three places.
  `countConsumedSessionsQuery` and `reopenPrescriptionQuery` bind it from the same Go constants;
  the `patient_session_balance` view hardcodes `IN (1, 3, 4)`. Changing what consumes a session
  means changing the view too, or the two will disagree exactly as status and count once did.
- Because the guard counts real rows, cancellations that do *not* free a session exclude
  themselves. Cancelling inside the 24h window writes ABSENT, which still consumes, so the count
  is unchanged and no row matches. `UpdateStatus` additionally gates on `Status.IsCancelled()`,
  which is an optimisation — it keeps a correlated count off every attend — not a correctness
  requirement.
- `patient_session_balance` was deliberately left unchanged. With the invariant restored, its
  `pr.status = 1` join only excludes prescriptions that would report zero remaining sessions.
  Relaxing it would paper over a stale cache instead of preventing one.
- `readStatus` takes a `rowQuerier` rather than the pool so `UpdateStatus` can read a status
  inside its own transaction. Reaching for the pool there would check out a second connection and
  self-deadlock wherever the pool is sized at one.
- The savepoint's conflict branch is not covered by an automated test — it needs two transactions
  interleaved at a specific point. It was verified manually by forcing the reopen to raise 23505
  and confirming the cancellation still committed.
