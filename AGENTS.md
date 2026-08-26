# AGENTS.md

Guidance for coding agents working in `appointment-manager`.

## 0) Locked Architecture Decisions (Mar 2026)

- Password hashing: Argon2id (current implementation in `internal/password`).
- HTTP stack: Go standard library only (`net/http`, `http.ServeMux`).
- Do not add router frameworks (`chi`, `gin`) unless explicitly requested.
- Database access layer: `pgx`.
- Do not add `sqlc` unless explicitly requested.
- DB migrations tool: `golang-migrate`.
- Migration files path: `internal/db/migrations`.
- Assistant model: single assistant for now.
- Initial assistant creation: manual by product owner; do not add bootstrap/seed/signup flows unless explicitly requested.
- Always use the available skills (project skills under `.claude/skills/`, plus any loaded plugin skills) and MCP integrations for the required task.
- Appointment transition rules:
  - cancel: if now is before start-24h -> `cancelled`; otherwise -> `absent`.
  - attend: only allowed when now is within [start, end].
- Appointment status updates must be concurrency-safe (atomic update with expected current status, or equivalent locking).
- UI localization: `github.com/invopop/ctxi18n` v0.9.0, always behind the `internal/i18n` facade —
  no `.templ` file or handler imports `ctxi18n` directly. Catalogs are `internal/i18n/locales/*.yml`
  (`es` default, `en`), the choice lives in the `lang` cookie, and `/api/v1/*` responses stay in
  English. Go error strings are never translated: the UI maps sentinels to catalog keys in each
  package's `messages.go`. See ADR 0007.
- Frontend assets (Tailwind CSS, htmx, Alpine.js) are self-hosted and pinned — no CDN `<script src="https://...">` tags in `.templ` files. Pinned versions: Tailwind CSS v4.3.2, htmx v2.0.10, Alpine.js v3.15.12. Regenerating/bumping these is covered by the `frontend-asset-update` skill.

### Architecture Decision Records

Design and performance decisions whose reasoning would not survive in the code live in
`documents/decisions/` as numbered ADRs. `ls` that directory for the current set; each filename
names the area it covers, and every ADR opens with a **Scope** line listing the packages and
symbols it governs.

- Read the relevant ADR before changing the code it covers. Several record a *load-bearing*
  detail — a literal that must not become a placeholder, a struct that must stay pointer-free —
  that no test would catch if broken.
- Check the **Status** line before treating one as binding. `Accepted` describes the code as it
  is; `Proposed` records reasoning for a decision that has *not* been made and that no code
  implements yet — do not build against it without asking.
- When a decision is made that a future reader would otherwise have to reverse-engineer, add the
  next-numbered ADR in the same format rather than leaving it in a commit message.

## 1) Repository Snapshot

- Language: Go (`go 1.27.0` in `go.mod`).
- Module: `appointment-manager`.
- Main entrypoint: `cmd/server/main.go`.
- Core packages:
  - `internal/appointment`
  - `internal/assistant`
  - `internal/auth`
  - `internal/password`
  - `internal/passwordreset`
  - `internal/web`
- Domain packages:
  - `internal/professional`
  - `internal/patient`
  - `internal/slot`
  - `internal/prescription`
  - `internal/healthinsurance`
  - `internal/domain`
- Infra / cross-cutting packages:
  - `internal/db/migrations`
  - `internal/middleware`
  - `internal/session`
  - `internal/token` (opaque bearer tokens and their digests, shared by session and password reset)
  - `internal/ratelimit`
  - `internal/mailer` (SMTP client; needs `SMTP_*` env vars, see README)
  - `internal/notification`
  - `internal/storage` (S3-compatible client; needs `STORAGE_*` env vars, see README)
  - `internal/server`
  - `internal/health`
  - `internal/metrics` (Prometheus metrics)
  - `internal/tracing` (OTel tracing)
  - `internal/worker`
  - `internal/ui` (templ views + static assets)
- Lint config: `.golangci.yml` (strict, many linters enabled).
- Testing libs: `stretchr/testify`.

## 2) Build / Run Commands

- Build all packages:
  - `go build ./...`
- Run API locally:
  - `go run ./cmd/server`

### Frontend Assets

- Tailwind CSS, htmx, and Alpine.js are self-hosted under `internal/ui/static/{css,vendor}/`, pinned and committed — never reintroduce CDN `<script>`/`<link>` tags in `.templ` files.
- Fresh clone requires **no** extra setup step — `app.css` and the vendored JS are already committed, so `go run ./cmd/server` serves working CSS/JS immediately.
- For regenerating CSS after Tailwind class changes, or bumping the pinned htmx/Alpine versions, use the `frontend-asset-update` skill.

### Database migrations (`golang-migrate`)

- Migration files live under `internal/db/migrations` as sequential up/down pairs.
- For creating, applying, or rolling back migrations, use the `db-migration` skill.

## 3) Lint / Format Commands

- Run full lint:
  - `golangci-lint run ./...`
- Lint the `//go:build integration` files too (the command above skips them, so
  issues can accumulate there unseen):
  - `golangci-lint run --build-tags=integration ./...`
- Run lint on one package:
  - `golangci-lint run ./internal/assistant/...`
- Format Go code:
  - `gofmt -w <file1.go> <file2.go>`
- Keep module graph tidy when dependencies change:
  - `go mod tidy`

Notes:
- Lint is strict (security, complexity, modernize, sloglint, copyloopvar, etc.).
- `//nolint:<linter>` must be specific and justified.
- Run vulnerability scan: `govulncheck ./...`
- `govulncheck` takes the stdlib version from `go env GOVERSION`. Distro Go builds bake the
  enabled experiments into that string (`go1.27.0-X:nodwarf5` on Arch/CachyOS), which is not
  valid semver, so every standard-library finding is dropped *silently* — the scan reports
  "No vulnerabilities found" instead of failing. That is why `lefthook.yml` runs it as
  `GOVERSION=$(go env GOVERSION | cut -d- -f1) govulncheck ...`; do not drop that prefix.
- `lefthook` pre-commit already runs `golangci-lint run`, `govulncheck ./...`, `go test -short ./...`,
  and `make check-css` (on `.templ` changes) in parallel on every commit — a failing commit may be
  any of these, not just CSS drift.
- `golangci-lint` must be built with a Go toolchain whose language version is >= the one in the
  `go` directive of `go.mod` (currently `1.27`; the patch level does not matter), otherwise it refuses to run (`can't load config: the Go language version ... is lower than the targeted Go version`). If your installed binary is out of date, update it with `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest` (or per https://golangci-lint.run/welcome/install/).
- `govulncheck` has the same toolchain constraint as `golangci-lint` above: a binary built with an
  older Go than the `go` directive fails every package with `package requires newer Go version
  go1.N (application built with go1.N-1)` and exits non-zero, which breaks the `lefthook` pre-commit
  hook. On every Go major bump, reinstall both: `go install golang.org/x/vuln/cmd/govulncheck@latest`.
- `formatters.enable` in `.golangci.yml` includes `gofmt`, so `golangci-lint run ./...` also fails on unformatted files — a separate manual `gofmt -w` pass shouldn't normally be needed, but is listed above as a fallback.

## 4) Test Commands

- Run all tests:
  - `go test ./...`
- Run all tests with race detector:
  - `go test ./... -race`
- Run tests for one package:
  - `go test ./internal/appointment`
- Run one specific test:
  - `go test ./internal/assistant -run '^TestCreateEndpoint$' -v`
- Run one subtest:
  - `go test ./internal/assistant -run 'TestCreateEndpoint/success' -v`
- Disable test cache while debugging:
  - `go test ./internal/assistant -count=1 -run '^TestGetEndpoint$' -v`

### Integration tests in CI (staged rollout, Aug 2026)

Every integration test opens with `testcontainers.SkipIfProviderIsNotHealthy(t)`, which turns an
unreachable Docker daemon into a **skip** — and `go test` reports a package whose tests all skipped
as `ok`. The `linter-tests` job had no `DOCKER_HOST`, so all 133 integration tests skipped silently
and CI was green on coverage it never ran. Two guards now prevent a repeat, both in
`.forgejo/workflows/config.yml`:

- The step greps its own `-v` output for `--- SKIP` and fails the job. Match the *indented* form
  too: when a subtest skips, the parent still prints `--- PASS`, so an anchored `^--- SKIP` misses it.
  The project has no other `t.Skip`, so any skip means the daemon is gone.
- `set -euo pipefail` with `shell: bash`. Without `pipefail`, `go test | tee` returns tee's status
  and a **failing** suite exits 0 — the same class of silent-green bug.

Job containers run *inside* `forgejo-dind`, so the default gateway is dind's own interface: that is
both where dockerd listens and where the ports of testcontainers' containers are published. Hence
`DOCKER_HOST` and `TESTCONTAINERS_HOST_OVERRIDE` both point at the gateway, and the CI image needs
`iproute2` for `ip route`.

Every `postgres.Run` call sets `PGDATA=/var/lib/postgresql/data`. Postgres 18 moved the default to
`/var/lib/postgresql/$PG_MAJOR/docker`, and its entrypoint creates that path as root, chowns only
the leaf, then re-execs under `gosu postgres` and re-runs the same setup — which dies with
`mkdir: can't create directory ... Permission denied` on the arm64 image inside dind. The classic
path hangs directly off the volume, which the image already owns as `postgres`, so the second pass
is a no-op. Keep the override: dropping it silently breaks CI only, never local amd64 runs.

`internal/auth`'s integration fixtures build their hasher with `password.WithTestCost()`. At the
production cost a single Argon2 hash outlasts `maxQueueWait` (3s) on the CI runner, so
`TestLoginQueuesConcurrentPasswordChecks` — 10 concurrent logins against 2 hashing slots — was
answered with 503 there while passing locally, where the same hash costs ~60ms. The catch when
applying it: **`Compare` re-derives with the parameters stored in the encoded hash, not with the
hasher's**, so the login path only gets cheap if `seedAssistantForAuth` also hashes through a
`WithTestCost` hasher. Setting it on the handler alone changes nothing.

**All 13 integration packages run, with `-p 1`.** dind is capped at 1.5 CPU / 2.5 GB and every
container shares that budget with the job itself, so packages are serialised rather than run in
parallel -- each one starts its own Postgres. Measured 2026-08-26: the full suite is 248s locally
(`-p 1`, x86), and `internal/db` ran 2.47x slower on the arm64 runner, which projects to roughly
10 minutes of CI. `internal/appointment` alone is 89s of that 248s. If the job gets too slow, the
first lever is running integration only on `main` pushes, the second is sharing one Postgres across
packages.

### Coverage (team convention)

- For the coverage command and the full pre-finish verification sequence, use the `pre-pr-check` skill.

Important:
- `cmd/server/main.go` is intentionally excluded from strict unit-coverage goals unless explicitly requested.

## 5) Code Style Guidelines

Follow existing project conventions first.

### Formatting and Imports

- Always `gofmt` touched files.
- Prefer import grouping:
  1. stdlib
  2. third-party
  3. local module imports
- Keep imports minimal; remove unused imports immediately.

### Comments

Do not narrate the code. A comment is warranted in exactly two cases:

1. **Doc comments** on exported (and notable unexported) declarations — types, functions,
   methods, constants. Follow Go stdlib style: start with the identifier's name, say what it
   does and what the caller needs to know, and stop. One or two sentences. Not an essay, and
   not a restatement of the signature.
2. **A non-obvious decision inside a function**, where the code alone cannot explain *why*.
   A workaround, a deliberate deviation from the obvious implementation, an ordering that
   matters, a literal that is load-bearing.

Everything else is noise. In particular, do not add comments that restate the next line, label
sections of a function, or explain language features.

If the reasoning is long enough to need a paragraph, it does not belong in the code — write an
ADR in `documents/decisions/` and let the code stay clean.

### Types and Structs

- Prefer explicit domain types (e.g., `assistant.ID`).
- Keep struct fields focused and intentional.
- Preserve API contracts in JSON tags.
- If a JSON field name is security-sensitive (e.g., `json:"password"`), keep contract and justify any lint suppression.

### Naming, Error Handling, Concurrency

Follow standard idiomatic Go practice here (see the `golang-naming`,
`golang-error-handling`, and `golang-concurrency`/`golang-safety` skills for
the general guidance). Project-specific deviations:

- In test files, constants must be local to that file and should NOT use the
  `test` prefix (see also the Sonar/Duplication guidance in §6).
- In-memory repositories must not expose mutable internals — return
  defensive copies of shared maps/slices rather than references to them.

### HTTP Handlers

- Constructor validation is required for injected dependencies.
- Use structured logging with `slog`.
- Return appropriate HTTP status codes and stable error messages.
- Set explicit response headers when returning JSON.
- Keep routing in standard `http.ServeMux` with method-aware patterns.
- Decode request JSON through `internal/web.DecodeJSON` (or an equivalent helper) with:
  - `Content-Type` validation (`application/json`).
  - body size limit via `http.MaxBytesReader`.
  - `json.Decoder.DisallowUnknownFields()`.
  - single-object enforcement (second decode must return `io.EOF`).
- Return error responses using RFC 9457 Problem Details via `internal/web.WriteProblem`
  and content type `application/problem+json`.
- For appointment transition endpoints (`cancel`, `attend`), prefer action-style routes and no request body.

### Logging

- Use `log/slog` for structured logs.
- In tests, prefer `slog.New(slog.DiscardHandler)`.
- Always prefer to use slog with context, if context is available. Example: logger.InfoContext rather than logger.Info

## 6) Testing Conventions

- Default to black-box tests with external package names:
  - `assistant_test`, `password_test`.
- Only use same-package tests when access to unexported behavior is truly necessary.
- Use `testify/require` for preconditions and fatal assertions.
- Use `testify/assert` for non-fatal multi-assert validations.
- Use `testify/mock` to isolate dependencies in handler tests.
- Prefer endpoint-level handler testing via registered routes over directly calling private handler methods.
- For time-dependent business rules, prefer injected clock functions in services to keep unit tests deterministic.

### Sonar / Duplication Guidance

- Repeated string literals in tests should be extracted to constants when repeated frequently.
- Keep constants per file (do not centralize test constants into shared test constants files).

## 7) Agent Workflow Expectations

- Make the smallest correct change.
- Do not refactor unrelated code.
- Keep `main` behavior stable unless requested.
- Run lint + tests after changes:
  - minimum: `go test ./...`
  - preferred: `golangci-lint run ./...` and `go test ./... -race`
- Before finishing substantial changes (all TODOs for the task done), run the
  `pre-pr-check` skill: lint, race tests, vulnerability scan, coverage when
  tests changed, the pre-PR checklist, and starting a code review.

### Commits

- **Subject line only — no body.** This repo's history is one-liners; a
  multi-paragraph commit message is out of place here. Reasoning that needs more
  than a subject line belongs in an ADR under `documents/decisions/`, where it
  is findable later, not in a message nobody re-reads.
- Conventional-commit prefixes, as in `git log`: `feat`, `fix`, `refactor`,
  `docs`, `style`, `chore`, with a scope (`fix(password): ...`).
- Prefer several small commits over one large one, split by context rather than
  by file. Every commit must build and pass tests on its own — `lefthook`
  pre-commit enforces it, so a commit that only compiles alongside the next one
  cannot be created without `--no-verify`.

## 8) MCP and SKILLS Usage

Agents are encouraged to use available MCP integrations and skills proactively.

- Use Context7 MCP when you need up-to-date library/framework docs.
- Use loaded skills for domain-specific guidance (especially Go lint/testing/concurrency/security) and for project-specific workflows under `.claude/skills/` (`frontend-asset-update`, `db-migration`, `pre-pr-check`).
- Validate externally sourced guidance against current repo conventions before applying.
