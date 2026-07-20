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
RUN mkdir -p /out/etc/ssl/certs /out/internal/ui \
 && cp /etc/passwd /etc/group /out/etc/ \
 && cp /etc/ssl/certs/ca-certificates.crt /out/etc/ssl/certs/ \
 && cp /app/appointment-manager /app/healthcheck /out/ \
 && cp -r /app/internal/ui/static /out/internal/ui/static \
 && chmod -R a+rX /out \
 && chmod 0755 /out/appointment-manager /out/healthcheck

FROM scratch

COPY --from=builder /out/ /

USER appuser

HEALTHCHECK --interval=10s --timeout=2s --start-period=20s --retries=3 \
    CMD ["/healthcheck", "-url", "http://127.0.0.1:8080/readyz", "-timeout", "2s"]

ENTRYPOINT ["/appointment-manager"]
