# ADR 0010 — The assistant resets their password through a mailed single-use link

- **Date:** 2026-08-20
- **Status:** Accepted
- **Scope:** `internal/mailer` (`Client`, `Config`, `Message`, `NewClient`, `VerifyConnection`,
  `Send`, `tlsPolicy`, `containsLineBreak`, and the sentinels in `errors.go`),
  `internal/passwordreset` (`Store`, `Storer`, `PostgresRepository`, `Create`, `Verify`,
  `Consume`, `DeleteExpired`), `internal/password` (`Validate`, `MinLength`, `MaxLength`,
  `ErrPasswordTooShort`, `ErrPasswordTooLong`), `internal/auth` (`ResetHandler`,
  `ResetHandlerConfig`, `Mailer`, `ResetRepo`, `ResetMetrics`, `dispatch`, `sendResetLink`,
  `resetMessage`, `resetURL`, `rejectWeakPassword`, and the package-level render helpers in
  `render.go`), `internal/session` (`DeleteByAssistant`), `internal/assistant`
  (`UpdatePasswordHash`, `ErrEmptyPasswordHash`), `internal/ui/auth` (`ForgotPassword`,
  `ResetPassword`, `ResetPasswordExpired`, `FormError`), `cmd/resetpassword`, `cmd/server`
  (`initializeMailer`, `parseAppBaseURL`, `parseSMTPConfig`, `parsePasswordResetTokenTTL`,
  `parsePasswordResetRateLimit`, and the `APP_BASE_URL` / `SMTP_*` / `PASSWORD_RESET_*`
  variables), `internal/db/migrations/000013_password_reset_token`, and the `password_reset`
  subsystem in `internal/metrics`

## Context

Until now this application had no way to recover an account. The assistant row is created by a
manual `INSERT`, there was no reset, no authenticated change-password screen, and no mail
transport anywhere in the tree. Forgetting the password meant losing the appointment book.

That absence was not only a missing feature — it **shaped the design of other parts of the
system**. ADR 0009 states outright that its Decisions 3, 4 and 10 are calibrated on the fact that
*"there is no password-reset flow"*, and lists "A password-reset flow lands" as a trigger to
revisit. The same claim is asserted in `internal/ratelimit/limiter.go`, in `cmd/server/config.go`
and in the message of an integration test. ADR 0009 also names "Password management" as
production blocker 2.

This ADR records the flow that closes it, and the reasoning behind the parts of it that the code
cannot explain on its own. Fourteen comments across nine files defer to this document; each
decision below is what one or more of them points at.

The scope is deliberately narrow: **reset only**. Changing a password while logged in is a
different flow with a different threat model (it can demand the current password, and it does not
need mail at all) and is not built here.

## Decision 1 — A link with a token, not a one-time code

The mail carries `{APP_BASE_URL}/reset-password?token=…` rather than a short numeric code.

The token is 32 bytes of `crypto/rand` rendered with `base64.URLEncoding`. At 256 bits, guessing
is not a threat model — which is what removes the need for a per-token attempt counter, a lockout,
or any of the machinery a 6-digit code would demand. What the database stores is
`hex(sha256(token))`, never the token itself: a stolen dump cannot be replayed, exactly as with
session cookies in ADR 0006.

Expiry is absolute, written once at creation, defaulting to 30 minutes. Creating a token deletes
the assistant's previous ones, so at most one link is ever live and asking again invalidates the
old mail.

**The cost, accepted knowingly:** a token in a URL is written to the browser's history and to
Caddy's access logs. That is tolerable here — the logs are ours, on our own VPS, the token is
single-use and dies in 30 minutes — and it is mitigated with `Referrer-Policy: no-referrer` on
both reset routes, so the token is not handed to whatever the page links out to. A one-time code
posted into a form would avoid this, at the price of the attempt-counting machinery above. The
trade was made in favour of the simpler mechanism.

## Decision 2 — The neutral answer goes out before any work is done

`POST /forgot-password` writes the same message for every address and *then* looks the account up,
on a goroutine detached from the request.

The ordering is the anti-enumeration mechanism, not a nicety. Had the handler looked up the
assistant, created a token and sent a mail before answering, a registered address would have cost
hundreds of milliseconds more than an unregistered one — and that difference is a reliable oracle,
regardless of how identical the response body is. Answering first makes the timing constant by
construction rather than by trying to match two code paths.

The detached context is `context.WithTimeout(context.WithoutCancel(r.Context()), 45s)`. Each
piece is load-bearing:

- `WithoutCancel` keeps the request's values — the resolved locale, so the mail is written in the
  language the requester was using, and the trace ids, so the send is still correlated — while
  dropping the cancellation that fires the moment the response is written.
- The explicit timeout exists **because** `WithoutCancel` produces a context with no deadline of
  its own. Without it nothing would ever end this context; see also Decision 9.
- 45s is above `mailer.Send`'s own 30s so the inner budget is the one that expires first and
  produces the specific error.

The goroutine is registered on a `sync.WaitGroup` that the shutdown drains before closing the
pool, on the same principle as the notification drain — a mail in flight is not cut off by a
deploy.

An address with no account is treated as success, not as an error: there is nothing to do, and the
caller was answered either way.

## Decision 3 — `GET /reset-password` verifies, only `POST` consumes

`Store.Verify` checks that the token exists and has not expired. `Store.Consume` deletes the row
with `DELETE … RETURNING assistant_id` and hands back the assistant.

The split is why the `GET` exists at all. Its job is to refuse early: nobody should choose a
password, type it twice and submit it only to be told the link died twenty minutes ago. A spent or
expired token renders `ResetPasswordExpired()` with a route back to `/forgot-password` instead of
a form.

It also means following the link is safe to do more than once. Mail clients, antivirus scanners
and link-preview bots issue `GET` requests on their own; if the `GET` consumed the token, an
overzealous scanner in the recipient's own mail provider would burn the link before the person
ever clicked it. Consumption is bound to the `POST`, which those clients do not make.

`Consume` deleting and returning in one statement is what makes single-use atomic — no transaction,
and no window between checking and spending in which a second request could slip through.

## Decision 4 — Sessions are cleared before the new hash is written

The confirm step runs: validate → `Consume` → `Hash` → `DeleteByAssistant` → `UpdatePasswordHash`.

The order of the last two is a security decision, not an accident. Either write can fail
independently, so the question is which partial state is survivable:

- **Sessions first, hash second** — if the update fails, the account is logged out everywhere and
  the old password still works. Annoying; recoverable by trying again.
- **Hash first, sessions second** — if the delete fails, the password has changed while a session
  an attacker may be holding stays valid. The legitimate owner no longer knows the password that
  session was born from, and cannot end it.

The second is the one that must never happen, so the cheap failure is the one that is allowed to.
`cmd/resetpassword` follows the same order for the same reason.

Password validation runs *before* `Consume`, so a rejected password costs a retry on the same link
rather than a whole new mail.

## Decision 5 — A completed reset does not log anyone in

The confirm step answers `HX-Redirect: /login` and sets no cookie.

Proving control of a mailbox is not the same as proving control of the account, and the two should
not be conflated by a convenience. It also means the freshly chosen password is exercised
immediately, while the person still has it in front of them, instead of surfacing as a surprise on
the next visit.

## Decision 6 — The reset origin comes from configuration, never from the request

`APP_BASE_URL` is read at startup, validated to be `http`/`https` with a host, and is the only
source for the link in the mail.

Deriving it from the `Host` header would be a host-header injection: an attacker sends
`POST /forgot-password` with `Host: evil.com` for the assistant's address, the mail is generated
and delivered legitimately, and it carries a link to the attacker's server. The recipient has
every reason to trust that mail — it is the one they asked for — and hands over a valid token.

There is no default. A missing value stops the process; see Decision 8 for why that was chosen
over booting without the feature.

## Decision 7 — Password reset gets its own rate limiter

A second `ratelimit.Limiter` is constructed, with its own configuration, rather than sharing the
login one.

Sharing has two concrete failures, not one:

- Reset requests would spend the assistant's **login** allowance, so asking for a reset would help
  lock them out of the very thing they are trying to reach.
- ADR 0009 Decision 4 refills an account to full on a successful login. That refill would wipe the
  reset budget, so the limit would evaporate for exactly the caller who just proved they can log
  in — and an attacker with valid credentials is not the one it is defending against anyway.

The thresholds are tighter than login's (3 per account refilling every 15 minutes, 10 per address
refilling every 5 minutes) because the honest usage pattern is different: a person asks for a
reset once, reads their mail, and is done. They share `LOGIN_RATE_LIMIT_MAX_ENTRIES` for the map
bound, since that number is about memory, not about either policy.

`POST /reset-password` hashes with Argon2id and therefore inherits the 2-slot / 3s queue from
ADR 0008. It maps `password.ErrTooManyConcurrentHashes` to `503` with the `auth.error.busy`
catalog key, identically to login: a saturated hasher is a capacity problem, and telling the
caller their token was bad would be a lie.

## Decision 8 — The mailer's constructor performs no I/O, and an unreachable relay is not fatal

`mailer.NewClient` validates configuration and builds a client. `VerifyConnection` is separate,
called once at startup, and its failure is logged rather than returned.

This started as the opposite. The original design gated the whole feature behind an `SMTP_ENABLED`
boolean and dialled the relay inside the constructor, so a relay that was down stopped the
process. Both were dropped, for reasons worth recording because they are not visible in the
result:

**`SMTP_ENABLED` was removed** because the repository already had a convention for optional
services and it was not a boolean: `STORAGE_ENDPOINT` being absent is what disables object
storage. A second mechanism for the same idea is a mechanism to keep in sync.

**Dialling in the constructor was removed** after testing what a `gomail.Client` actually holds.
It holds no persistent connection: every `Send` dials, delivers and closes. This was confirmed
directly — with the relay down at boot, `Send` fails; bring the relay up with the process still
running, and the next `Send` succeeds and the mail is delivered. There is therefore no connection
whose health at startup predicts anything, which is the opposite of the Postgres pool, where the
boot-time ping is checking the thing that will actually be used.

That leaves `VerifyConnection` as what it honestly is: a **configuration smoke test**. It catches a
typo in the host or credentials at boot rather than at the first reset, hours later, in a code path
nobody is watching. It is not a health gate, is not part of `/readyz`, and carries no metric —
`appt_password_reset_mail_total{outcome="failed"}` is the signal that matters, because it reflects
real sends.

So the split is: **a bad configuration stops the process, an unreachable relay does not.** A
malformed config is a mistake that will never fix itself, and failing loudly at deploy time is
strictly better than discovering it later. A relay that cannot be reached is a transient condition
the flow recovers from on its own — and taking the appointment book offline over it would trade a
degraded recovery path for a total outage.

The consequence, chosen deliberately: `APP_BASE_URL`, `SMTP_HOST` and `SMTP_FROM_ADDRESS` are
**required**, and the server refuses to start without them. Password reset is now the only
in-band recovery path, and a server that booted quietly without it would leave the account
unrecoverable while reporting itself healthy. `cmd/resetpassword` (Decision 15) is the backstop for
the case where mail genuinely is not available.

## Decision 9 — `Send` imposes its own deadline

`Send` wraps the caller's context in a 30s timeout (10s of it for the dial) instead of trusting
whatever it was handed.

This application has **no request-deadline middleware**. `http.Server.WriteTimeout` stops writing
to the socket, but it never cancels `r.Context()`, so a context that came from a request carries no
deadline at all. Every blocking dependency therefore has to define its own budget or risk hanging
forever. The reset dispatch (Decision 2) is a second instance of the same rule, and adding a
general deadline middleware is on the README's TODO list precisely because these budgets are
currently scattered.

## Decision 10 — TLS is mandatory or absent; opportunistic is refused

`tlsPolicy` maps `UseTLS` onto `TLSMandatory` or `NoTLS`. `gomail.TLSOpportunistic` is never
returned.

Opportunistic STARTTLS negotiates encryption if the relay offers it and **silently falls back to
plaintext if it does not**. That is the worst available failure mode: it looks like the secure
option, produces no error, and a relay that stops advertising STARTTLS — or an attacker who strips
the advertisement — downgrades the connection with nothing in the logs. Mandatory fails loudly
instead, which is the behaviour worth having.

`SMTP_USE_TLS=false` exists only for a local catcher such as Mailpit. Against a remote relay it
puts the credentials on the wire in the clear; the comment on `Config` says so, and this is the
justification behind it.

## Decision 11 — Header fields are checked for line breaks even where the parser already rejects them

`Message.validate` rejects a CR or LF in `To` or `Subject`, on top of parsing `To` with
`net/mail.ParseAddress`.

A line break ends one header field and begins another. An unsanitised value can therefore append a
`Bcc:` of the attacker's choosing to a message the application believes it controls. **`Subject` is
what this check actually guards** — nothing else validates it, and it is the field carrying
translated text.

`To` is covered too, even though `mail.ParseAddress` was verified to reject every CR/LF form on its
own. That is defence in depth, held deliberately rather than inherited: the guarantee currently
comes from a standard-library parser whose exact strictness is not part of this application's
contract, and one explicit check is cheaper than a dependency on that behaviour never loosening.

Validation accumulates into `errors.Join` rather than returning at the first problem, matching
`internal/slot` and `internal/mailer/config.go`, so a caller sees every defect at once. Parsing
runs only when the address is non-empty, so a missing recipient is one error and not two.

## Decision 12 — Only length is validated, and the maximum is not a CPU defence

`password.Validate` accepts 12 to 128 **runes**. There are no composition rules — no required
digit, no symbol, no mixed case.

Composition rules are counterproductive and have been dropped from the guidance that originally
promoted them (NIST SP 800-63B): they push people towards predictable substitutions and towards
writing passwords down, while adding little entropy. The 12-character floor follows OWASP ASVS and
is the constraint that actually matters.

Counting runes rather than bytes is deliberate: four emoji are sixteen bytes but four characters,
and a byte-based minimum would accept a four-character password.

**The maximum is a sanity bound, not a defence against expensive hashing.** It is worth stating
plainly because the opposite is a natural assumption and is wrong: Argon2id's cost is fixed by its
parameters — 64 MiB, 3 passes, 1 lane (ADR 0008) — and does not grow with the length of the input.
A 128-character password costs exactly what a 12-character one costs. The bound exists to keep an
absurd input from travelling through the system, nothing more.

`assistant.Create`'s `validateCreateInput` still accepts `"a"`, as ADR 0009 noted. This does not
fix that; it only ensures the new path does not repeat it.

## Decision 13 — `password` and `passwordreset` stay separate packages

`internal/password` owns the primitive: hashing, comparison, the semaphore, and now the length
rule. `internal/passwordreset` owns the token lifecycle backed by Postgres.

The obvious objection is that both are "about passwords" and could be one package with more files.
They are kept apart because their dependencies differ in kind, not in degree. `password` depends on
`golang.org/x/crypto` and nothing else — no database, no clock, no context beyond cancellation —
which is what lets it be tested as pure computation and reused by `cmd/resetpassword` without
dragging a connection pool along. `passwordreset` is persistence: a `Storer` interface, a
`PostgresRepository`, expiry, a sweep job shaped as `worker.JobFunc`.

Merging them would put a `*pgxpool.Pool` in the import graph of the package that hashes passwords,
for no gain. The split mirrors `session`, which is the same shape and the precedent this was
modelled on.

## Decision 14 — `internal/notification` is not reused

Routing reset mail through the existing notification queue was considered and rejected. Four
independent reasons, any one of which would be sufficient:

- **`Event` carries a `SlotID`.** A reset has no slot. Widening the struct is not free: ADR 0002
  keeps it pointer-free deliberately and load-bearingly.
- **The queue drops events when its buffer fills** (the `default:` arm of its `select`). A missed
  appointment reminder is degraded service; a dropped reset mail is the feature not working. The
  reliability contracts are different.
- **It resolves recipients and content at delivery time**, on purpose, so nothing stale is sent. A
  reset token is generated once and cannot be re-derived later.
- **Its delivery step is still a placeholder**, and ADR 0003 — which would make it durable — is
  `Proposed`. `AGENTS.md` forbids building on a `Proposed` ADR without asking.

`internal/mailer` is therefore an independent package with no knowledge of resets, so that when
notification delivery becomes real it can consume it unchanged.

## Decision 15 — The offline command talks to the database directly and applies no migrations

`cmd/resetpassword` opens the pool with `pgxpool.New` rather than `db.NewPostgresPool`.

`db.NewPostgresPool` runs the embedded migrations as part of connecting. That is right for the
server and wrong here, twice over. A rescue command invoked with `docker exec` may be running a
different image version than the server, and it has no business moving the schema. Worse, a
previously interrupted migration leaves the schema-version table marked dirty, and `runMigrations`
then fails — meaning the emergency escape hatch would stop working in precisely the situation that
made it necessary. What is given up is the pool-sizing validation and the `timezone=UTC` runtime
parameter, and neither applies: the command passes no pool options and reads no timestamps.

Three further properties of the command are deliberate:

- **No `-password` flag.** The password is generated from `crypto/rand` and printed once, so it
  never reaches shell history or another user's `ps`. It goes to stdout alone, with diagnostics on
  stderr, so the stream can be captured directly; nothing is printed if any step fails.
- **It reuses `password.Argon2` rather than hashing independently.** This is not tidiness. A hash
  written with different parameters would be rejected by the login forever, and the command would
  report success — a silent lockout produced by the tool meant to prevent one.
- **It trims the address and nothing else.** The lookup is a raw `WHERE email = $1` and the login
  does not fold case either, so folding it here would make the command find accounts the login
  cannot. The `assistant` table's `chk_assistant_email_lowercase` constraint guarantees stored
  addresses are already lowercase, so the gap is only on input, and closing it belongs in a change
  that fixes every entry point at once.

## What this changes about ADR 0009

ADR 0009's Decisions 3, 4 and 10 — never lock an account out, refill to full on a successful
login, refund an attempt that never reached the password check — are all justified there by the
absence of an escape hatch. That premise no longer holds.

**No rate-limiting behaviour is changed by this ADR.** What changes is the strength of the
argument: those decisions were *necessary* and are now merely *conservative*. A stricter account
limit has become defensible, since an assistant who locks themselves out can now recover by mail
or, failing that, by `cmd/resetpassword`.

ADR 0009 is not edited — it records the state in which its decisions were taken, and rewriting that
would erase the reasoning. The three comments in the code that asserted the absence
(`internal/ratelimit/limiter.go`, `cmd/server/config.go`, and an assertion message in
`internal/auth/ratelimit_integration_test.go`) are corrected, because those describe the present.

## What this does not fix

- **There is still no authenticated change-password screen.** Someone who knows their password and
  wants a different one has to go through the mailbox. Deliberately out of scope.
- **`assistant.Create` still accepts a one-character password.** Decision 12 covers the reset path
  only.
- **Email lookups are case-sensitive on input** — see Decision 15. Login has the same gap and both
  must be fixed together.
- **Reset mail delivery is unmonitored beyond a counter.** `appt_password_reset_mail_total` records
  whether `Send` returned an error. It cannot know whether the message was accepted by the
  recipient's server, filed as spam, or silently dropped downstream.
- **The token is still in the URL** — Decision 1 accepts the browser-history and access-log
  exposure rather than eliminating it.
- **No MFA.** A mailbox is a single factor, so the reset path is exactly as strong as the
  assistant's mail account.

## Revisit when

- **A second replica is deployed.** The reset rate limiter is per-process, exactly like the login
  one, so ADR 0009's Decision 5 applies here unchanged.
- **`appt_password_reset_mail_total{outcome="failed"}` moves off zero.** That is the only signal
  that the flow is broken: the requester is answered identically whether or not the mail went out,
  so a relay the application cannot reach is otherwise invisible.
- **An authenticated change-password screen is added.** Decision 5's refusal to log the user in and
  Decision 4's session clearing should be reconsidered together with it, since that flow already
  has an authenticated session and no reason to destroy it.
- **Notification delivery becomes real** (ADR 0003 leaving `Proposed`). Decision 14's reasons should
  be re-read then; the package boundary was drawn so that reset mail could keep its own path.
- **Mail volume grows beyond password resets.** `Send` dialling per message is fine at this volume
  and would not be for a campaign.