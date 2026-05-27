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
- Always use the available SKILLS.md and MCP for the required task.
- Appointment transition rules:
  - cancel: if now is before start-24h -> `cancelled`; otherwise -> `absent`.
  - attend: only allowed when now is within [start, end].
- Appointment status updates must be concurrency-safe (atomic update with expected current status, or equivalent locking).

## 1) Repository Snapshot

- Language: Go (`go 1.26.3` in `go.mod`).
- Module: `appointment-manager`.
- Main entrypoint: `cmd/server/main.go`.
- Core packages:
  - `internal/appointment`
  - `internal/assistant`
  - `internal/password`
  - `internal/web`
- Domain packages:
  - `internal/professional`
  - `internal/patient`
  - `internal/slot`
  - `internal/domain`
- DB package:
  - `internal/db/migrations`
- Planned:
  - `internal/shared`
- Lint config: `.golangci.yml` (strict, many linters enabled).
- Testing libs: `stretchr/testify`.

## 2) Build / Run Commands

- Build all packages:
  - `go build ./...`
- Run API locally:
  - `go run ./cmd/server`

### Database migration commands (`migrate`)

- Create migration file pair:
  - `migrate create -ext sql -dir internal/db/migrations -seq <migration_name>`
- Apply all pending migrations:
  - `migrate -path internal/db/migrations -database "$DATABASE_URL" up`
- Roll back one migration:
  - `migrate -path internal/db/migrations -database "$DATABASE_URL" down 1`

## 3) Lint / Format Commands

- Run full lint:
  - `golangci-lint run ./...`
- Run lint on one package:
  - `golangci-lint run ./internal/assistant/...`
- Format Go code:
  - `gofmt -w <file1.go> <file2.go>`
- Keep module graph tidy when dependencies change:
  - `go mod tidy`

Notes:
- Lint is strict (security, complexity, modernize, sloglint, copyloopvar, etc.).
- `//nolint:<linter>` must be specific and justified.

## 4) Test Commands

- Run all tests:
  - `go test ./...`
- Run all tests with race detector:
  - `go test ./... -race`
- Run tests for one package:
  - `go test ./internal/appointment`
  - `go test ./internal/assistant`
  - `go test ./internal/password`
  - `go test ./internal/web`
- Run one specific test:
  - `go test ./internal/assistant -run '^TestCreateEndpoint$' -v`
- Run one subtest:
  - `go test ./internal/assistant -run 'TestCreateEndpoint/success' -v`
- Disable test cache while debugging:
  - `go test ./internal/assistant -count=1 -run '^TestGetEndpoint$' -v`

### Coverage (team convention)

- Internal-packages coverage gate target: `>= 90%`.
- Preferred command:
  - `go test ./... -race -covermode=atomic -coverpkg=./internal/... -coverprofile=coverage.out`
  - `go tool cover -func=coverage.out`
- Read the `total:` line from `go tool cover -func` as source of truth.

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

### Types and Structs

- Prefer explicit domain types (e.g., `assistant.ID`).
- Keep struct fields focused and intentional.
- Preserve API contracts in JSON tags.
- If a JSON field name is security-sensitive (e.g., `json:"password"`), keep contract and justify any lint suppression.

### Naming

- Use Go naming conventions (MixedCaps, no snake_case for identifiers).
- Error vars follow `ErrXxx` pattern.
- Tests use descriptive names with table-driven style when practical.
- In test files, constants must be local to that file and should NOT use the `test` prefix.

### Error Handling

- Return errors; do not panic for expected failures.
- Wrap underlying errors with context using `%w` when propagating.
- Use `errors.Is` / `errors.As` for branching on error types.
- Keep error messages lowercase, concise, and actionable.

### Concurrency and Safety

- Protect shared maps/slices with mutexes (`sync.RWMutex` as appropriate).
- For in-memory repositories, avoid exposing mutable internals:
  - return defensive copies where needed.
- Ensure code passes `-race` before considering work done.

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

Before finishing substantial changes, run:

1. `golangci-lint run ./...`
2. `go test ./... -race`
3. coverage command for internal packages when tests changed

## 8) MCP and SKILLS Usage

Agents are encouraged to use available MCP integrations and skills proactively.

- Use Context7 MCP when you need up-to-date library/framework docs.
- Use loaded SKILLS for domain-specific guidance (especially Go lint/testing/concurrency/security).
- Validate externally sourced guidance against current repo conventions before applying.

## 9) Quick Pre-PR Checklists

- [ ] Code formatted (`gofmt`).
- [ ] Lint clean (`golangci-lint run ./...`).
- [ ] Tests pass (`go test ./... -race`).
- [ ] Internal coverage remains `>= 90%` when relevant.
- [ ] New tests follow `*_test` package style and per-file constants rule.
- [ ] No unnecessary `nolint`; every suppression has a reason.
- [ ] If a string repeat it'self more than three times create a constant for it
- [ ] Always start a code-review when all the TODO's list are done

## 10) Incremental Delivery Roadmap

Use this order unless the user requests a different priority:

1. Domain entities + shared enum package (`shared`, `professional`, `patient`, `slot`, `appointment`).
2. Service layer with business rules RN1/RN2/RN3 and unit tests.
3. PostgreSQL schema + migrations under `internal/db/migrations`.
4. `pgx` repositories and transaction boundaries.
5. HTTP handlers for all aggregates and auth middleware.
6. Hardening pass (`golangci-lint`, `go test -race`, coverage check).
