# ADR 0004 — Notification observability: what is measured, and what is deliberately not

- **Date:** 2026-08-04
- **Status:** Accepted
- **Scope:** `internal/metrics` (the `appt_notifications_*` collectors and their recorder
  methods), `internal/notification` (`Metrics`, `noopMetrics`, `EventKind.String`, the span in
  `Service.send`), and the wiring in `cmd/server/notification.go`
- **Implements:** ADR 0002's *Future* section, which named `appt_notifications_dropped_total` and a
  queue-depth gauge as prerequisites to tuning the queue empirically

## Context

ADR 0002 sized `NOTIFICATION_BUFFER_SIZE` and `NOTIFICATION_TICKER_INTERVAL` from arithmetic —
"a realistic peak for a clinic is ~10-20 cancellations/minute", "if deliveries are timing out it
clears ~12 events/minute" — and then said so plainly: the two metrics below are what turn those
estimates into measurements. Its own *Revisit when* list was blocked on a metric that did not
exist.

Everything the queue did was visible only as a log line. Two consequences mattered:

- **The drop was unalertable.** `enqueue` discards on a full queue with a `WarnContext` and
  nothing else. That is the correct behaviour (ADR 0002 Decision 2), but a correct behaviour still
  needs to be counted, because it is the signal that the configuration is wrong.
- **A lost notification looked like a working one.** If `resolveSlotCancellation` errors inside the
  drain, the event is already off the queue and nothing retries it. The notification is not late,
  it is *gone* — and the only record was an `ERROR` line.

## Decision 1 — Five series, answering four distinct questions

| Series | Question it answers |
|---|---|
| `appt_notifications_dropped_total{reason}` | Is the buffer big enough? |
| `appt_notifications_queue_depth` | How close to full does it get *before* anything is lost? |
| `appt_notifications_queue_capacity` | Against what ceiling? |
| `appt_notifications_processed_total{kind,outcome}` | Are notifications actually getting out? |
| `appt_notifications_send_duration_seconds{kind}` | Can the sequential drain keep up? |

`dropped_total` and `queue_depth` are not redundant. The counter is a post-mortem: by the time it
moves, messages are already lost. The gauge is the leading indicator, and it is the direct
measurement behind Decision 3's sizing rule. Capacity is exported alongside depth so saturation is
a ratio (`depth / capacity`) rather than a threshold hard-coded per environment — it also records
what the buffer size resolved to in *this* process, which is the same reason `logPoolConfig` reads
the pool's settings back off the live pool.

Per ADR 0002 Decision 4, the correct first response to a non-zero `dropped_total{reason="queue_full"}`
is to **shorten the interval**, not deepen the buffer. That is easy to get backwards, which is why
it is repeated here next to the metric that triggers it.

## Decision 2 — `reason` and `outcome` are labels, not separate metrics

There are two ways to discard a notification and three ways to finish one. Both sets are closed,
small, and always looked at together — you want "what happened to notifications" as one breakdown,
not four unrelated counters that a dashboard has to re-assemble. This matches
`appt_appointments_finalized_total{outcome}`, which made the same call for the same reason.

The label values are owned by `internal/metrics`, not by the caller: `Metrics` exposes
`RecordNotificationDroppedQueueFull()` and `RecordNotificationDroppedUnknownKind()` rather than a
single `RecordNotificationDropped(reason string)`. The queue knows it ran out of room; it does not
know what a Prometheus label is called. `kind` is the exception and *is* passed as a string,
because that vocabulary genuinely belongs to `internal/notification`.

`outcome="no_recipients"` is kept separate from `"sent"` on purpose. Cancelling a slot nobody
booked notifies nobody, which is an ordinary result — but if it were folded into `sent`, a bug that
resolved every slot to zero recipients would look exactly like healthy delivery.

## Decision 3 — `kind` is a label today even though it has one value

`EventKind` has exactly one member, so `kind` adds nothing right now. It is present because ADR
0002's whole premise is that this queue grows more kinds (appointment reminders are the obvious
next one), and adding a label later silently breaks every recorded series and every dashboard
query built on it. Adding it now costs one label on a low-frequency counter.

**Load-bearing:** `EventKind.String` returns `"unknown"` from its `default` branch
(`internal/notification/event.go`). That default is not politeness — it is what bounds the label's
cardinality. `EventKind` is an `int16`, so a bug that queued arbitrary values would otherwise mint
up to 65 535 series. Any new kind must be added to that switch; anything not in it collapses onto
one series and is counted by `dropped_total{reason="unknown_kind"}`, which should stay flat at zero
forever.

## Decision 4 — The send span is a root, and stays one

`Service.send` opens `notification.Service.send` as a **new root span**. It is deliberately *not*
linked to the HTTP request that cancelled the slot.

Linking them would mean carrying the producer's `trace.SpanContext` on `Event`. That type embeds a
`TraceState`, which is backed by a slice — and ADR 0002 Decision 3 records, as a load-bearing
property, that `Event` contains **no pointers**, which is why the runtime places the channel's
`hchan` header and its whole ring buffer in a single contiguous allocation the GC never scans. One
pointer field costs that for every event, permanently.

What the root span buys, even unlinked: before this, the drain's recipient query surfaced in Tempo
as a parentless `db.select` with nothing saying what asked for it. It also gives
`send_duration_seconds` a sampled context, so its `trace_id` exemplar actually attaches and a slow
send links from Grafana straight to the trace.

`sendSlotCancelled` returns an `error` purely so the span gets a meaningful status. Resolving *no
recipients* returns `nil` — same reasoning as `spanError` in `internal/appointment/service.go`: an
ordinary outcome must not feed trace-based error-rate alerting.

If the link is ever wanted, the way that preserves the property is to store the raw `TraceID` and
`SpanID` as fixed-size arrays (`[16]byte`, `[8]byte`) and rebuild a `SpanContext` at send time,
dropping `TraceState`. Not done here; recorded so the pointer-free constraint is not mistaken for
"linking is impossible".

## Decision 5 — Queue depth is read at scrape time, not published on enqueue

`RegisterNotificationQueue` takes two functions and registers them as `prometheus.GaugeFunc`, so
`len(queue)` and `cap(queue)` are evaluated during collection. The alternative — a plain gauge that
`enqueue` and `drain` write — was rejected: it puts work on the producer's path (which ADR 0002
Decision 2 is entirely about keeping empty), and a gauge written on activity goes stale the moment
activity stops, which is exactly when someone is reading the dashboard.

The registration is separate from `metrics.New` for the same reason `RegisterDBPool` is: the
channel does not exist until the service does. A full `prometheus.Collector` like `dbPoolCollector`
is not needed — that one exists because it carries a `state` label.

## Decision 6 — No queue-wait histogram

Considered and rejected: a histogram of how long an event sat in the queue before being sent.

It would need an enqueue timestamp on `Event`. A `time.Time` is disqualified outright — it holds a
`*Location`, which breaks Decision 4's constraint above. An `int64` of Unix nanoseconds *would*
stay pointer-free (`Event` would grow 18 → 32 bytes, still trivial), so the constraint is not the
real objection.

The real objection is that the metric would mostly re-derive its own configuration. `drain` runs on
a ticker, so wait time is bounded by `NOTIFICATION_TICKER_INTERVAL` and distributed roughly
uniformly across it. The histogram would show the interval, which is already known. When it
*would* become informative is when the drain stops clearing the queue within one tick — and
`queue_depth` together with `send_duration_seconds` already says that, without touching `Event`.

Revisit if delivery becomes real and per-message latency becomes something the clinic cares about
rather than something the interval determines.

## Decision 7 — Metrics are optional, injected, and default to a no-op

`notification.Metrics` is declared in `internal/notification` — the consumer — and satisfied by
`*metrics.Metrics` structurally; a `nil` recorder is swapped for `noopMetrics` in the constructor
rather than rejected by `validate`. This copies `internal/appointment/service.go` exactly, and the
reason is the same: a deployment that exports no metrics must still run, and tests must not have to
wire a registry to exercise behaviour.

There is a second effect worth stating, because it is load-bearing for the test suite:
`newRecordingService` in `notification_test.go` passes `nil`, so every behavioural test in that file
runs against `noopMetrics`. That is what keeps the no-op path from being a branch nothing executes.
Instrumentation assertions use `newMeteredService` instead.

## Dashboard queries

The Grafana row is reproducible from these; keep them here so it can be rebuilt from the repo.

```promql
# Queue depth vs capacity
appt_notifications_queue_depth
appt_notifications_queue_capacity

# Saturation (single stat, alert at 0.8)
appt_notifications_queue_depth / appt_notifications_queue_capacity

# Dropped, by reason
sum by (reason) (rate(appt_notifications_dropped_total[$__rate_interval]))

# Processed, by outcome
sum by (outcome) (rate(appt_notifications_processed_total[$__rate_interval]))

# Send latency p95
histogram_quantile(0.95, sum by (le) (rate(appt_notifications_send_duration_seconds_bucket[$__rate_interval])))
```

`dropped_total` is absent from `/metrics` until it first increments — normal for a `CounterVec`
with no observed label values — so its panel must tolerate no data rather than be read as broken.

## Revisit when

- `appt_notifications_dropped_total{reason="queue_full"}` is non-zero in production. This is ADR
  0002's own revisit trigger; the response order is in Decision 1 above.
- `appt_notifications_dropped_total{reason="unknown_kind"}` is non-zero at all. It means a kind was
  queued that `send` has no branch for, and that `EventKind.String` has one it does not name.
- Delivery stops being a log line. `send_duration_seconds` is the metric that decides ADR 0002
  Decision 5: once a provider round trip puts p95 in the hundreds of milliseconds, batching the
  provider call starts to pay. Per-recipient outcomes also become worth measuring then — the
  current `processed_total` counts *events*, and one rejected address must not be indistinguishable
  from a whole slot failing.
- A second `EventKind` is added. Check every dashboard query aggregates over `kind` rather than
  assuming one series.