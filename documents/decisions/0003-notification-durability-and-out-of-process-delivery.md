# ADR 0003 — Notification durability: outbox first, broker only on a trigger

- **Date:** 2026-08-04
- **Status:** **Proposed** — nothing here is built, and nothing in the tree depends on it. See
  *Status note* below before treating any of it as settled.
- **Scope:** `internal/notification`, `internal/slot` (the cancel path), `internal/worker`,
  and any future queue/transport package
- **Extends:** ADR 0002, specifically its *Future: when delivery becomes real* section, which
  named the outbox as the honest fix but did not work out what that implies

## Status note

`Proposed` means: the reasoning is recorded so it does not have to be re-derived, but the
decision is **not** made and no code should be written against it yet.

This ADR exists because the question "how do we move notifications to a real queue?" was asked
and answered informally. The answer contains at least two conclusions that are not obvious and
would very likely be got wrong on a second pass — Decision 2 (outbox and broker are not
alternatives) and Decision 4 (an out-of-process sender breaks ADR 0002's Decision 1). Losing
those to a chat log is the failure this file prevents.

What promotes it to `Accepted` is listed under *Revisit when*. Until one of those fires, the
in-memory design in ADR 0002 stands unchanged and is the correct design for the current load.

## Context

Today the pipeline is a buffered channel drained by a goroutine in the same process
(ADR 0002). It is best-effort by design: `enqueue` drops on a full queue, and a `SIGKILL`, OOM
or panic loses up to one tick of events. Delivery itself is still a log line.

The prompt for this ADR was a proposal to scale that design outward:

- keep the producer seam as an indirection (func or interface) — the handler hands off and returns;
- split `internal/notification` into a **queue** part (Redis/Valkey, RabbitMQ, NATS, SQS) and a
  separate **processor** part that could run as a Lambda or equivalent, triggered per message or
  on a schedule, so delivery no longer depends on the main process's memory or lifetime;
- with an open question: should the processor read from SQS, or from an outbox table?

The decisions below answer that, and record the parts of it that are traps.

## Decision 1 — The producer seam needs no change, and no new abstraction

`slot` declares its own `sendNotificationFunc` and binds `Service.NotifySlotCancelled` to it as a
method value (`internal/slot/handler.go:282`). The abstraction already lives with the consumer,
which is what the `internal/notification` package doc calls out as deliberate.

The consequence worth stating plainly: **moving to SQS, NATS, or an outbox requires zero changes
in `internal/slot`.** No `Notifier` interface needs to be introduced to enable any of this. The
portability layer already exists and it is that func type.

## Decision 2 — Outbox and broker are not alternatives; the outbox feeds the broker

This answers the open question directly, by rejecting its framing.

- The **outbox** is a *producer-side durability* mechanism.
- The **queue** is a *transport*.

They solve different problems and the first is the one we actually have.

### The dual-write problem

If a handler commits the cancellation to Postgres and then publishes to SQS, those are two
systems with no shared transaction. A crash between them leaves the cancellation durably stored
and the notification non-existent — the same hole as today's in-memory channel, but with more
infrastructure and the false impression of durability.

**Swapping `chan Event` for SQS fixes nothing on its own**, because the fragile point is not the
queue, it is the gap between the commit and the publish.

The canonical shape is: the handler writes the domain change **and** the outbox row in one
transaction; a relay then reads the outbox and publishes. Or the consumer reads the outbox
directly and there is no broker at all (Decision 3).

### Local complication: the cancel path has no transaction today

`internal/slot` uses no transactions. `h.repo.Cancel` (`handler.go:257`) and `h.cancelAppointments`
(`handler.go:275`) are already two separate statements, and the comment above the second one
states the pair can be left inconsistent and that the reconciliation sweep converges it.

Two things follow:

1. Adding an outbox means introducing a real transaction on this path. Small, but not free, and
   it is the actual work item hiding behind "just add an outbox table".
2. There is already an accepted precedent in this codebase for eventual consistency converged by
   a periodic worker. The outbox is the same shape, so it is not a foreign pattern here.

## Decision 3 — Stage it, and expect to stop at stage 1

### Stage 1 — outbox table drained by the existing worker

A `notification_outbox` table (`id`, `kind`, `slot_id`, `status`, `attempts`, `next_attempt_at`,
`created_at`, `sent_at`, `last_error`), written in the same transaction as the cancellation.
`internal/worker` polls it with `SELECT ... FOR UPDATE SKIP LOCKED`, delivers, marks the row, and
backs off on failure.

No new infrastructure. This alone converts *best-effort, lost on `SIGKILL`* into *at-least-once,
durable* — which is the real gap ADR 0002 names. It also **preserves ADR 0002's Decision 1
intact**, because the outbox row still carries identifiers only and resolution still happens at
send time.

### Stage 2 — a broker in front of the sender

Only once there is a concrete trigger: a second consumer, fan-out across channels (email *and*
SMS *and* push, delivered independently), or volume that a single sequential drain cannot clear.

### Why stage 1 is likely the terminal state, not a stepping stone

- ADR 0002 Decision 3 documents the producer rate as bounded by human clicks — ~10-20
  cancellations/minute, one event per HTTP request, no bulk-cancel endpoint. Postgres with
  `SKIP LOCKED` handles orders of magnitude more than that.
- Deployment is a single VPS running Docker stacks. Adopting SQS/Lambda introduces an AWS
  dependency into an otherwise self-hosted application, to solve a scale problem that does not
  exist.
- A broker earns its place when you need to *decouple services* or *fan out*, not when you need
  *durability*. For durability, Postgres is already there and already transactional with the data
  that matters.

## Decision 4 — An out-of-process sender collides with ADR 0002's Decision 1

This is the part of the original proposal with a non-obvious cost, and the reason this ADR is
worth writing even though nothing is being built.

ADR 0002 Decision 1 resolves recipients **at send time**, so a queued event can never deliver a
stale view of the booking — a patient who cancelled their own appointment in the meantime drops
out of the result set by construction. That property is only free while the sender is in-process
and holds a database pool.

Move the sender to a Lambda and there are three ways out, none free:

| | Keeps ADR 0002 Dec. 1 | Cost |
|---|---|---|
| **a. Lambda connects to Postgres** | Yes, intact | VPC wiring, plus the known Lambda/Postgres connection-pool problem (each concurrent invocation is a connection; needs RDS Proxy or equivalent). Couples a nominally separate service to our schema. |
| **b. Message carries rendered content** | **No** | Staleness returns. And it puts patient PII (name, email, phone) in the queue, in transit and at rest — a compliance decision for health data, not an architecture one. |
| **c. Relay resolves, Lambda only delivers** | Mostly | Relay runs in the main process (which has the pool), reads the outbox, resolves, and publishes an already-resolved message; the Lambda is a dumb provider caller that knows nothing of the schema. Resolution is still late — at relay time rather than enqueue time — so most of the value survives. PII in the queue remains, but the window shrinks. |

**If stage 2 ever happens, (c) is the preferred shape.** Option (b) should not be chosen by
accident, which is exactly how it tends to get chosen — it is the path of least resistance when
someone wants the consumer to be "standalone".

## Decision 5 — Do not build a multi-broker abstraction

The proposal listed Redis/Valkey, RabbitMQ, NATS and SQS as interchangeable behind one package.
They are not, and an interface over all four is a known trap: delivery semantics differ too much
(SQS visibility timeout and ack; JetStream ack/nak/term; Redis Streams consumer groups and
`XAUTOCLAIM`). The intersection of all of them degenerates to `Publish([]byte)`, which discards
precisely what makes any of them useful.

Pick one and write against it directly. The portability layer is the consumer-side func type
(Decision 1), not a broker facade.

**If Redis/Valkey is the pick, it must be Streams, not pub/sub.** Pub/sub has no durability and
no acks — that is fire-and-forget, i.e. what we already have, plus a network hop.

## Decision 6 — Idempotency keys must be per recipient, not per event

Every design above delivers *at-least-once*. While delivery is a log line this is harmless; once
it is real email, a duplicate is a user-visible defect.

The subtlety comes from ADR 0002 Decision 1: because the event carries no recipient list, a
retried event **re-resolves** a set that may have changed since the first attempt. So the dedup
record has to be keyed on `(slot_id, recipient_id, kind)`, not on the event id.

This is the same conclusion ADR 0002 reaches from the other direction when it says fault
isolation must move from per-event to per-recipient. Both changes belong in whichever change
makes delivery real.

## Prerequisite — measure before migrating any of this

Unchanged from ADR 0002, and restated because it gates everything above:
`appt_notifications_dropped_total` and a queue-depth gauge come first.

If the drop counter never increments in production, the buffer is sufficient and the *only*
remaining argument for migrating is durability — which is a real argument, but a different one
from scale, and it points at stage 1 rather than stage 2. Migrating on an estimate instead of a
measurement is how a project ends up with a broker it does not need.

## Revisit when

Any of these promotes this ADR from `Proposed` toward `Accepted` — and each points at a different
stage, so note which:

- **`appt_notifications_dropped_total` is non-zero in production** → measure first; per ADR 0002
  Decision 4 the first response is a shorter interval, not this ADR.
- **Delivery stops being a log line** → stage 1. Durability, retries and Decision 6 all become
  real at once; the in-memory queue is the limiting factor from that moment.
- **A provider outage outlasts `flushTimeout`** → stage 1, urgently. This is the concrete failure
  the outbox exists to prevent.
- **Notifications become something the clinic must prove it sent**, rather than a courtesy →
  stage 1 is mandatory, not optional. Best-effort on an in-memory queue is not defensible.
- **A second consumer or genuine fan-out appears** (email *and* SMS delivered independently, or
  another service needing the same events) → the first real argument for stage 2, and the point
  at which Decision 4 must be settled before any code is written.
- **Slot cancellation stops being one-at-a-time** — a bulk endpoint, or a professional's whole
  week cancelled in one action → breaks the "one event per human click" premise that Decision 3
  rests on here and that Decisions 3 and 5 rest on in ADR 0002. Same trigger as ADR 0001.