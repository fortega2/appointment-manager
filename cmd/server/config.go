package main

import (
	"appointment-manager/internal/db"
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/mailer"
	"appointment-manager/internal/ratelimit"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
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

	outboxDrainIntervalEnv = "OUTBOX_DRAIN_INTERVAL"
	outboxBatchSizeEnv     = "OUTBOX_BATCH_SIZE"
	// Keeps retries below the 5-minute backoff ceiling. See ADR 0003.
	defaultOutboxDrainInterval = 15 * time.Second
	defaultOutboxBatchSize     = 20

	loginRateLimitEnabledEnv       = "LOGIN_RATE_LIMIT_ENABLED"
	loginRateLimitAccountBurstEnv  = "LOGIN_RATE_LIMIT_ACCOUNT_BURST"
	loginRateLimitAccountRefillEnv = "LOGIN_RATE_LIMIT_ACCOUNT_REFILL"
	loginRateLimitIPBurstEnv       = "LOGIN_RATE_LIMIT_IP_BURST"
	loginRateLimitIPRefillEnv      = "LOGIN_RATE_LIMIT_IP_REFILL"
	loginRateLimitMaxEntriesEnv    = "LOGIN_RATE_LIMIT_MAX_ENTRIES"
	// Allows brief mistakes while slowing guesses; IP limits are looser for
	// shared addresses. Successful logins refill the account limit.
	defaultLoginRateLimitAccountBurst  = 5
	defaultLoginRateLimitAccountRefill = 3 * time.Minute
	defaultLoginRateLimitIPBurst       = 20
	defaultLoginRateLimitIPRefill      = 30 * time.Second
	// Bounds limiter memory against attacker-generated keys.
	defaultLoginRateLimitMaxEntries = 10_000

	appBaseURLEnv = "APP_BASE_URL"

	smtpHostEnv        = "SMTP_HOST"
	smtpPortEnv        = "SMTP_PORT"
	smtpUsernameEnv    = "SMTP_USERNAME"
	smtpPasswordEnv    = "SMTP_PASSWORD" //nolint:gosec // G101 false positive: an env var name, not a credential.
	smtpFromAddressEnv = "SMTP_FROM_ADDRESS"
	smtpFromNameEnv    = "SMTP_FROM_NAME"
	smtpUseTLSEnv      = "SMTP_USE_TLS"

	passwordResetTokenTTLEnv = "PASSWORD_RESET_TOKEN_TTL" //nolint:gosec // G101 false positive: an env var name, not a credential.
	// Thirty minutes is long enough to walk away from the screen and short
	// enough that a link left in an inbox stops being a key.
	defaultPasswordResetTokenTTL = 30 * time.Minute

	passwordResetRateLimitAccountBurstEnv  = "PASSWORD_RESET_RATE_LIMIT_ACCOUNT_BURST"
	passwordResetRateLimitAccountRefillEnv = "PASSWORD_RESET_RATE_LIMIT_ACCOUNT_REFILL"
	passwordResetRateLimitIPBurstEnv       = "PASSWORD_RESET_RATE_LIMIT_IP_BURST"
	passwordResetRateLimitIPRefillEnv      = "PASSWORD_RESET_RATE_LIMIT_IP_REFILL"
	// Tighter than the login allowance on purpose: a person asks for a reset
	// once, and every granted request puts a mail in somebody else's inbox.
	defaultPasswordResetRateLimitAccountBurst  = 3
	defaultPasswordResetRateLimitAccountRefill = 15 * time.Minute
	defaultPasswordResetRateLimitIPBurst       = 10
	defaultPasswordResetRateLimitIPRefill      = 5 * time.Minute

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
	return parsePositiveDuration(raw, workerIntervalEnv, defaultWorkerTickerInterval)
}

// parseOutboxDrainInterval reads OUTBOX_DRAIN_INTERVAL, overriding the group's
// default interval for the outbox drain job specifically.
func parseOutboxDrainInterval(raw string) (time.Duration, error) {
	return parsePositiveDuration(raw, outboxDrainIntervalEnv, defaultOutboxDrainInterval)
}

// parseOutboxBatchSize reads OUTBOX_BATCH_SIZE, the number of events the drain
// claims per run.
func parseOutboxBatchSize(raw string) (int, error) {
	return parsePositiveInt(raw, outboxBatchSizeEnv, defaultOutboxBatchSize)
}

// parseLoginRateLimit reads LOGIN_RATE_LIMIT_* variables, using defaults for
// unset values and rejecting malformed or non-positive values.
func parseLoginRateLimit(getenv func(string) string) (ratelimit.Config, error) {
	// An unset variable must never read as "off": the only way to disable the
	// limiter is to say so.
	enabled, err := parseBool(getenv(loginRateLimitEnabledEnv), loginRateLimitEnabledEnv, true)
	if err != nil {
		return ratelimit.Config{}, err
	}

	accountBurst, err := parsePositiveInt(getenv(loginRateLimitAccountBurstEnv), loginRateLimitAccountBurstEnv, defaultLoginRateLimitAccountBurst)
	if err != nil {
		return ratelimit.Config{}, err
	}

	accountRefill, err := parsePositiveDuration(getenv(loginRateLimitAccountRefillEnv), loginRateLimitAccountRefillEnv, defaultLoginRateLimitAccountRefill)
	if err != nil {
		return ratelimit.Config{}, err
	}

	ipBurst, err := parsePositiveInt(getenv(loginRateLimitIPBurstEnv), loginRateLimitIPBurstEnv, defaultLoginRateLimitIPBurst)
	if err != nil {
		return ratelimit.Config{}, err
	}

	ipRefill, err := parsePositiveDuration(getenv(loginRateLimitIPRefillEnv), loginRateLimitIPRefillEnv, defaultLoginRateLimitIPRefill)
	if err != nil {
		return ratelimit.Config{}, err
	}

	maxEntries, err := parsePositiveInt(getenv(loginRateLimitMaxEntriesEnv), loginRateLimitMaxEntriesEnv, defaultLoginRateLimitMaxEntries)
	if err != nil {
		return ratelimit.Config{}, err
	}

	return ratelimit.Config{
		Enabled:       enabled,
		AccountBurst:  accountBurst,
		AccountRefill: accountRefill,
		IPBurst:       ipBurst,
		IPRefill:      ipRefill,
		MaxEntries:    maxEntries,
	}, nil
}

// parseBool reads an optional boolean, falling back to def when unset.
func parseBool(raw, name string, def bool) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}

	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", name, err)
	}

	return parsed, nil
}

// parsePositiveInt reads an optional count that must be at least one, falling
// back to def when unset.
func parsePositiveInt(raw, name string, def int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}

	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("invalid %s: must be greater than zero", name)
	}

	return parsed, nil
}

// parsePositiveDuration reads an optional Go duration string that must be above
// zero, falling back to def when unset.
func parsePositiveDuration(raw, name string, def time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("invalid %s: must be greater than zero", name)
	}

	return parsed, nil
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

// parsePoolConfig converts optional DB_POOL_* values into pgx options, preserving
// pgx defaults for omitted values and rejecting malformed or out-of-range ones.
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

// parseAppBaseURL reads the origin the reset link is built from. It is required
// and never derived from the request: a forged Host header would otherwise put
// an attacker's domain in a mail the user trusts. See ADR 0010.
func parseAppBaseURL(getenv func(string) string) (string, error) {
	raw := strings.TrimSpace(getenv(appBaseURLEnv))
	if raw == "" {
		return "", fmt.Errorf("%s is required", appBaseURLEnv)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s is not a valid url: %w", appBaseURLEnv, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("%s must be http or https, got %q", appBaseURLEnv, parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("%s must include a host", appBaseURLEnv)
	}

	return strings.TrimRight(raw, "/"), nil
}

func parseSMTPConfig(getenv func(string) string) (mailer.Config, error) {
	port, err := parsePositiveInt(getenv(smtpPortEnv), smtpPortEnv, mailer.DefaultPort)
	if err != nil {
		return mailer.Config{}, err
	}

	useTLS, err := parseBool(getenv(smtpUseTLSEnv), smtpUseTLSEnv, true)
	if err != nil {
		return mailer.Config{}, err
	}

	return mailer.Config{
		Host:        strings.TrimSpace(getenv(smtpHostEnv)),
		Username:    strings.TrimSpace(getenv(smtpUsernameEnv)),
		Password:    getenv(smtpPasswordEnv),
		FromAddress: strings.TrimSpace(getenv(smtpFromAddressEnv)),
		FromName:    strings.TrimSpace(getenv(smtpFromNameEnv)),
		Port:        port,
		UseTLS:      useTLS,
	}, nil
}

func parsePasswordResetTokenTTL(getenv func(string) string) (time.Duration, error) {
	return parsePositiveDuration(
		getenv(passwordResetTokenTTLEnv),
		passwordResetTokenTTLEnv,
		defaultPasswordResetTokenTTL,
	)
}

// parsePasswordResetRateLimit builds a second limiter rather than sharing the
// login one. See ADR 0010.
func parsePasswordResetRateLimit(getenv func(string) string) (ratelimit.Config, error) {
	accountBurst, err := parsePositiveInt(
		getenv(passwordResetRateLimitAccountBurstEnv),
		passwordResetRateLimitAccountBurstEnv,
		defaultPasswordResetRateLimitAccountBurst,
	)
	if err != nil {
		return ratelimit.Config{}, err
	}

	accountRefill, err := parsePositiveDuration(
		getenv(passwordResetRateLimitAccountRefillEnv),
		passwordResetRateLimitAccountRefillEnv,
		defaultPasswordResetRateLimitAccountRefill,
	)
	if err != nil {
		return ratelimit.Config{}, err
	}

	ipBurst, err := parsePositiveInt(
		getenv(passwordResetRateLimitIPBurstEnv),
		passwordResetRateLimitIPBurstEnv,
		defaultPasswordResetRateLimitIPBurst,
	)
	if err != nil {
		return ratelimit.Config{}, err
	}

	ipRefill, err := parsePositiveDuration(
		getenv(passwordResetRateLimitIPRefillEnv),
		passwordResetRateLimitIPRefillEnv,
		defaultPasswordResetRateLimitIPRefill,
	)
	if err != nil {
		return ratelimit.Config{}, err
	}

	// The reset limiter reuses the login entry cap: both track the same kind of
	// key and the same attacker invents them.
	maxEntries, err := parsePositiveInt(
		getenv(loginRateLimitMaxEntriesEnv),
		loginRateLimitMaxEntriesEnv,
		defaultLoginRateLimitMaxEntries,
	)
	if err != nil {
		return ratelimit.Config{}, err
	}

	return ratelimit.Config{
		Enabled:       true,
		AccountBurst:  accountBurst,
		AccountRefill: accountRefill,
		IPBurst:       ipBurst,
		IPRefill:      ipRefill,
		MaxEntries:    maxEntries,
	}, nil
}
