package main

import (
	"appointment-manager/internal/notification"
	"appointment-manager/internal/outbox"
	"appointment-manager/internal/slot"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func initializeOutboxRelay(
	logger *slog.Logger,
	pool *pgxpool.Pool,
	notificationService *notification.Service,
) (*outbox.Relay, error) {
	batchSize, err := parseOutboxBatchSize(os.Getenv(outboxBatchSizeEnv))
	if err != nil {
		return nil, err
	}

	relay, err := outbox.NewRelay(logger, pool, batchSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create outbox relay: %w", err)
	}

	if err := relay.Register(slot.EventAppointmentsCancelled, notificationService.SendSlotCancelled); err != nil {
		return nil, fmt.Errorf("failed to register %s handler: %w", slot.EventAppointmentsCancelled, err)
	}

	return relay, nil
}
