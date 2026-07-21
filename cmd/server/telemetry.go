package main

import (
	"appointment-manager/internal/tracing"
	"context"
	"log/slog"
	"os"
	"strings"
	"time"
)

const tracingShutdownTimeout = 5 * time.Second

// startTracing initialises OpenTelemetry tracing from the environment and
// returns a stop func that flushes buffered spans on shutdown. When
// OTEL_EXPORTER_OTLP_ENDPOINT is unset, tracing is disabled and stop is a no-op,
// so the call is safe to wire unconditionally.
func startTracing(ctx context.Context, logger *slog.Logger) (func(), error) {
	endpoint := strings.TrimSpace(os.Getenv(otelEndpointEnv))

	sampleRatio, err := parseSampleRatio(os.Getenv(otelSampleRatioEnv))
	if err != nil {
		return nil, err
	}

	shutdown, err := tracing.Init(ctx, tracing.Config{
		Endpoint:       endpoint,
		ServiceName:    serviceName,
		ServiceVersion: parseServiceVersion(os.Getenv(otelServiceVersionEnv)),
		SampleRatio:    sampleRatio,
	})
	if err != nil {
		return nil, err
	}

	if endpoint == "" {
		logger.WarnContext(ctx, "tracing disabled", slog.String("reason", otelEndpointEnv+" not set"))
	} else {
		logger.InfoContext(ctx, "tracing enabled",
			slog.String("endpoint", endpoint),
			slog.Float64("sample_ratio", sampleRatio),
		)
	}

	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), tracingShutdownTimeout)
		defer cancel()

		if err := shutdown(shutdownCtx); err != nil {
			logger.ErrorContext(shutdownCtx, "failed to shut down tracing", slog.Any("error", err))
		}
		logger.InfoContext(shutdownCtx, "tracing shut down")
	}, nil
}
