---
name: pre-pr-check
description: Run the appointment-manager pre-PR verification sequence (lint, race tests, coverage, checklist) before finishing substantial changes or opening a PR. Use once all TODOs for the current task are done, not after every small edit.
tools: Bash, Read
---

# Pre-PR Check

Run this before finishing any substantial change in `appointment-manager` —
i.e. once every TODO for the current task is done, not after each small edit.

## 1. Lint

```bash
golangci-lint run ./...
```

`golangci-lint` must be built with a Go toolchain >= the `go` directive in
`go.mod`. If it refuses to run with a "language version is lower than the
targeted Go version" error, update it:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
```

`formatters.enable` in `.golangci.yml` includes `gofmt`, so this also catches
unformatted files — a separate `gofmt -w` pass normally isn't needed.

## 2. Tests with race detector

```bash
go test ./... -race
```

## 3. Vulnerability scan

```bash
govulncheck ./...
```

## 4. Coverage (only when tests changed)

Internal-packages coverage gate: **>= 70%**.

```bash
go test ./... -race -covermode=atomic -coverpkg=./internal/... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

Read the `total:` line as source of truth. `cmd/server/main.go` is
intentionally excluded from this gate unless explicitly requested.

## 5. Checklist

- [ ] Code formatted (`gofmt`) — covered by step 1.
- [ ] Lint clean.
- [ ] Tests pass with `-race`.
- [ ] Internal coverage remains >= 90% when relevant.
- [ ] New tests follow the `*_test` package style and the per-file test-constant
      rule (constants local to the file, no `test` prefix).
- [ ] No unnecessary `//nolint`; every suppression is specific and justified.
- [ ] A string literal repeated more than three times in tests is extracted to
      a constant.
