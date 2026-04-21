FROM golang:1.26.2-alpine3.23 AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /app/appointment-manager-api ./cmd/api/main.go

FROM scratch

COPY --from=builder /app/appointment-manager-api /appointment-manager-api

ENTRYPOINT ["/appointment-manager-api"]
