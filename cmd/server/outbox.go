package main

import (
	"appointment-manager/internal/outbox"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// initializeOutboxRelay builds the relay that drains public.outbox. Handlers
// will be registered when internal/notification migrates off in-request sends.
func initializeOutboxRelay(logger *slog.Logger, pool *pgxpool.Pool) (*outbox.Relay, error) {
	batchSize, err := parseOutboxBatchSize(os.Getenv(outboxBatchSizeEnv))
	if err != nil {
		return nil, err
	}

	relay, err := outbox.NewRelay(logger, pool, batchSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create outbox relay: %w", err)
	}

	return relay, nil
}
