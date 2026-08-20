package main

import (
	"appointment-manager/internal/mailer"
	"context"
	"fmt"
	"log/slog"
	"os"
)

// initializeMailer builds the SMTP client and probes the relay. A bad config
// stops the process; an unreachable relay only logs, because password reset is
// not worth taking the appointment book down for. See ADR 0010.
func initializeMailer(ctx context.Context, logger *slog.Logger) (*mailer.Client, error) {
	cfg, err := parseSMTPConfig(os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("failed to parse smtp configuration: %w", err)
	}

	client, err := mailer.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create smtp client: %w", err)
	}

	if err := client.VerifyConnection(ctx); err != nil {
		logger.ErrorContext(ctx, "smtp relay is not reachable, password reset mails will fail",
			slog.String("host", cfg.Host),
			slog.Int("port", cfg.Port),
			slog.Any("error", err))
	} else {
		logger.InfoContext(ctx, "smtp relay reachable",
			slog.String("host", cfg.Host),
			slog.Int("port", cfg.Port),
			slog.Bool("tls", cfg.UseTLS))
	}

	return client, nil
}
