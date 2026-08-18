# ADR 0009 — Login attempts are rate limited in memory, by account and by address

- **Date:** 2026-08-17
- **Status:** Accepted
- **Scope:** `internal/ratelimit` (`Limiter`, `Config`, `Decision`, `bucket`, `store`, `Metrics`),
  `internal/auth` (`Handler.checkRateLimit`, `Handler.recordLoginSuccess`, `Handler.renderError`,
  `ErrNilRateLimiter`, `auth.error.rate_limited`), `internal/web` (`ClientIP`,
  `SetRateLimitHeaders`, `SetRetryAfter`, `RetryAfterSeconds`, `NewTooManyRequestsProblem`),
  the `login` subsystem in `internal/metrics`, and the `LOGIN_RATE_LIMIT_*` variables in
  `cmd/server/config.go`

## Context

ADR 0008 bounded what a login burst can cost by making the Argon2 semaphore queue instead of
reject. It closed with the explicit note that it did not fix the underlying problem, and named the
missing piece: nothing stopped an attacker from *making* the attempts in the first place. Every
attempt burned a hashing slot even with garbage credentials, because the unknown-email branch still
compares against `dummyHash` to mask timing.

Two attacks were open. One host working through many accounts, and many hosts converging on one
account — distributed credential stuffing. The second is the one that matters here: the clinic has a
handful of accounts, so an attacker who knows the assistant's email address needs to guess only a
password, and can spread the guessing across as many addresses as it cares to rent.

This is also being built under a constraint that is not obvious from the code: **there is no
password-reset flow**, and creating the first assistant is a manual `INSERT`. Any mechanism that can
lock an account out has no escape hatch. That single fact drives Decisions 3, 4 and 5.

## Decision 1 — A token bucket, not a fixed or sliding window

Three models were on the table.

A **fixed window** ("N attempts per 15 minutes") is the cheapest, and has a boundary burst: N at
14:59:59 and N more at 15:00:01 is `2N` attempts in two seconds. When the thing being protected is
64 MiB of Argon2 work behind two slots, that burst is precisely the attack.

A **sliding window log** is exact, and stores one timestamp per attempt. Its per-key memory is
chosen by the sender, which is the wrong property for a structure the sender also populates.

A **token bucket** is two words per key — a `float64` and a `time.Time` — no matter how much traffic
that key sees, and its burst-then-drip shape matches the behaviour worth allowing: a person
mistyping a password a few times in a row, then slowing down. It also never *locks*. It degrades to
a drip and heals on its own, which is the only acceptable failure mode given no password reset.

The header arithmetic falls out of it directly, and lives in `bucketConfig`:

```
Remaining   = floor(tokens)
Retry-After = ceil((1 - tokens)     * refill)
Reset       = ceil((burst - tokens) * refill)
```

`refill` is the time to earn back **one** token, not a window length. Rounding is always up, so a
caller that waits exactly as long as it was told never returns a fraction of a second too early.

## Decision 2 — Two keys, and the account key is the one that justifies this existing

`Allow` takes both a `netip.Addr` and an account, and both must have a token.

The address key stops one host working through many accounts. The account key stops many hosts
converging on one account. **Only the second requires the application.** A reverse proxy sees
addresses and nothing else, so an IP-keyed edge limiter — Caddy's, Cloudflare's, anyone's — is
structurally blind to distributed stuffing against a single username. That is the whole reason this
lives in `internal/auth` rather than being deferred to the edge, and it is why
`appt_login_rate_limited_total{scope="account"}` rising while `scope="ip"` stays flat is worth an
alert: it is a signature nothing upstream can produce.

The account key is a SHA-256 of the address lowercased and trimmed. Folding case and space stops
`  USER@x.com ` from buying a second allowance for the same account. The digest is **not** a
security boundary — anyone holding the address recomputes it in a line — it is there because the
value arrives from a request body bounded only by `maxBytesReader = 1 << 20`, and an unhashed key
would let a sender choose 1 MB map keys. It also keeps the address out of the limiter's state.

## Decision 3 — A refused attempt is charged nothing, and both buckets are charged or neither is

Both halves are load-bearing and neither is visible from reading `Allow` casually.

**Refusals cost nothing.** `Allow` decides against both buckets first and only calls `consume` once
both have agreed. Charging a refused attempt would let a caller that is already over the limit hold
its own bucket empty with the very requests being refused it, so the limit would never lift — not
for the attacker, and not for the legitimate user sharing that account.

**Both or neither.** If the address bucket refuses but the account still had room, the account is
not charged. Otherwise one blocked address could walk a list of usernames and drain every account's
budget on the way, turning one refused attacker into a lockout of every account it chose to name.

A third case — the attempt was charged but never reached the password check at all — is Decision 10.

`bucket.decide` and `bucket.headroom` are separate for a related reason: the verdict is about the
request being paid for, while the headers describe what is left after paying. Deriving the verdict
from the post-consume state refuses the very request that spent the last token. That was a real bug
during implementation, caught by `TestAllowSpendsTheAccountBurstThenRefuses`.

## Decision 4 — A successful login refills the account, and rewrites the headers it already sent

`RecordSuccess` puts the account bucket back to **full** and refunds one address token. Full, not a
single refund: someone who mistypes their way to the limit and then gets it right must not be left
rationed, because there is nothing they can do about it but wait.

It returns a `Decision`, which looks redundant until you follow the order of writes. `checkRateLimit`
charges the attempt and writes `X-RateLimit-*` before the password is checked, because it must
answer refusals before reaching Argon2. On the success path that header is stale by the time the
response is written — it advertises the allowance as it stood mid-request, not the refilled one. The
handler rewrites it from the returned `Decision`. Without that, a successful login reports
`Remaining: 0` on an account that is actually full.

## Decision 5 — In-memory, per process, and what that costs

State is a mutex and two maps. It does not survive a deploy and does not span replicas.

Both are acceptable **today** and neither is free. A deploy hands every attacker a fresh allowance;
with a single replica behind Caddy and deploys measured in weeks, that is a worse trade to fix than
to accept. A second replica would halve the effective limit, silently. The trip-wire is in
*Revisit when*.

The entry cap is what bounds memory, not the sweep. Keys are chosen by whoever is sending requests,
so an unbounded map would be the denial of service this exists to prevent. `store` is an LRU —
`container/list` beside the map — so eviction at the cap is O(1) rather than a scan of 10 000
entries performed at exactly the moment the process is under attack. `DeleteExpired` runs as a
`worker.JobFunc` beside `expire-sessions` and drops entries whose bucket has refilled completely; a
full bucket is indistinguishable from the one a new caller would be handed, so forgetting it changes
no decision. That sweep is hygiene. Remove it and the memory bound still holds.

**Eviction can forget a key that still owed time.** An attacker who pushes 10 000 invented accounts
through could reset a real victim's drained bucket. What throttles that today is the *other*
dimension: an invented account is only inserted when the attempt naming it was granted, so reaching
the cap costs one address token per entry and 20 does not go far. That is why `Allow` weighs the
address bucket **first** and returns through `refusedByAddress`, which reads an already tracked
account but inserts nothing: an inserting lookup on the refused path would have let a blocked
address evict the whole map at whatever rate it could send, with its own budget already at zero and
so throttling nothing. It is an indirect defence, which is why
`appt_login_rate_limiter_evictions_total` is expected to sit at zero and is alerted on above it.

## Decision 6 — The client address is trusted because of the deployment, and Cloudflare breaks that

`web.ClientIP` prefers `X-Real-Ip` over the TCP peer, and returns `netip.Addr` rather than a string
so an unparsed header cannot become a map key. It normalises: the zone is stripped and IPv4-mapped
IPv6 is unwrapped, or `203.0.113.7` and `::ffff:203.0.113.7` would be two allowances for one client.

The header is trustworthy only because of how this is deployed. The container publishes no host
ports (`docker/docker-compose.yml` uses `expose:`, not `ports:`), so the only ingress is Caddy, and
the Caddyfile overwrites the header with its own view of the peer. A client cannot set it.

**That stops holding the moment the zone is proxied through Cloudflare, and the fix is outside this
repository.** Caddy's `{remote_host}` becomes a Cloudflare edge address, so `header_up X-Real-IP
{remote_host}` would forward Cloudflare's address and the limiter would throttle the CDN instead of
the attacker — one bucket for the entire internet. Before the DNS record is flipped to proxied, the
Caddyfile must set `trusted_proxies` to Cloudflare's ranges and forward `{client_ip}`. No Go code
changes; no test can catch this.

In development there is no proxy at all: `docker-compose.dev.yml` runs Postgres only, the app runs on
the host, no header arrives and `RemoteAddr` is used. Nothing needs configuring.

## Decision 7 — The edge layer stays with Caddy; Cloudflare's free tier cannot do it

The plan of record was a coarse per-IP limit at Cloudflare instead of rebuilding Caddy with
`xcaddy --with github.com/mholt/caddy-ratelimit`. Checked against Cloudflare's own documentation on
2026-08-15, the free tier gets **one** rate limiting rule, with the counting period fixed at 10 s,
the mitigation timeout fixed at 10 s, and IP-only counting. The only expressible rule is "more than
N requests to `/login` from one address in 10 seconds, blocked for 10 seconds" — a volumetric flood
cutter that anyone pacing under the threshold never trips. "Five attempts per three minutes" is not
expressible below Pro (1 min / 1 hour) or Business (10 min / 1 day).

So the edge layer remains a Caddy rebuild, and it remains *not done*. What it still buys, now that
this ADR covers both keys, is no longer coverage but the **cost** of a refusal: a refusal here has
already paid for a TLS handshake, the full middleware chain, a body read and a template render,
where one at the edge pays for none of it.

The "attacker finds the origin address and bypasses the proxy" case is closed separately: the VPS
firewall accepts only Cloudflare's ranges on 443.

## Decision 8 — Environment variables here, unlike `maxQueueWait` in ADR 0008

ADR 0008 Decision 3 made `maxQueueWait` a constant and argued that a value derived from a machine
constraint has no business varying per deployment. These are the opposite kind of number: a
judgement about how much retrying is normal for real people, which nobody can get right before
watching real usage. They are `LOGIN_RATE_LIMIT_*` so they can be retuned without a release.

`LOGIN_RATE_LIMIT_ENABLED` defaults to **true** when unset. A missing variable must never read as
"protection off". A variable that is set but malformed stops the process, as everywhere else in
`cmd/server/config.go`, and `Config` is validated even when the limiter is disabled, so enabling it
later cannot surface a misconfiguration that was there the whole time.

## Decision 9 — `X-RateLimit-*` are de facto, and `Reset` is delta-seconds

`Retry-After` is standard (RFC 9110 §10.2.3) and `429` is RFC 6585 §4. The three `X-RateLimit-*`
fields are not standard at all: the IETF draft in flight
(`draft-ietf-httpapi-ratelimit-headers`, revision 11 as of May 2026, still an Internet-Draft)
specifies `RateLimit` and `RateLimit-Policy` structured fields instead. These were chosen because
they are what clients actually recognise today; adding the draft fields later is additive.

`Reset` carries delta-seconds rather than an epoch timestamp, so it shares a unit with `Retry-After`
and does not depend on the client's clock agreeing with the server's. `Retry-After` is never zero:
telling a caller to retry immediately invites a retry certain to be refused again.

Advertising `Remaining` on a login endpoint does help an attacker pace itself. That is accepted: the
information is recoverable by counting refusals anyway, and the alternative punishes only the honest
client that would have backed off.

## Decision 10 — An attempt that never reached the password check gets its account token back

`checkRateLimit` charges before `verifyCredentials`, which is the whole point (Decision 3): a refused
attempt must cost no Argon2 slot. But that ordering charges attempts that then fail for reasons
carrying no signal about the credentials at all — `errCredentialLookupFailed` when the database is
unreachable, `password.ErrTooManyConcurrentHashes` when the hashing queue is full (ADR 0008), and
`errPasswordCheckFailed` when the comparison itself breaks. None of those compared a password.
`RecordAbandoned` hands the token back.

Without it, a user who hits five 503s during a hashing-queue burst is locked out for three minutes
per retry, for an outage that is ours — and Decision 4's reasoning applies with more force here,
because there is still no password-reset flow to escape through.

**Only the account is refunded, and only by one token.** By one and not to full, because unlike
Decision 4 the attempt proved nothing: it earns its charge back and no more. The account and not the
address, because whoever is driving the load that emptied the hashing queue is exactly who the
address budget exists to slow down — refunding it would let a flood hold its own budget open during
the outage it is causing. The asymmetry mirrors `RecordSuccess`, which fills the account but only
refunds the address.

A caller who can *cause* these failures gains nothing from the refund: a request that never reached
the comparison leaks no information about whether the password was right, which is the only thing
the account budget is rationing.

## What this does not fix

- **Malformed bodies are free.** The limiter runs after `parseLoginForm` / `DecodeJSON`, so a
  request with a broken body is refused with a 400 before any token is charged. It reaches no Argon2
  slot either, so it is cheap — but it is unbounded, and only an edge layer caps it.
- **Distributed attempts under the threshold.** An attacker willing to spend one guess per account
  per three minutes is not slowed by this. Nothing short of MFA is.
- **No CAPTCHA and no MFA.** OWASP treats rate limiting as one leg of three; the other two are not
  implemented.
- **The edge layer is still missing** — see Decision 7.
- **Password management is still absent** (production blocker 2): no reset, no change, and
  `validateCreateInput` accepts `"a"`. Decisions 3, 4 and 10 are shaped by that absence and should be
  revisited when it is fixed.

## Revisit when

- A second replica is deployed, or deploys become frequent. Per-process state stops being
  equivalent to per-service state, and the limiter would need to move to Postgres — the
  `session.PostgresRepository` migration in ADR 0006 is the precedent.
- `appt_login_rate_limiter_evictions_total` moves off zero. Either the entry cap is too low or the
  map is under deliberate pressure; see Decision 5 for why eviction is not harmless.
- `appt_login_rate_limited_total{scope="account"}` rises while `scope="ip"` stays flat. That is
  distributed credential stuffing in progress, and the thresholds in Decision 8 are the lever.
- A password-reset flow lands. Decision 4's refill-to-full, Decision 10's refund and the whole
  refusal-to-lock-out stance are calibrated to there being no escape hatch; with one, a stricter
  account limit becomes defensible.
- The zone is moved behind a Cloudflare proxy — see Decision 6, and do it *before* the DNS change.
- The Cloudflare plan is upgraded to Pro or above, which would make Decision 7 worth reopening.
