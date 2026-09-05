# Medical Appointment Manager

A production-grade medical appointment scheduling system built entirely in Go. Designed for clinics and private practices to manage professionals, patients, time slots, and appointments with strict business rules, concurrency safety, and a dual REST API + HTML interface.

## Features

- **Professional management** — register, update, activate/deactivate practitioners (currently kinesiology-focused).
- **Patient management** — register and update patients with health insurance details.
- **Time slot management** — create availability blocks with configurable max capacity. Overlapping slots are prevented at the database level using PostgreSQL exclusion constraints.
- **Appointment lifecycle** — book, cancel, and mark appointments as attended. Business rules enforce cancellation windows (cancel before 24h, otherwise mark as absent) and attendance windows (only during the appointment period).
- **Concurrency safety** — all status transitions are atomic with optimistic locking (`UPDATE ... WHERE status = ?`), and slot capacity checks use `SELECT ... FOR UPDATE` within transactions.
- **Authentication** — session-based login for administrative staff (assistants) with Argon2id password hashing.
- **Password recovery** — a single-use link mailed to the assistant, plus an offline CLI for when mail is unavailable. See ADR 0010.
- **Dual interface** — JSON REST API under `/api/v1/` for programmatic access, plus an HTMX-powered HTML interface for day-to-day use.

## Tech Stack

| Layer | Technology |
|---|---|
| Language | Go 1.27 |
| HTTP | `net/http`, `http.ServeMux` — no router frameworks |
| Database | PostgreSQL 18 + `pgx` driver |
| Migrations | `golang-migrate` with embedded SQL |
| Templates | `a-h/templ` — type-safe HTML templates |
| Frontend | HTMX + Alpine.js + Tailwind CSS (self-hosted, pinned versions — no CDN) |
| Auth | Session-based (Postgres-backed, ADR 0006) + Argon2id |
| Container | Multi-stage Docker (scratch-based) + docker-compose |
| Mail | `wneessen/go-mail` over SMTP; Mailpit as the dev catcher |
| Observability | Prometheus metrics + OpenTelemetry tracing |
| Testing | `testify` (assert, require, mock) + `testcontainers-go` |
| Linting | `golangci-lint` with 35+ linters (strict config) |

## Architecture

```
┌─────────────────────────────────────────────────┐
│                HTTP Server (:8080)               │
│  ┌───────────────────────────────────────────┐  │
│  │            Middleware Chain                │  │
│  │  Tracing → Metrics → Logger → RequestID   │  │
│  │           → Gzip → CSRF → Auth            │  │
│  └───────────────────────────────────────────┘  │
│  ┌──────────────────┐  ┌──────────────────┐    │
│  │  REST API         │  │  HTMX UI          │    │
│  │  /api/v1/*        │  │  /appointments,   │    │
│  │  JSON in/out      │  │  /professionals,  │    │
│  │  RFC 9457 errors  │  │  /patients, /slots│    │
│  └──────────────────┘  └──────────────────┘    │
│  ┌───────────────────────────────────────────┐  │
│  │            Service Layer                   │  │
│  │    Appointment  Professional  Patient      │  │
│  │    Slot         Assistant    Auth          │  │
│  └───────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────┐  │
│  │         PostgreSQL Repositories            │  │
│  │       (pgx, transactions, locking)         │  │
│  └───────────────────────────────────────────┘  │
└─────────────────────────────────────────────────┘
```

## Project Structure

```
├── cmd/
│   ├── server/               # Entrypoint, dependency wiring
│   ├── healthcheck/          # Docker HEALTHCHECK CLI
│   └── resetpassword/        # Offline password reset CLI (ADR 0010)
├── internal/
│   ├── appointment/          # Entity, service, REST + UI handlers, repository
│   ├── assistant/            # Entity, service, REST handler, repository
│   ├── professional/         # Entity, REST + UI handlers, repository
│   ├── patient/              # Entity, REST + UI handlers, repository
│   ├── slot/                 # Entity, UI handler, repository, queries
│   ├── prescription/         # Entity, UI handler, repository
│   ├── healthinsurance/      # Lookup repository
│   ├── domain/               # Shared value types
│   ├── auth/                 # Login, logout and password reset handlers
│   ├── session/              # Session store, Postgres-backed (ADR 0006)
│   ├── password/             # Argon2id hashing and the length rule
│   ├── passwordreset/        # Single-use reset tokens (ADR 0010)
│   ├── mailer/               # SMTP client for outbound mail
│   ├── notification/         # In-memory notification queue (ADR 0002)
│   ├── ratelimit/            # Token-bucket limiter (ADR 0009)
│   ├── worker/               # Periodic background jobs
│   ├── storage/              # S3-compatible object storage client
│   ├── metrics/              # Prometheus collectors
│   ├── tracing/              # OpenTelemetry setup
│   ├── i18n/                 # Message catalogs, locale resolution (es/en)
│   ├── health/               # Liveness + readiness probes
│   ├── middleware/           # RequestID, logger, gzip, CSRF, auth, locale
│   ├── web/                  # DecodeJSON, RFC 9457 Problem Details
│   ├── db/                   # Pool creation, migrations
│   ├── server/               # Graceful shutdown lifecycle
│   └── ui/                   # templ views + static assets
├── documents/decisions/      # Architecture Decision Records
└── docker/                   # Dockerfiles, compose, env
```

## Getting Started

```bash
# Clone and start the stack
docker compose -f docker/docker-compose.dev.yml up -d

# Run database migrations
DATABASE_URL="postgres://app_user:app_pass@localhost:5432/appointment-manager?sslmode=disable" \
  migrate -path internal/db/migrations up

# Start the server
go run ./cmd/server
```

The server starts on `:8080`. Visit `http://localhost:8080` for the UI or `http://localhost:8080/healthz` for a health check.

The dev stack also runs [Mailpit](https://mailpit.axllent.org/), a catcher that accepts every
mail and delivers none, so the password reset link can be read without a relay. Set
`SMTP_HOST=localhost`, `SMTP_PORT=1025` and `SMTP_USE_TLS=false`, then open
`http://127.0.0.1:8025`.

Frontend assets (Tailwind CSS, htmx, Alpine.js) are self-hosted and already committed under `internal/ui/static/`, so no extra setup is needed. If you change Tailwind utility classes in any `.templ` file, run `make css` and commit the regenerated `internal/ui/static/css/app.css`.

### Environment variables

On startup the server loads a `.env` file from the working directory if one is present
(via [godotenv](https://github.com/joho/godotenv)); if it's missing, it silently falls
back to the OS environment. Variables already set in the OS environment take precedence
over the `.env` file.

| Variable | Required | Description |
| --- | --- | --- |
| `DATABASE_URL` | yes | Postgres connection string. |
| `DB_POOL_MAX_CONNS` | no | Maximum connections the pool may open (pgx default: the greater of `4` and the host's CPU count). See [Connection pool sizing](#connection-pool-sizing). |
| `DB_POOL_MIN_CONNS` | no | Connections kept open even while idle, so a burst after a quiet period does not pay the handshake (pgx default `0`). Must not exceed `DB_POOL_MAX_CONNS`; the server refuses to start otherwise. |
| `DB_POOL_MAX_CONN_LIFETIME` | no | How long a connection lives before being retired regardless of use, which is what lets the pool drift back to a healthy database after a failover (pgx default `1h`). |
| `DB_POOL_MAX_CONN_LIFETIME_JITTER` | no | Random spread added on top of the lifetime so a pool opened at once does not expire at once (pgx default `0`, i.e. no spread). |
| `DB_POOL_MAX_CONN_IDLE_TIME` | no | How long an unused connection survives before being closed, down to `DB_POOL_MIN_CONNS` (pgx default `30m`). `0` is rejected: to pgx it means retiring the idle pool on every sweep, not "no limit". |
| `DB_POOL_HEALTH_CHECK_PERIOD` | no | How often the pool sweeps idle connections for expiry and tops itself back up to the minimum (pgx default `1m`). |
| `DEFAULT_LOCALE` | no | Language the UI renders in when a request expresses no preference of its own: `es` (default) or `en`. A visitor's `lang` cookie wins over it, and their `Accept-Language` header comes next. An unsupported value stops the process. |
| `ENV` | no | `development` (default) enables dev-friendly settings. |
| `LOG_LEVEL` | no | `debug` (default), `info`, `warn` or `error`, case-insensitive. |
| `STORAGE_ENDPOINT` | no | S3-compatible endpoint, e.g. `s3.example.com`. When unset, object storage is disabled. |
| `STORAGE_ACCESS_KEY` | with storage | Access key for the object store. |
| `STORAGE_SECRET_KEY` | with storage | Secret key for the object store. |
| `STORAGE_BUCKET` | with storage | Bucket where prescription documents are stored (created if missing). |
| `STORAGE_REGION` | no | Optional region for the object store. |
| `STORAGE_USE_SSL` | no | `true` (default) uses HTTPS; set `false` for a plain-HTTP store. |
| `WORKER_TICKER_INTERVAL` | no | How often the background sweeps run — expiring overdue appointments to absent, and cancelling appointments stranded on an already-blocked slot (default `30m`). |
| `OUTBOX_DRAIN_INTERVAL` | no | How often the transactional outbox is drained and its events delivered (default `15s`). Keep it below the 5-minute retry backoff ceiling, or the backoff schedule stops being what paces retries. |
| `OUTBOX_BATCH_SIZE` | no | How many due events one drain claims per run (default `20`). The whole batch shares one transaction and one job timeout. |
| `METRICS_ADDR` | no | Listen address for the Prometheus metrics server (default `:9090`). Served on a separate listener from the app. |
| `LOGIN_RATE_LIMIT_ENABLED` | no | Whether login attempts are rate limited (default `true`). Unset means enabled — a missing variable must never silently drop the protection. |
| `LOGIN_RATE_LIMIT_ACCOUNT_BURST` | no | Attempts a single account may make back to back before it is throttled (default `5`). |
| `LOGIN_RATE_LIMIT_ACCOUNT_REFILL` | no | How long one account attempt takes to come back, not a window length (default `3m`). A successful login refills the account to full. |
| `LOGIN_RATE_LIMIT_IP_BURST` | no | Attempts a single client IP may make back to back before it is throttled (default `20`). |
| `LOGIN_RATE_LIMIT_IP_REFILL` | no | How long one client-IP attempt takes to come back (default `30s`). |
| `LOGIN_RATE_LIMIT_MAX_ENTRIES` | no | Maximum keys the limiter tracks per dimension before evicting the least recently used (default `10000`, roughly 1.5 MB per dimension). This is what bounds its memory against keys an attacker invents. |
| `APP_BASE_URL` | yes | Origin the password reset link is built from, e.g. `https://turnos.example.com`. Must be http or https and include a host. Never derived from the request: a forged `Host` header would otherwise put an attacker's domain in a mail the user trusts. |
| `SMTP_HOST` | yes | SMTP relay to send through. A relay that is unreachable at startup is logged, not fatal — the server still boots and the reset recovers on its own once the relay is back. A *missing* one is fatal, because password reset is the only recovery path and starting without it would leave the account unrecoverable and say nothing. |
| `SMTP_PORT` | no | Submission port on the relay (default `587`). |
| `SMTP_USERNAME` / `SMTP_PASSWORD` | no | Relay credentials. A password without a username stops the process. |
| `SMTP_FROM_ADDRESS` | yes | Sender the reset mail claims to be from. Must parse as an address. |
| `SMTP_FROM_NAME` | no | Display name shown beside the sender address. |
| `SMTP_USE_TLS` | no | `true` (default) requires TLS. `false` is for a local catcher such as Mailpit only; against a remote relay it puts the credentials on the wire in the clear. Opportunistic STARTTLS is deliberately not offered. |
| `PASSWORD_RESET_TOKEN_TTL` | no | How long a reset link stays redeemable (default `30m`). Absolute, written once, single use. |
| `PASSWORD_RESET_RATE_LIMIT_ACCOUNT_BURST` | no | Reset requests one account may make back to back (default `3`). |
| `PASSWORD_RESET_RATE_LIMIT_ACCOUNT_REFILL` | no | How long one account request takes to come back (default `15m`). |
| `PASSWORD_RESET_RATE_LIMIT_IP_BURST` | no | Reset requests one client IP may make back to back (default `10`). |
| `PASSWORD_RESET_RATE_LIMIT_IP_REFILL` | no | How long one client-IP request takes to come back (default `5m`). |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | no | OTLP/HTTP collector base URL for tracing (e.g. `http://tempo:4318`). When unset, tracing is disabled. |
| `OTEL_TRACES_SAMPLE_RATIO` | no | Head-based trace sampling probability in `[0,1]` (default `1.0`). Child spans follow their parent's decision. |
| `OTEL_SERVICE_VERSION` | no | Release identifier attached to spans as `service.version` (default `dev`). |

Every `DB_POOL_*` variable is optional and independent. Leaving one unset (or blank)
keeps pgx's own default for that setting rather than a default this project picked —
which is why the effective configuration is logged once at startup, as
`postgres pool configured`. A variable that *is* set but malformed or out of range
stops the process instead of falling back.

### Connection pool sizing

For throughput, the number of connections actively working should land near the
formula from the [PostgreSQL wiki](https://wiki.postgresql.org/wiki/Number_Of_Database_Connections)
(also used by [HikariCP](https://github.com/brettwooldridge/HikariCP/wiki/About-Pool-Sizing)):

```
(core_count × 2) + effective_spindle_count
```

`core_count` is **physical** cores, not hyperthreads. `effective_spindle_count` is ~0
when the working set fits in RAM and approaches the number of disks as the cache hit
rate drops — for a single disk, take it as 1.

| Host | `DB_POOL_MAX_CONNS` |
| --- | --- |
| 6 physical cores, 1 disk | `(6 × 2) + 1` ≈ **14** |
| 2 vCPU VPS, 1 disk | `(2 × 2) + 1` = **5** |

On a VPS, "vCPU" usually means a shared hypervisor thread rather than a dedicated
core, so stay at the conservative end — more so when Postgres runs on the same host as
the app and the two compete for the same CPUs.

Two things worth knowing before tuning:

- **There is one pool.** The HTTP handlers, both background sweeps, the notification
  drain and the readiness check all share it. The number above is the total for the
  process, not a per-component budget.
- **Everything has to fit in `max_connections`.** Size the server's limit above the sum
  of every pool pointed at it, leaving room for administrative and monitoring sessions.

### Resetting a password without mail

The image ships a second binary for when the mailed link is not an option — no relay
configured, a relay that is down, or a mail that never arrives:

```bash
docker exec appointment-manager /resetpassword -email=you@example.com
```

It prints a fresh random password on stdout **once** and nothing else, so the stream can
be captured directly; diagnostics go to stderr. There is deliberately no `-password`
flag, which is what keeps the password out of your shell history and out of `ps`. Every
session the account holds is closed, and the hash is written with the same Argon2id
parameters the login verifies against.

It talks to the database directly and does **not** apply migrations, so it still works
when a half-applied migration is what broke the deployment in the first place.

## Monitoring & Observability

The service exposes Prometheus metrics on a **separate listener** (default `:9090`, configurable
via `METRICS_ADDR`) at `GET /metrics`. Keeping it off the main `:8080` app port means metric
scrapes bypass the CSRF/Gzip/auth middleware chain and are not exposed through the public reverse
proxy.

What is exported (all under the `appt_` namespace, plus the standard `go_*` / `process_*` runtime
and process collectors):

- **HTTP RED** — `appt_http_requests_total{method,route,status_class}`,
  `appt_http_request_duration_seconds{method,route}`, `appt_http_requests_in_flight`.
  The `route` label is the low-cardinality route **template** (e.g. `/api/v1/appointments/{id}`),
  never the raw path.
- **Database** — `appt_db_query_duration_seconds{operation}`,
  `appt_db_query_errors_total{operation}` (via a pgx query tracer), and live pool gauges
  `appt_db_pool_connections{state}`.
- **Business** — `appt_appointments_created_total` and
  `appt_appointments_finalized_total{outcome}` where `outcome` is one of
  `attended` / `cancelled` / `absent` / `expired`.
- **Password reset** — `appt_password_reset_mail_total{outcome}` (`sent` / `failed`) and
  `appt_password_resets_completed_total`. The first is the only signal that the flow is
  broken: the requester is answered identically whether or not the mail went out, so a
  relay the app cannot reach is otherwise invisible.

Each metric declaration in `internal/metrics/metrics.go` documents its dashboard and alert PromQL
inline.

### Scraping from an external Prometheus

The app container is attached to the `caddy_network` (a shared, non-`internal` network), so a
Prometheus running in a separate stack can scrape it **once its own container is also attached to
`caddy_network`**. Add a scrape job:

```yaml
scrape_configs:
  - job_name: appointment-manager
    static_configs:
      - targets: ["appointment-manager:9090"]
```

Local development runs the metrics server on `http://localhost:9090/metrics`.

### Distributed tracing (Tempo)

Set `OTEL_EXPORTER_OTLP_ENDPOINT` (e.g. `http://tempo:4318`) to export OpenTelemetry
traces over OTLP/HTTP to Tempo. When the variable is unset, tracing stays fully disabled
and every span is a no-op — no collector is required to run the service.

- Each incoming request opens a server span (outermost middleware), which parents the
  appointment service spans (`appointment.Service.Create` / `Cancel` / `Attend`) and a
  client span per database query (`db.select`, `db.insert`, …).
- **Logs ↔ traces**: structured logs emitted with a request context carry `trace_id` and
  `span_id`, so you can pivot from a Loki log line to the matching trace in Tempo.
- **Metrics ↔ traces**: the HTTP and DB duration histograms attach a `trace_id`
  [exemplar](https://prometheus.io/docs/prometheus/latest/feature_flags/#exemplars-storage)
  when a sampled span is active, letting you jump from a P99 latency spike straight to the
  offending trace. This requires Prometheus to run with `--enable-feature=exemplar-storage`.
- Sampling is head-based via `OTEL_TRACES_SAMPLE_RATIO` (default `1.0`, i.e. sample every
  trace — suitable for this low-volume service; lower it if trace volume grows).

Like the metrics scrape, the app reaches Tempo over `caddy_network`, so **Tempo's container
must also be attached to `caddy_network`** for spans to be delivered.

## Highlights for Developers

- **Zero external HTTP frameworks** — pure `net/http` with idiomatic Go patterns. Demonstrates deep understanding of the standard library.
- **Database-level integrity** — overlapping slot prevention via GiST exclusion constraints, partial unique indexes for active appointments, and foreign key enforcement throughout.
- **Strict error handling** — all API errors follow RFC 9457 Problem Details (`application/problem+json`). Request validation is thorough (content type, body size limits, unknown field rejection).
- **Security by design** — Argon2id for passwords, CSRF protection via `Go 1.26` cross-origin protections, session-based auth, gzip compression, structured logging with `slog`.
- **Comprehensive testing** — unit tests with mocked dependencies, integration tests with disposable PostgreSQL instances via `testcontainers`, injection of clock functions for deterministic time-dependent rule testing. Internal package coverage target: ≥ 90%.
- **Strict linting** — 35+ linters configured in `.golangci.yml` covering security, complexity, style, and performance. All `//nolint` directives are justified.

## TODO
- **Add request-deadline middleware**
- **Add email to the notification package**
- **Consider whether it is worth adding an outbox table to the notification worker/package**

## License

[MIT](LICENSE)
