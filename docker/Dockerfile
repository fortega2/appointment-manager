FROM golang:1.26.5-alpine3.24 AS builder

RUN apk add --no-cache ca-certificates && update-ca-certificates

RUN adduser -D -g '' -u 10001 appuser

WORKDIR /app

ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /app/appointment-manager-server ./cmd/server/main.go
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /app/healthcheck ./cmd/healthcheck/main.go

FROM scratch

COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /app/appointment-manager-server /appointment-manager-server
COPY --from=builder /app/healthcheck /healthcheck
COPY --from=builder /app/internal/ui/static /internal/ui/static

USER appuser

HEALTHCHECK --interval=10s --timeout=2s --start-period=20s --retries=3 \
    CMD ["/healthcheck", "-url", "http://127.0.0.1:8080/readyz", "-timeout", "2s"]

ENTRYPOINT ["/appointment-manager-server"]
