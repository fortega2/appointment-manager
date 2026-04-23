FROM golang:1.26.2-alpine3.23 AS builder

RUN adduser -D -g '' -u 10001 appuser

WORKDIR /app

ARG TARGETOS
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /app/appointment-manager-api ./cmd/api/main.go

FROM scratch

COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /app/appointment-manager-api /appointment-manager-api

USER appuser

ENTRYPOINT ["/appointment-manager-api"]
