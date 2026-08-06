package main

import (
	"appointment-manager/internal/db"
	"appointment-manager/internal/i18n"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
	defaultLocaleEnv            = "DEFAULT_LOCALE"
	defaultLocale               = i18n.LocaleES

	serviceName             = "appointment-manager"
	otelEndpointEnv         = "OTEL_EXPORTER_OTLP_ENDPOINT"
	otelSampleRatioEnv      = "OTEL_TRACES_SAMPLE_RATIO"
	otelServiceVersionEnv   = "OTEL_SERVICE_VERSION"
	defaultServiceVersion   = "dev"
	defaultTraceSampleRatio = 1.0

	notificationTickerIntervalEnv = "NOTIFICATION_TICKER_INTERVAL"
	notificationBufferSizeEnv     = "NOTIFICATION_BUFFER_SIZE"
	// Notifications are drained far more often than the appointment sweeps: a
	// patient learning their appointment is off is time-sensitive, and the queue
	// is in-memory, so a short interval also shrinks the window in which a crash
	// loses events. An empty drain costs one channel poll, so ticking often is
	// close to free.
	defaultNotificationTickerInterval = time.Minute
	defaultNotificationBufferSize     = 100

	dbPoolMaxConnsEnv              = "DB_POOL_MAX_CONNS"
	dbPoolMinConnsEnv              = "DB_POOL_MIN_CONNS"
	dbPoolMaxConnLifetimeEnv       = "DB_POOL_MAX_CONN_LIFETIME"
	dbPoolMaxConnLifetimeJitterEnv = "DB_POOL_MAX_CONN_LIFETIME_JITTER"
	dbPoolMaxConnIdleTimeEnv       = "DB_POOL_MAX_CONN_IDLE_TIME"
	dbPoolHealthCheckPeriodEnv     = "DB_POOL_HEALTH_CHECK_PERIOD"
)

// Range failures shared by the pool parsers. They carry no variable name of
// their own: the caller wraps them with the environment variable at fault, so
// the message reads the same as every other one in this file.
var (
	errMustBeGreaterThanZero = errors.New("must be greater than zero")
	errMustBeNonNegative     = errors.New("must be non-negative")
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

// parseDefaultLocale reads DEFAULT_LOCALE, the language the UI renders in when a
// request expresses no preference of its own. When unset it falls back to
// defaultLocale; an unsupported code is rejected so misconfiguration fails fast.
func parseDefaultLocale(raw string) (i18n.Locale, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultLocale, nil
	}

	locale, ok := i18n.Parse(raw)
	if !ok {
		return "", fmt.Errorf("invalid %s: %q is not a supported locale", defaultLocaleEnv, raw)
	}

	return locale, nil
}

// parseServiceVersion reads OTEL_SERVICE_VERSION, the release identifier
// attached to spans. When unset it falls back to defaultServiceVersion.
func parseServiceVersion(raw string) string {
	return stringOrDefault(raw, defaultServiceVersion)
}

// parseNotificationConfig reads NOTIFICATION_TICKER_INTERVAL (a Go duration
// string, e.g. "1m") and NOTIFICATION_BUFFER_SIZE (how many notifications may
// wait at once before new ones are dropped). Each falls back to its default
// when unset, warning as it does so: a queue draining or dropping on a value
// nobody chose should be visible in the logs rather than a silent surprise.
//
// A value that is present but malformed or non-positive is rejected instead of
// defaulted -- that is a misconfiguration, not an omission, and must fail fast.
func parseNotificationConfig(logger *slog.Logger, rawTickerInterval, rawBufferSize string) (time.Duration, int, error) {
	tickerInterval := defaultNotificationTickerInterval
	if raw := strings.TrimSpace(rawTickerInterval); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid %s: %w", notificationTickerIntervalEnv, err)
		}
		if parsed <= 0 {
			return 0, 0, fmt.Errorf("invalid %s: must be greater than zero", notificationTickerIntervalEnv)
		}
		tickerInterval = parsed
	} else {
		logger.Warn("notification ticker interval is not set, falling back to the default",
			slog.String("env", notificationTickerIntervalEnv),
			slog.Duration("default", defaultNotificationTickerInterval),
		)
	}

	bufferSize := defaultNotificationBufferSize
	if raw := strings.TrimSpace(rawBufferSize); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid %s: %w", notificationBufferSizeEnv, err)
		}
		if parsed <= 0 {
			return 0, 0, fmt.Errorf("invalid %s: must be greater than zero", notificationBufferSizeEnv)
		}
		bufferSize = parsed
	} else {
		logger.Warn("notification buffer size is not set, falling back to the default",
			slog.String("env", notificationBufferSizeEnv),
			slog.Int("default", defaultNotificationBufferSize),
		)
	}

	return tickerInterval, bufferSize, nil
}

// poolConfig carries the raw DB_POOL_* values straight from the environment, so
// parsePoolConfig stays a pure function of its input and testable without env
// vars, matching the rest of this file.
type poolConfig struct {
	MaxConns              string
	MinConns              string
	MaxConnLifetime       string
	MaxConnLifetimeJitter string
	MaxConnIdleTime       string
	HealthCheckPeriod     string
}

// parsePoolConfig turns the DB_POOL_* environment into pgx pool options. Every
// one of them is optional and independent: an unset (or blank) value yields no
// option at all, which leaves pgx's own default in place rather than a default
// of ours. That is why nothing warns here, unlike parseNotificationConfig -- the
// fallback is not a number this project picked, and the values that actually
// took effect are logged once the pool is up.
//
// A value that is present but malformed or out of range is rejected instead of
// defaulted: that is a misconfiguration, not an omission, and must fail fast.
func parsePoolConfig(raw poolConfig) ([]db.Option, error) {
	specs := []struct {
		env   string
		raw   string
		parse func(string) (db.Option, error)
	}{
		{dbPoolMaxConnsEnv, raw.MaxConns, positiveInt32(db.WithMaxConns)},
		{dbPoolMinConnsEnv, raw.MinConns, nonNegativeInt32(db.WithMinConns)},
		{dbPoolMaxConnLifetimeEnv, raw.MaxConnLifetime, positiveDuration(db.WithMaxConnLifetime)},
		{dbPoolMaxConnLifetimeJitterEnv, raw.MaxConnLifetimeJitter, nonNegativeDuration(db.WithMaxConnLifetimeJitter)},
		{dbPoolMaxConnIdleTimeEnv, raw.MaxConnIdleTime, positiveDuration(db.WithMaxConnIdleTime)},
		{dbPoolHealthCheckPeriodEnv, raw.HealthCheckPeriod, positiveDuration(db.WithHealthCheckPeriod)},
	}

	options := make([]db.Option, 0, len(specs))
	for _, spec := range specs {
		value := strings.TrimSpace(spec.raw)
		if value == "" {
			continue
		}

		option, err := spec.parse(value)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", spec.env, err)
		}

		options = append(options, option)
	}

	return options, nil
}

// positiveInt32 parses a connection count that must be at least one. ParseInt
// with a bit size of 32 is what bounds the value to the int32 pgx expects, so
// the conversion below cannot overflow.
func positiveInt32(apply func(int32) db.Option) func(string) (db.Option, error) {
	return func(value string) (db.Option, error) {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return nil, err
		}
		if parsed <= 0 {
			return nil, errMustBeGreaterThanZero
		}

		return apply(int32(parsed)), nil
	}
}

// nonNegativeInt32 parses a connection count where zero is meaningful -- for the
// pool minimum it means "keep nothing warm", which is pgx's own default.
func nonNegativeInt32(apply func(int32) db.Option) func(string) (db.Option, error) {
	return func(value string) (db.Option, error) {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return nil, err
		}
		if parsed < 0 {
			return nil, errMustBeNonNegative
		}

		return apply(int32(parsed)), nil
	}
}

// positiveDuration parses a Go duration string (e.g. "30m") that must be above
// zero. Zero is rejected rather than treated as "unbounded": for the idle and
// lifetime timeouts it means the health check retires connections on every
// sweep, which is the opposite of what someone writing 0 usually intends.
func positiveDuration(apply func(time.Duration) db.Option) func(string) (db.Option, error) {
	return func(value string) (db.Option, error) {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return nil, err
		}
		if parsed <= 0 {
			return nil, errMustBeGreaterThanZero
		}

		return apply(parsed), nil
	}
}

// nonNegativeDuration parses a Go duration string where zero is meaningful --
// for the lifetime jitter it means "no jitter", which is pgx's own default.
func nonNegativeDuration(apply func(time.Duration) db.Option) func(string) (db.Option, error) {
	return func(value string) (db.Option, error) {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return nil, err
		}
		if parsed < 0 {
			return nil, errMustBeNonNegative
		}

		return apply(parsed), nil
	}
}

// logPoolConfig reports the pool settings that ended up in force, whether they
// came from a DB_POOL_* variable or from pgx
func logPoolConfig(logger *slog.Logger, cfg *pgxpool.Config) {
	logger.Info("postgres pool configured",
		slog.Int64("max_conns", int64(cfg.MaxConns)),
		slog.Int64("min_conns", int64(cfg.MinConns)),
		slog.Duration("max_conn_lifetime", cfg.MaxConnLifetime),
		slog.Duration("max_conn_lifetime_jitter", cfg.MaxConnLifetimeJitter),
		slog.Duration("max_conn_idle_time", cfg.MaxConnIdleTime),
		slog.Duration("health_check_period", cfg.HealthCheckPeriod),
	)
}
