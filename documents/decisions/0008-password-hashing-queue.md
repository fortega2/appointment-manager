# ADR 0008 — The Argon2 concurrency semaphore queues, it does not reject

- **Date:** 2026-08-11
- **Status:** Accepted
- **Scope:** `internal/password` (`Argon2`, `acquire`, `maxConcurrentHashes`, `maxQueueWait`,
  `Metrics`, `ErrTooManyConcurrentHashes`), the `password` subsystem in `internal/metrics`, and the
  `assistant.Hasher` interface

## Context

`internal/password` caps concurrent Argon2id work at `maxConcurrentHashes = 2`, because each hash
allocates 64 MiB. The cap itself was never in question. What it did on saturation was:

```go
select {
case a.sem <- struct{}{}:
	return nil
default:
	return ErrTooManyConcurrentHashes
}
```

Three simultaneous `POST /login` requests meant the third got a 503 — on an otherwise idle server,
for work that takes about 100 ms. The clinic this runs for sits behind one NAT'd connection; three
people signing in at the start of a shift is the normal case, not an attack. This was found during
the pre-production readiness audit and classified as a correctness bug rather than a rate-limiting
gap, which is the distinction the rest of this ADR turns on.

## Decision 1 — Waiting for a slot is free; rejecting was never buying anything

The semaphore exists to bound peak memory. Rejecting a caller and queueing a caller bound it
*identically*: the 64 MiB is allocated by `argon2.IDKey`, which only runs once a slot is held. A
waiting caller costs a blocked goroutine and nothing else.

So the rejection bought no memory safety. What it cost was availability under ordinary load. The
`default:` arm is now a `case <-ctx.Done():`, and the wait is real:

```go
select {
case a.sem <- struct{}{}:
	a.metrics.ObservePasswordQueueWait(ctx, time.Since(start))
	return nil
case <-ctx.Done():
	a.metrics.RecordPasswordQueueTimeout(waitFailureReason(ctx.Err()))

	return fmt.Errorf("%w: %w", ErrTooManyConcurrentHashes, ctx.Err())
}
```

Go's runtime serves blocked channel senders in FIFO order, so the queue cannot starve a caller
under sustained load. `golang.org/x/sync/semaphore` documents that guarantee explicitly rather than
leaving it to the runtime, and was considered — it was rejected because it is currently an indirect
dependency and buys nothing else a buffered channel does not already provide.

## Decision 2 — Contrast with ADR 0002: two semaphores, opposite policies, both right

ADR 0002 Decision 2 says the notification queue **drops on a full queue and never blocks**. This
ADR says the hashing semaphore **blocks and never drops**. A reader finding both will reasonably
suspect one is wrong. They are not; the two sit on opposite sides of the request boundary.

A notification is fire-and-forget background work. Its producer is a request that has already done
its real job — cancelling a slot — and making that request wait on a notification queue would
charge the user for work they did not ask for and will never see. Dropping is the cheap, correct
answer, and the drop counter makes it visible.

A password hash *is* the request. The user is already waiting on it; there is nothing to protect
them from by failing early. Failing fast does not return the user to a useful state, it just
returns them to the login form to try again — generating another hash attempt. Rejection here is
not backpressure, it is a retry amplifier.

The rule, stated once so a third semaphore does not have to re-derive it: **queue synchronous work
the caller is already blocked on, drop asynchronous work the caller does not observe.**

## Decision 3 — `maxQueueWait` is 3 s, derived from the server's `WriteTimeout`

The wait is bounded twice: by the caller's context, and by `maxQueueWait = 3 * time.Second`.

The context bound alone is not enough, and the reason is easy to miss. Nothing in the request path
sets a deadline — there is no `http.TimeoutHandler` and no middleware calling `context.WithTimeout`.
`r.Context()` is cancelled when the *client* disconnects, and that is all. `http.Server.WriteTimeout`
(15 s, `cmd/server/main.go`) does not cancel the request context; it only makes the eventual write
fail. Relying on ctx alone would therefore mean an effectively unbounded queue whose members
outlive the connection they are meant to answer.

**This 3 s is load-bearing and is coupled to a constant in another package.** It must leave room,
inside the 15 s write budget, for the hash itself plus the `GetByEmail` and session-insert round
trips that bracket it. Raising `maxQueueWait` past `WriteTimeout`, or lowering `WriteTimeout` toward
`maxQueueWait`, breaks that relationship silently: the queue would still be waiting when the
response could no longer be written, and the user would see a dropped connection instead of an
error. No test enforces the relationship, because neither package can see the other's constant.
Change one, check the other.

## Decision 4 — Cancellation and timeout share one sentinel; the metric carries the difference

`acquire` returns `ErrTooManyConcurrentHashes` wrapping `ctx.Err()` in both the "budget elapsed" and
"client hung up" cases, so the existing 503 mapping and the `auth.error.busy` catalog key keep
working untouched.

Splitting them into two sentinels was considered and rejected: the HTTP layer cannot act on the
difference. A client that disconnected is not reading the response, so whatever status is chosen
for that branch is written to a dead socket. The distinction only matters to the operator, so it
lives where operators look — the `reason` label on
`appt_password_queue_timeouts_total`, which is `timeout` or `client_cancelled`. Only `timeout`
indicates saturation; `client_cancelled` is ordinary background noise and must not page anyone.

## Decision 5 — `maxConcurrentHashes` stays at 2, and the constraint is CPU

The old comment justified 2 against "the container's ~2-core budget" while `docker/docker-compose.yml`
sets `cpus: '1.5'` and `memory: 3G`. With `memory: 3G`, a memory-derived cap would be far higher
than 2 — at 64 MiB per hash the memory limit alone would permit dozens.

The real constraint is CPU. `GOMAXPROCS=2` is set explicitly in the compose file, and
`defaultParallelism = 1` means one hash occupies one thread, so two concurrent hashes already
saturate the runtime's entire scheduling budget. A third would timeslice against the other two,
making every one of them slower without completing any sooner. The cap is unchanged; only its
stated justification was wrong.

## Decision 6 — A queue-wait histogram here, unlike ADR 0004 Decision 6

ADR 0004 Decision 6 rejected a queue-wait histogram for notifications, on the grounds that the
metric would mostly re-derive its own configuration: the drain runs on a ticker, so the wait is
determined by `NOTIFICATION_TICKER_INTERVAL` and the histogram would simply show that interval back.

That objection does not transfer. Hashing has no ticker. The wait is a function of live concurrent
login volume against a budget of 2, which is genuinely unknown and not derivable from any
configured value. It is also the *only* direct evidence for whether `maxConcurrentHashes = 2` and
`maxQueueWait = 3s` are the right numbers for this deployment — the two constants this ADR asserts
without production data behind them.

`appt_password_queue_wait_seconds` puts a bucket boundary at 3 for the same reason
`notificationSendBuckets` puts one at 5: a caller that gave up lands on an edge and reads as its
own step rather than smearing across a wide final bucket.

## What this does not fix

Under a genuine credential-stuffing flood, this converts a fast failure into a slow one: attacker
requests now occupy goroutines for up to 3 s each instead of being refused immediately. That is an
acceptable trade for a fix whose purpose is to stop punishing legitimate bursts, but it is not
itself a defence.

The defences remain outstanding and are tracked as a separate pre-production blocker: per-account
application-level rate limiting on `/login` (per IP is insufficient — distributed stuffing against
one username walks through an IP-keyed limiter), and coarse per-IP limiting at Caddy. Neither is
implemented. Do not read this ADR as closing that item.

## Revisit when

- `appt_password_queue_timeouts_total{reason="timeout"}` is persistently above zero. That means the
  budget of 2 is genuinely too small for real load, not that the wait is too short — check
  `queue_wait_seconds` p95 before touching either constant.
- `GOMAXPROCS` or `cpus` changes in `docker/docker-compose.yml`; `maxConcurrentHashes` is derived
  from the former and should move with it.
- `serverWriteTimeout` changes in `cmd/server/main.go` — see Decision 3.
- Rate limiting lands. Once abusive traffic is refused upstream, the queue only ever holds
  legitimate callers, and `maxQueueWait` could be raised if p95 wait ever approaches it.
