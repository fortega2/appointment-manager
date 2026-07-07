# Medical Appointment Manager

A production-grade medical appointment scheduling system built entirely in Go. Designed for clinics and private practices to manage professionals, patients, time slots, and appointments with strict business rules, concurrency safety, and a dual REST API + HTML interface.

## Features

- **Professional management** — register, update, activate/deactivate practitioners (currently kinesiology-focused).
- **Patient management** — register and update patients with health insurance details.
- **Time slot management** — create availability blocks with configurable max capacity. Overlapping slots are prevented at the database level using PostgreSQL exclusion constraints.
- **Appointment lifecycle** — book, cancel, and mark appointments as attended. Business rules enforce cancellation windows (cancel before 24h, otherwise mark as absent) and attendance windows (only during the appointment period).
- **Concurrency safety** — all status transitions are atomic with optimistic locking (`UPDATE ... WHERE status = ?`), and slot capacity checks use `SELECT ... FOR UPDATE` within transactions.
- **Authentication** — session-based login for administrative staff (assistants) with Argon2id password hashing.
- **Dual interface** — JSON REST API under `/api/v1/` for programmatic access, plus an HTMX-powered HTML interface for day-to-day use.

## Tech Stack

| Layer | Technology |
|---|---|
| Language | Go 1.26 |
| HTTP | `net/http`, `http.ServeMux` — no router frameworks |
| Database | PostgreSQL 17 + `pgx` driver |
| Migrations | `golang-migrate` with embedded SQL |
| Templates | `a-h/templ` — type-safe HTML templates |
| Frontend | HTMX + Alpine.js + Tailwind CSS (self-hosted, pinned versions — no CDN) |
| Auth | Session-based (in-memory store) + Argon2id |
| Container | Multi-stage Docker (scratch-based) + docker-compose |
| Testing | `testify` (assert, require, mock) + `testcontainers-go` |
| Linting | `golangci-lint` with 35+ linters (strict config) |

## Architecture

```
┌─────────────────────────────────────────────────┐
│                HTTP Server (:8080)               │
│  ┌───────────────────────────────────────────┐  │
│  │            Middleware Chain                │  │
│  │  RequestID → Logger → Gzip → CSRF → Auth  │  │
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
│   └── healthcheck/          # Docker HEALTHCHECK CLI
├── internal/
│   ├── appointment/          # Entity, service, REST + UI handlers, repository
│   ├── assistant/            # Entity, service, REST handler, repository
│   ├── professional/         # Entity, REST + UI handlers, repository
│   ├── patient/              # Entity, REST + UI handlers, repository
│   ├── slot/                 # Entity, UI handler, repository, queries
│   ├── healthinsurance/      # Lookup repository
│   ├── auth/                 # Login/logout handlers (JSON + form)
│   ├── health/               # Liveness + readiness probes
│   ├── session/              # In-memory session store
│   ├── password/             # Argon2id hashing
│   ├── middleware/           # RequestID, logger, gzip, CSRF, auth
│   ├── web/                  # DecodeJSON, RFC 9457 Problem Details
│   ├── db/                   # Pool creation, migrations
│   ├── server/               # Graceful shutdown lifecycle
│   └── ui/                   # Page templates (login, home, layout, components)
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

Frontend assets (Tailwind CSS, htmx, Alpine.js) are self-hosted and already committed under `internal/ui/static/`, so no extra setup is needed. If you change Tailwind utility classes in any `.templ` file, run `make css` and commit the regenerated `internal/ui/static/css/app.css`.

### Environment variables

On startup the server loads a `.env` file from the working directory if one is present
(via [godotenv](https://github.com/joho/godotenv)); if it's missing, it silently falls
back to the OS environment. Variables already set in the OS environment take precedence
over the `.env` file.

| Variable | Required | Description |
| --- | --- | --- |
| `DATABASE_URL` | yes | Postgres connection string. |
| `ENV` | no | `development` (default) enables dev-friendly settings. |
| `STORAGE_ENDPOINT` | no | S3-compatible endpoint, e.g. `s3.example.com`. When unset, object storage is disabled. |
| `STORAGE_ACCESS_KEY` | with storage | Access key for the object store. |
| `STORAGE_SECRET_KEY` | with storage | Secret key for the object store. |
| `STORAGE_BUCKET` | with storage | Bucket where prescription documents are stored (created if missing). |
| `STORAGE_REGION` | no | Optional region for the object store. |
| `STORAGE_USE_SSL` | no | `true` (default) uses HTTPS; set `false` for a plain-HTTP store. |

## Highlights for Developers

- **Zero external HTTP frameworks** — pure `net/http` with idiomatic Go patterns. Demonstrates deep understanding of the standard library.
- **Database-level integrity** — overlapping slot prevention via GiST exclusion constraints, partial unique indexes for active appointments, and foreign key enforcement throughout.
- **Strict error handling** — all API errors follow RFC 9457 Problem Details (`application/problem+json`). Request validation is thorough (content type, body size limits, unknown field rejection).
- **Security by design** — Argon2id for passwords, CSRF protection via `Go 1.26` cross-origin protections, session-based auth, gzip compression, structured logging with `slog`.
- **Comprehensive testing** — unit tests with mocked dependencies, integration tests with disposable PostgreSQL instances via `testcontainers`, injection of clock functions for deterministic time-dependent rule testing. Internal package coverage target: ≥ 90%.
- **Strict linting** — 35+ linters configured in `.golangci.yml` covering security, complexity, style, and performance. All `//nolint` directives are justified.

## License

[MIT](LICENSE)
