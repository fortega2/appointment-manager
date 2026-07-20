FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine3.24 AS builder

RUN apk add --no-cache ca-certificates && update-ca-certificates

RUN adduser -D -g '' -u 10001 appuser

WORKDIR /app

ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN chmod -R a+rX internal/ui/static

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /app/appointment-manager ./cmd/server
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /app/healthcheck ./cmd/healthcheck/main.go

# Check that the files are executable and readable by all users
RUN ls -l /app/appointment-manager /app/healthcheck && umask

FROM scratch

COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder --chmod=755 /app/appointment-manager /appointment-manager
COPY --from=builder --chmod=755 /app/healthcheck /healthcheck
COPY --from=builder /app/internal/ui/static /internal/ui/static

USER appuser

HEALTHCHECK --interval=10s --timeout=2s --start-period=20s --retries=3 \
    CMD ["/healthcheck", "-url", "http://127.0.0.1:8080/readyz", "-timeout", "2s"]

ENTRYPOINT ["/appointment-manager"]
