package main

import (
	"appointment-manager/internal/db"
	"appointment-manager/internal/i18n"
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	unsetValue    = ""
	notANumber    = "not-a-number"
	validInterval = "45s"
	validBuffer   = "250"

	poolDatabaseURL = "postgres://localhost:5432/app?sslmode=disable"
	blankValue      = "   "
	zeroDuration    = "0s"
	negativeCount   = "-1"
	// One past math.MaxInt32, which the pool counts cannot hold.
	overflowCount = "2147483648"
)

func TestParseSampleRatio(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    float64
		wantErr bool
	}{
		{name: "unset falls back to default", raw: "", want: defaultTraceSampleRatio},
		{name: "valid mid-range ratio", raw: "0.25", want: 0.25},
		{name: "valid boundary zero", raw: "0", want: 0},
		{name: "valid boundary one", raw: "1", want: 1},
		{name: "negative is rejected", raw: "-0.1", wantErr: true},
		{name: "above one is rejected", raw: "1.1", wantErr: true},
		{name: "malformed is rejected", raw: "not-a-number", wantErr: true},
		{name: "NaN is rejected", raw: "NaN", wantErr: true},
		{name: "positive infinity is rejected", raw: "+Inf", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseSampleRatio(tt.raw)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.InDelta(t, tt.want, got, 0)
		})
	}
}

func TestParseDefaultLocale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    i18n.Locale
		wantErr bool
	}{
		{name: "unset falls back to default", raw: unsetValue, want: defaultLocale},
		{name: "blank falls back to default", raw: blankValue, want: defaultLocale},
		{name: "spanish", raw: "es", want: i18n.LocaleES},
		{name: "english", raw: "en", want: i18n.LocaleEN},
		{name: "surrounding space is trimmed", raw: "  en  ", want: i18n.LocaleEN},
		{name: "region is not a supported code", raw: "es-AR", wantErr: true},
		{name: "unsupported language is rejected", raw: "fr", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseDefaultLocale(tt.raw)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseNotificationConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		rawInterval  string
		rawBuffer    string
		wantInterval time.Duration
		wantBuffer   int
		wantErr      bool
	}{
		{
			name:         "both unset fall back to defaults",
			rawInterval:  unsetValue,
			rawBuffer:    unsetValue,
			wantInterval: defaultNotificationTickerInterval,
			wantBuffer:   defaultNotificationBufferSize,
		},
		{
			name:         "both set are honoured",
			rawInterval:  validInterval,
			rawBuffer:    validBuffer,
			wantInterval: 45 * time.Second,
			wantBuffer:   250,
		},
		{
			// Each falls back on its own, so setting one does not force the other.
			name:         "only the interval is set",
			rawInterval:  validInterval,
			rawBuffer:    unsetValue,
			wantInterval: 45 * time.Second,
			wantBuffer:   defaultNotificationBufferSize,
		},
		{
			name:         "whitespace counts as unset",
			rawInterval:  "   ",
			rawBuffer:    "  ",
			wantInterval: defaultNotificationTickerInterval,
			wantBuffer:   defaultNotificationBufferSize,
		},
		{name: "malformed interval is rejected", rawInterval: notANumber, rawBuffer: validBuffer, wantErr: true},
		{name: "zero interval is rejected", rawInterval: "0s", rawBuffer: validBuffer, wantErr: true},
		{name: "negative interval is rejected", rawInterval: "-1m", rawBuffer: validBuffer, wantErr: true},
		{name: "malformed buffer is rejected", rawInterval: validInterval, rawBuffer: notANumber, wantErr: true},
		{name: "zero buffer is rejected", rawInterval: validInterval, rawBuffer: "0", wantErr: true},
		{name: "negative buffer is rejected", rawInterval: validInterval, rawBuffer: "-1", wantErr: true},
		{name: "fractional buffer is rejected", rawInterval: validInterval, rawBuffer: "1.5", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger := slog.New(slog.DiscardHandler)

			interval, buffer, err := parseNotificationConfig(logger, tt.rawInterval, tt.rawBuffer)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantInterval, interval)
			assert.Equal(t, tt.wantBuffer, buffer)
		})
	}
}

// poolSettings is the slice of pgx pool config the DB_POOL_* variables can move.
// Comparing the whole struct in one assertion is what proves an option changed
// its own field and nothing else.
type poolSettings struct {
	maxConns              int32
	minConns              int32
	maxConnLifetime       time.Duration
	maxConnLifetimeJitter time.Duration
	maxConnIdleTime       time.Duration
	healthCheckPeriod     time.Duration
}

// applyPoolOptions resolves options against a real pgx config, so the settings
// under test are the effective ones: ParseConfig has already filled in pgx's
// defaults, exactly as it does at startup.
func applyPoolOptions(t *testing.T, options []db.Option) poolSettings {
	t.Helper()

	cfg, err := pgxpool.ParseConfig(poolDatabaseURL)
	require.NoError(t, err)

	for _, option := range options {
		option(cfg)
	}

	return poolSettings{
		maxConns:              cfg.MaxConns,
		minConns:              cfg.MinConns,
		maxConnLifetime:       cfg.MaxConnLifetime,
		maxConnLifetimeJitter: cfg.MaxConnLifetimeJitter,
		maxConnIdleTime:       cfg.MaxConnIdleTime,
		healthCheckPeriod:     cfg.HealthCheckPeriod,
	}
}

func TestParsePoolConfig(t *testing.T) {
	t.Parallel()

	// pgx derives the connection ceiling from the host's CPU count, so the
	// baseline has to be read back rather than written down.
	defaults := applyPoolOptions(t, nil)

	tests := []struct {
		name string
		raw  poolConfig
		want func(poolSettings) poolSettings
	}{
		{
			name: "all unset keeps every pgx default",
			raw:  poolConfig{},
			want: func(s poolSettings) poolSettings { return s },
		},
		{
			// Whitespace counts as unset everywhere else in this file; a variable
			// left blank in a .env must not take the process down.
			name: "blank values count as unset",
			raw: poolConfig{
				MaxConns:              blankValue,
				MinConns:              blankValue,
				MaxConnLifetime:       blankValue,
				MaxConnLifetimeJitter: blankValue,
				MaxConnIdleTime:       blankValue,
				HealthCheckPeriod:     blankValue,
			},
			want: func(s poolSettings) poolSettings { return s },
		},
		{
			name: "max conns is honoured on its own",
			raw:  poolConfig{MaxConns: "14"},
			want: func(s poolSettings) poolSettings { s.maxConns = 14; return s },
		},
		{
			name: "min conns is honoured on its own",
			raw:  poolConfig{MinConns: "2"},
			want: func(s poolSettings) poolSettings { s.minConns = 2; return s },
		},
		{
			// Zero is what pgx already defaults to, and it means "keep nothing
			// warm" -- a real choice, not a missing value.
			name: "min conns accepts zero",
			raw:  poolConfig{MinConns: "0"},
			want: func(s poolSettings) poolSettings { s.minConns = 0; return s },
		},
		{
			name: "max conn lifetime is honoured on its own",
			raw:  poolConfig{MaxConnLifetime: "30m"},
			want: func(s poolSettings) poolSettings { s.maxConnLifetime = 30 * time.Minute; return s },
		},
		{
			name: "max conn lifetime jitter is honoured on its own",
			raw:  poolConfig{MaxConnLifetimeJitter: "5m"},
			want: func(s poolSettings) poolSettings { s.maxConnLifetimeJitter = 5 * time.Minute; return s },
		},
		{
			// Unlike the timeouts, zero jitter is pgx's own default and a
			// legitimate request for no spread at all.
			name: "max conn lifetime jitter accepts zero",
			raw:  poolConfig{MaxConnLifetimeJitter: zeroDuration},
			want: func(s poolSettings) poolSettings { s.maxConnLifetimeJitter = 0; return s },
		},
		{
			name: "max conn idle time is honoured on its own",
			raw:  poolConfig{MaxConnIdleTime: "5m"},
			want: func(s poolSettings) poolSettings { s.maxConnIdleTime = 5 * time.Minute; return s },
		},
		{
			name: "health check period is honoured on its own",
			raw:  poolConfig{HealthCheckPeriod: "30s"},
			want: func(s poolSettings) poolSettings { s.healthCheckPeriod = 30 * time.Second; return s },
		},
		{
			name: "every variable set at once",
			raw: poolConfig{
				MaxConns:              "5",
				MinConns:              "1",
				MaxConnLifetime:       "30m",
				MaxConnLifetimeJitter: "5m",
				MaxConnIdleTime:       "5m",
				HealthCheckPeriod:     "30s",
			},
			want: func(poolSettings) poolSettings {
				return poolSettings{
					maxConns:              5,
					minConns:              1,
					maxConnLifetime:       30 * time.Minute,
					maxConnLifetimeJitter: 5 * time.Minute,
					maxConnIdleTime:       5 * time.Minute,
					healthCheckPeriod:     30 * time.Second,
				}
			},
		},
		{
			// Surrounding whitespace is trimmed rather than handed to the parser.
			name: "values are trimmed before parsing",
			raw:  poolConfig{MaxConns: " 5 ", MaxConnLifetime: " 30m "},
			want: func(s poolSettings) poolSettings {
				s.maxConns = 5
				s.maxConnLifetime = 30 * time.Minute

				return s
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			options, err := parsePoolConfig(tt.raw)
			require.NoError(t, err)

			assert.Equal(t, tt.want(defaults), applyPoolOptions(t, options))
		})
	}
}

// A malformed or out-of-range value is a misconfiguration, not an omission: it
// must stop the process instead of quietly falling back to a pgx default the
// operator did not ask for.
func TestParsePoolConfigRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     poolConfig
		wantEnv string
	}{
		{name: "malformed max conns", raw: poolConfig{MaxConns: notANumber}, wantEnv: dbPoolMaxConnsEnv},
		{name: "zero max conns", raw: poolConfig{MaxConns: "0"}, wantEnv: dbPoolMaxConnsEnv},
		{name: "negative max conns", raw: poolConfig{MaxConns: negativeCount}, wantEnv: dbPoolMaxConnsEnv},
		{name: "max conns beyond int32", raw: poolConfig{MaxConns: overflowCount}, wantEnv: dbPoolMaxConnsEnv},
		{name: "fractional max conns", raw: poolConfig{MaxConns: "1.5"}, wantEnv: dbPoolMaxConnsEnv},
		{name: "malformed min conns", raw: poolConfig{MinConns: notANumber}, wantEnv: dbPoolMinConnsEnv},
		{name: "negative min conns", raw: poolConfig{MinConns: negativeCount}, wantEnv: dbPoolMinConnsEnv},
		{
			name:    "malformed max conn lifetime",
			raw:     poolConfig{MaxConnLifetime: notANumber},
			wantEnv: dbPoolMaxConnLifetimeEnv,
		},
		{
			name:    "zero max conn lifetime",
			raw:     poolConfig{MaxConnLifetime: zeroDuration},
			wantEnv: dbPoolMaxConnLifetimeEnv,
		},
		{
			name:    "negative max conn lifetime jitter",
			raw:     poolConfig{MaxConnLifetimeJitter: "-1m"},
			wantEnv: dbPoolMaxConnLifetimeJitterEnv,
		},
		{
			// Zero does not mean "no idle limit" to pgx: the health check would
			// destroy every idle connection on every sweep.
			name:    "zero max conn idle time",
			raw:     poolConfig{MaxConnIdleTime: zeroDuration},
			wantEnv: dbPoolMaxConnIdleTimeEnv,
		},
		{
			name:    "negative max conn idle time",
			raw:     poolConfig{MaxConnIdleTime: "-1m"},
			wantEnv: dbPoolMaxConnIdleTimeEnv,
		},
		{
			name:    "zero health check period",
			raw:     poolConfig{HealthCheckPeriod: zeroDuration},
			wantEnv: dbPoolHealthCheckPeriodEnv,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			options, err := parsePoolConfig(tt.raw)

			require.Error(t, err)
			assert.Nil(t, options)
			// The message must name the offending variable, or the operator is
			// left guessing which of the six is wrong.
			assert.Contains(t, err.Error(), tt.wantEnv)
		})
	}
}

// A default nobody chose governs how often notifications go out and when they
// start being dropped, so falling back must be visible in the logs rather than
// silent.
func TestParseNotificationConfigWarnsOnEachFallback(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

	_, _, err := parseNotificationConfig(logger, unsetValue, unsetValue)
	require.NoError(t, err)

	assert.Contains(t, logs.String(), notificationTickerIntervalEnv)
	assert.Contains(t, logs.String(), notificationBufferSizeEnv)

	logs.Reset()
	_, _, err = parseNotificationConfig(logger, validInterval, validBuffer)
	require.NoError(t, err)

	assert.Empty(t, logs.String(), "nothing to warn about when both are configured")
}
