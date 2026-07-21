package main

import (
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	databaseURLEnv              = "DATABASE_URL"
	environmentEnv              = "ENV"
	environmentDevelopment      = "development"
	logLevelEnv                 = "LOG_LEVEL"
	workerIntervalEnv           = "WORKER_TICKER_INTERVAL"
	defaultWorkerTickerInterval = 30 * time.Minute
	metricsAddrEnv              = "METRICS_ADDR"
	defaultMetricsAddr          = ":9090"

	serviceName             = "appointment-manager"
	otelEndpointEnv         = "OTEL_EXPORTER_OTLP_ENDPOINT"
	otelSampleRatioEnv      = "OTEL_TRACES_SAMPLE_RATIO"
	otelServiceVersionEnv   = "OTEL_SERVICE_VERSION"
	defaultServiceVersion   = "dev"
	defaultTraceSampleRatio = 1.0
)

// parseLogLevel reads LOG_LEVEL ("debug", "info", "warn", "error", case
// insensitive). When unset it falls back to debug, matching the default
// development experience.
func parseLogLevel(raw string) (slog.Level, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return slog.LevelDebug, nil
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		return 0, fmt.Errorf("invalid %s: %w", logLevelEnv, err)
	}

	return level, nil
}

// parseWorkerInterval reads WORKER_TICKER_INTERVAL as a Go duration string (e.g.
// "30m", "1h"). When unset it falls back to defaultWorkerTickerInterval; a
// malformed or non-positive value is rejected so misconfiguration fails fast.
func parseWorkerInterval(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultWorkerTickerInterval, nil
	}

	interval, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", workerIntervalEnv, err)
	}
	if interval <= 0 {
		return 0, fmt.Errorf("invalid %s: must be greater than zero", workerIntervalEnv)
	}

	return interval, nil
}

// stringOrDefault trims raw and returns def when the result is empty, the shared
// shape behind the env vars that only need a trimmed value or a fallback.
func stringOrDefault(raw, def string) string {
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		return trimmed
	}

	return def
}

// parseMetricsAddr reads METRICS_ADDR (the listen address for the Prometheus
// metrics server, e.g. ":9090"). When unset it falls back to defaultMetricsAddr.
func parseMetricsAddr(raw string) string {
	return stringOrDefault(raw, defaultMetricsAddr)
}

// parseSampleRatio reads OTEL_TRACES_SAMPLE_RATIO as the head-based trace
// sampling probability. When unset it falls back to defaultTraceSampleRatio; a
// malformed value or one outside [0,1] is rejected so misconfiguration fails
// fast.
func parseSampleRatio(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultTraceSampleRatio, nil
	}

	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", otelSampleRatioEnv, err)
	}
	if math.IsNaN(ratio) || ratio < 0 || ratio > 1 {
		return 0, fmt.Errorf("invalid %s: must be within [0,1]", otelSampleRatioEnv)
	}

	return ratio, nil
}

// parseServiceVersion reads OTEL_SERVICE_VERSION, the release identifier
// attached to spans. When unset it falls back to defaultServiceVersion.
func parseServiceVersion(raw string) string {
	return stringOrDefault(raw, defaultServiceVersion)
}
