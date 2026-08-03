# ADR 0001 — No partial index for clinic-cancelled slot lookups (yet)

- **Date:** 2026-08-03
- **Status:** Accepted
- **Scope:** `slotCancellationRecipientsQuery` in `internal/appointment/query.go`

## Context

`Query.SlotCancellationRecipients` resolves who the clinic owes a message after it cancels a
slot. It is called once per cancellation, from the `internal/notification` drain goroutine, under
a 5 s per-send timeout.

```sql
WHERE a.slot_id = $1
  AND a.status = $2   -- StatusCancelledByClinic (5)
```

Before wiring the notification service we checked whether this query has an index to use, and
whether the `$2` placeholder prevents one from being used.

`public.appointment` has exactly two indexes touching `slot_id`, and **both are partial on
`WHERE status = 1`**:

```
"idx_appointment_slot_confirmed"       btree (slot_id) WHERE status = 1
"idx_appointment_slot_patient_active"  UNIQUE, btree (patient_id, slot_id) WHERE status = 1
```

There is no general `slot_id` index — Postgres does not create one for a foreign key, unlike the
referenced side.

## Measurements

Run in a throwaway `explain_scratch` database inside the `appointment-manager-postgres`
container, against a 200 000-row table across 20 000 slots that mirrors the two real partial
indexes. Numbers are synthetic in absolute terms but the *plan shapes* are what matter.

### Without any `status = 5` index (today)

| Case | Query form | Plan | Buffers | Time |
|---|---|---|---|---|
| A | `status = 5` literal | Parallel Seq Scan | 2062 | 8.04 ms |
| B | `status = $2` placeholder, generic plan | Parallel Seq Scan | 2062 | 7.24 ms |
| C | literal, `enable_seqscan = off` | Parallel Seq Scan, `Disabled: true` | — | — |
| D | *contrast:* `status = 1` literal | Bitmap Index Scan on `idx_appointment_slot_confirmed` | 8 | 0.042 ms |

A and B are identical plans. Case C is the proof: even with sequential scans penalised, the
planner still picks one and marks it disabled, meaning **no index is a candidate at all**.
`status = 5` and `status = 1` are disjoint predicates, so neither partial index can ever serve
this query in either query form.

Case D is the same query shape on `status = 1` — the committed `cancelAppointmentsBySlotQuery`
path. It is ~190× faster and touches ~260× fewer buffers, which is what a matching partial index
buys.

### With a hypothetical `WHERE status = 5` index

```sql
CREATE INDEX idx_appointment_slot_cancelled_by_clinic
    ON public.appointment (slot_id) WHERE status = 5;
```

| Case | Query form | Plan | Buffers | Time |
|---|---|---|---|---|
| E | `status = 5` literal | **Index Scan** | 3 | 0.033 ms |
| F | `status = $2` placeholder, generic plan | Parallel Seq Scan | 2062 | 7.21 ms |

Index size: **784 kB** against a 16 MB table — cheap, because only clinic-cancelled rows are
indexed.

## Decision

**Do not add the index now.** Leave `slotCancellationRecipientsQuery` as written, placeholder
included.

Reasons:

- The placeholder costs nothing today. A ≡ B, and case C shows there is nothing for it to
  discard. This is *not* the bug that was fixed in `cancelAppointmentsBySlotQuery`.
- The query runs once per slot cancellation, on a background drain, not on a request path. A
  single-digit-millisecond scan is not worth an index that must be maintained on every write to
  `appointment`.
- Adding it now would be optimising a table that is currently tiny, on speculation about a shape
  the notification pipeline has not yet proven in production.

## Consequence to remember

**If this index is ever added, `a.status = $2` must become the literal `5` in the same change.**

Cases E and F are the whole point of recording this. Postgres can only use a partial index when
it can *prove* the query's `WHERE` implies the index predicate, and a parameter hides the value
from a generic plan. With the index in place, the literal is an Index Scan (3 buffers, 0.033 ms)
and the placeholder falls back to a Parallel Seq Scan (2062 buffers, 7.21 ms) — a ~240×
regression that **no test would catch**, since the results are identical.

`cancelAppointmentsBySlotQuery` already carries an explanatory comment about exactly this; the
literal there is load-bearing for the same reason.

## Revisit when

Any of:

- `public.appointment` grows past ~1M rows — the seq scan is linear, so ~8 ms at 200k becomes
  ~80 ms at 2M.
- Slot cancellation stops being rare (bulk schedule changes, a professional's whole week pulled).
- Buffer-cache pressure becomes a concern. Each call currently evicts ~16 MB of shared buffers,
  which pollutes the cache for latency-sensitive request-path queries.

At that point: one migration adding the partial index, **plus** the `$2` → `5` change, **plus**
the comment explaining why the literal is required.