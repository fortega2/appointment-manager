# ADR 0002 — Notification queue: in-memory channel, drop-on-full, resolve-per-event

- **Date:** 2026-08-04
- **Status:** Accepted
- **Scope:** `internal/notification`, plus the `NOTIFICATION_*` variables wired in
  `cmd/server/notification.go`

## Context

Cancelling a slot means telling the patients booked on it. That work must not be paid for by the
HTTP request that triggered it: the assistant clicking *cancel* should get their table back
immediately, whether or not a mail server is reachable.

The shape chosen is a buffered channel drained by a background goroutine:

- `slot.Handler` calls `sendNotification(ctx, id)` once, at `internal/slot/handler.go:282`, after
  the slot and its appointments are already cancelled. It is bound to
  `notification.Service.NotifySlotCancelled` as a method value.
- `Service.Run` ticks every `NOTIFICATION_TICKER_INTERVAL` and drains whatever is buffered.
- `Service.send` resolves recipients through `appointment.Query.SlotCancellationRecipients`
  (`internal/appointment/query.go:154`) and delivers.

Delivery itself is still a placeholder — a log line per recipient in `sendSlotCancelled`. Everything
below is about the pipeline around it, which is real.

This ADR records why each knob is set where it is, because all three defaults look arbitrary
without the reasoning, and two of them are easy to "optimise" in the wrong direction.

## Decision 1 — The queue carries identifiers, not rendered content

`Event` (`internal/notification/event.go:19`) holds a `SlotID` and a `Kind`. It does not hold
recipients, addresses, or message bodies.

Recipients are resolved at *send* time, inside the drain. An event that waited in the queue
therefore cannot deliver a stale view of the booking: if a patient cancelled their own appointment
in the meantime, they are no longer in the result set. `SlotCancellationRecipients` reads only
`StatusCancelledByClinic`, so patients who cancelled on their own are excluded by construction —
the clinic owes them nothing.

The consequence to keep in mind is that this makes the queue *cheap* but the drain *dependent on
the database*. The shutdown flush therefore has to run while the pool is still open, which is why
`startNotificationWorker` returns a `stop` func that callers defer before the pool closes
(`cmd/server/notification.go:77-89`).

There is a second, quieter benefit: `Event` contains no pointers. See Decision 3.

## Decision 2 — Enqueue drops on a full queue, it never blocks

`enqueue` (`internal/notification/notification.go:117-126`) is a `select` with a `default` that
logs a warning and discards the event.

The alternative — blocking until space frees — would push backpressure from a broken mail server
straight into an HTTP handler, holding a request (and its pooled database connection) hostage to a
subsystem the user never asked about. For a best-effort courtesy message that trade is wrong in
every direction.

Dropping is the correct failure mode here **because the notification is not the record**. The
appointment is already cancelled and durably stored before `sendNotification` is ever called; the
message is an announcement of a fact that survives independently.

## Decision 3 — `NOTIFICATION_BUFFER_SIZE` defaults to 100

### Sizing rule

```
buffer ≈ peak cancellations per drain cycle × safety factor
       = (cancellations/min) × NOTIFICATION_TICKER_INTERVAL(min) × 3..5
```

The producer rate is bounded by human clicks. There is no bulk-cancel endpoint: `handler.go:282`
fires exactly one event per HTTP request, not in a loop. A realistic peak for a clinic is
~10-20 cancellations/minute, so 100 leaves 5-10× headroom at the default 1 m interval.

### Memory cost — measured

`make(chan Event, N)` allocates the whole ring buffer eagerly, at `make` time. It never grows and
never shrinks.

| Quantity | Value |
|---|---|
| `unsafe.Sizeof(Event{})` | **18 bytes** (`SlotID [16]byte` at offset 0, `Kind int16` at offset 16) |
| `unsafe.Alignof(Event{})` | 2 |
| Buffer at `N=100` | 100 × 18 = **1800 bytes** |
| Plus `hchan` header | ~96 bytes on 64-bit → **≈1.9 KB** after size-class rounding |

Memory is not the constraint and never will be: even `N=10000` is ~180 KB. Because `Event`
contains no pointers, the runtime places the `hchan` header and the buffer in a **single contiguous
allocation** and the GC does not scan it — zero GC pressure.

**Load-bearing consequence:** if `Event` ever gains a pointer field (a `*string`, a slice, a
rendered body), the buffer becomes a separate, GC-scanned allocation and this property is lost.
Decision 1 already keeps content out of the event; this is a second reason to keep it that way.

### Why not a much larger buffer

Memory would allow it, but throughput and usefulness do not:

- The drain is **sequential**, one database query per event under a 5 s `sendTimeout`. If deliveries
  are timing out it clears ~12 events/minute. A buffer of 1000 does not deliver more — it converts
  "dropped now, with a warning" into "delivered 80 minutes late".
- `flushTimeout` is 5 s for the *entire* shutdown drain. A deep backlog is discarded at shutdown
  regardless, so a large buffer is false comfort.
- The queue is in memory. A deeper queue simply loses more on a hard crash.

For a "your appointment was cancelled" message, dropping with a warning beats delivering long after
the fact.

## Decision 4 — `NOTIFICATION_TICKER_INTERVAL` defaults to 1 m, and is not a load knob

This is the knob most likely to be mistuned, because the project's *other* ticker looks identical
and behaves the opposite way.

| | `internal/worker` | `internal/notification` |
|---|---|---|
| What a tick does | Runs `job` **unconditionally** (`worker.go:72-81`) — a DB `UPDATE` sweep | `drain` returns immediately if the queue is empty |
| Cost of a tick at rest | One database round trip | A goroutine wakeup and one non-blocking channel receive |
| Default | `30m` | `1m` |
| Interval is therefore | A real load knob | A **latency** knob |

`drain` (`notification.go:142-151`) is a `select` with a `default`: on an empty queue it touches
nothing and returns. 1440 ticks a day cost nanoseconds each. The differing defaults are not
arbitrary — they follow from the two rows above.

What the interval actually controls:

1. **Notification latency** — the maximum delay between cancelling a slot and telling the patient.
2. **Crash exposure** — the queue is in memory. A graceful shutdown is covered by `flush`, so
   deploys lose nothing; a `SIGKILL`, OOM, or panic loses up to one interval of events.
3. **Buffer coupling** — the sizing rule in Decision 3 scales with the interval, so a 10 m interval
   consumes the same buffer 10× faster, and concentrates the drain's database work into a burst
   instead of spreading it.

Raising it to `5m` or `10m` buys nothing, because there is nothing to save, and pays on all three
counts. Lowering it is nearly free for the same reason `drain` is free at rest.

## Decision 5 — One query per event, not one batched `IN` per drain

A batched drain was considered: collect every buffered `SlotID`, issue a single
`WHERE slot_id = ANY($1)`, group recipients by slot in Go.

**Rejected at the current scale.** The arithmetic does not support it:

- Typical queue depth per drain is 0 or 1 — one event per HTTP request against a 1 m tick. At
  `N=1` the batch is identical to today plus the code to build it.
- The saving is `(N-1)` round trips *per minute*. At an optimistic `N=3`, that is ~2 round trips a
  minute against local Postgres: single-digit milliseconds per minute.

And it would cost a property that is currently tested. Today the drain isolates failures: if
resolving slot A fails, it is logged and slot B is still sent. That is asserted by
`TestServiceKeepsDrainingAfterALookupFailure` (`notification_test.go:471`), which queues a
`failing` and a `healthy` event together and requires both log lines. A single batched query turns
any error into the loss of the whole batch, and that test would have to be weakened.

Two smaller costs: `slotCancellationRecipientsQuery` repeats the slot and professional columns on
every row of the join, so a batched version must group by `slot_id` in Go; and it must separately
track which requested slots returned no rows, or the "cancelled slot had no recipients" log is
lost.

The one genuine argument in favour is the shutdown flush — with `flushTimeout` 5 s total and
`sendTimeout` 5 s per event, a backlog of ten events against a slow database delivers roughly one,
where a single batched query would fit entirely. That is a rare scenario on a queue that is already
documented as best-effort and in-memory, and it does not outweigh the fault isolation.

## Future: when delivery becomes real (email/SMS)

Replacing the inner loop of `sendSlotCancelled` is what makes delivery real. The decisions above
are deliberately staged so that change is additive, but three of them should be revisited *in that
same change*:

- **Batch the provider call, not the query.** This is where Decision 5 flips. A provider round trip
  costs 100 ms+, not 2 ms, so `(N-1)` saved calls per drain start to matter — and most providers
  accept many individual messages in one API call. The batching unit is "N messages, one provider
  request", which is why a longer `NOTIFICATION_TICKER_INTERVAL` may become defensible then and is
  not now. Note that batching across slots groups unrelated recipients; it does not change
  Decision 1, since resolution still happens at send time.
- **Per-recipient failure must not sink the event.** The current fault isolation is per *event*.
  Real delivery needs it per *recipient*: one rejected address should not cost the other patients
  on the same slot their message.
- **Retries and durability become a real question.** A provider that is down for longer than
  `flushTimeout` means the message is simply lost, with only a warning. At that point the in-memory
  queue is the limiting factor, and the honest fix is an outbox table rather than a deeper channel
  — the appointment is already in Postgres, so the announcement can be too.

Two things that should be done *before* any of the above, because they make the tuning empirical
instead of estimated:

- **A drop counter.** The drop in `enqueue` is only a `WarnContext`. `internal/metrics` already
  exists; an `appt_notifications_dropped_total` turns "is 100 enough?" into a Grafana panel. If it
  never increments, 100 is generous. If it does, the first response is to *shorten* the interval,
  not to deepen the buffer (Decision 4).
- **A queue-depth gauge**, for the same reason: it is the direct measurement behind the sizing rule
  in Decision 3.

## Revisit when

Any of:

- Delivery stops being a log line — see the section above; it touches Decisions 4 and 5.
- `appt_notifications_dropped_total` (once it exists) is non-zero in production.
- Slot cancellation stops being one-at-a-time — a bulk endpoint, or a professional's whole week
  pulled in one action, breaks the "one event per human click" premise that Decisions 3 and 5 both
  rest on. This is the same trigger listed in ADR 0001.
- The notification becomes something the clinic is obliged to prove it sent, rather than a
  courtesy. Best-effort delivery on an in-memory queue is then no longer acceptable at all, and the
  outbox is not optional.