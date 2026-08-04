package db_test

import (
	"appointment-manager/internal/db"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	dbInvalidDatabaseURL = "://invalid-url"
	dbUnknownSchemeURL   = "mysql://localhost:3306/app"
	dbValidDatabaseURL   = "postgres://localhost:5432/app?sslmode=disable"

	// Values picked to differ from every pgx default, so an option that silently
	// did nothing would fail the assertion rather than pass by coincidence.
	dbOptionMaxConns int32 = 14
	dbOptionMinConns int32 = 3
	dbOptionDuration       = 7 * time.Minute
)

func TestNewPostgresPoolValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		ctx         context.Context
		databaseURL string
		opts        []db.Option
		expectedErr error
	}{
		{
			name:        "nil context",
			ctx:         nil,
			databaseURL: dbValidDatabaseURL,
			expectedErr: db.ErrNilContext,
		},
		{
			name:        "empty database url",
			ctx:         context.Background(),
			databaseURL: "   ",
			expectedErr: db.ErrEmptyDatabaseURL,
		},
		{
			name:        "invalid database url",
			ctx:         context.Background(),
			databaseURL: dbInvalidDatabaseURL,
			expectedErr: db.ErrInvalidDatabaseURL,
		},
		{
			name:        "unknown database url scheme",
			ctx:         context.Background(),
			databaseURL: dbUnknownSchemeURL,
			expectedErr: db.ErrUnknownDatabaseScheme,
		},
		{
			// pgx does not check this pair itself: left alone it would leave the
			// health check unable to ever reach the minimum, silently. Rejecting it
			// here also proves the config is validated before the migrations run,
			// since this case never reaches a database.
			name:        "min conns above max conns",
			ctx:         context.Background(),
			databaseURL: dbValidDatabaseURL,
			opts:        []db.Option{db.WithMaxConns(2), db.WithMinConns(10)},
			expectedErr: db.ErrMinConnsAboveMaxConns,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool, err := db.NewPostgresPool(tt.ctx, tt.databaseURL, tt.opts...)

			require.Error(t, err)
			assert.Nil(t, pool)
			assert.True(t, errors.Is(err, tt.expectedErr))
		})
	}
}

type stubTracer struct{}

func (stubTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (stubTracer) TraceQueryEnd(_ context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {}

func TestWithQueryTracer(t *testing.T) {
	t.Parallel()

	cfg, err := pgxpool.ParseConfig(dbValidDatabaseURL)
	require.NoError(t, err)
	require.Nil(t, cfg.ConnConfig.Tracer)

	db.WithQueryTracer(stubTracer{})(cfg)

	assert.NotNil(t, cfg.ConnConfig.Tracer)
}

// Each option must move its own field and leave the rest of the pgx defaults
// alone, so callers can set one knob without silently adopting five others.
func TestPoolOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		option db.Option
		assert func(t *testing.T, cfg *pgxpool.Config)
	}{
		{
			name:   "max conns",
			option: db.WithMaxConns(dbOptionMaxConns),
			assert: func(t *testing.T, cfg *pgxpool.Config) {
				t.Helper()
				assert.Equal(t, dbOptionMaxConns, cfg.MaxConns)
			},
		},
		{
			name:   "min conns",
			option: db.WithMinConns(dbOptionMinConns),
			assert: func(t *testing.T, cfg *pgxpool.Config) {
				t.Helper()
				assert.Equal(t, dbOptionMinConns, cfg.MinConns)
			},
		},
		{
			name:   "max conn lifetime",
			option: db.WithMaxConnLifetime(dbOptionDuration),
			assert: func(t *testing.T, cfg *pgxpool.Config) {
				t.Helper()
				assert.Equal(t, dbOptionDuration, cfg.MaxConnLifetime)
			},
		},
		{
			name:   "max conn lifetime jitter",
			option: db.WithMaxConnLifetimeJitter(dbOptionDuration),
			assert: func(t *testing.T, cfg *pgxpool.Config) {
				t.Helper()
				assert.Equal(t, dbOptionDuration, cfg.MaxConnLifetimeJitter)
			},
		},
		{
			name:   "max conn idle time",
			option: db.WithMaxConnIdleTime(dbOptionDuration),
			assert: func(t *testing.T, cfg *pgxpool.Config) {
				t.Helper()
				assert.Equal(t, dbOptionDuration, cfg.MaxConnIdleTime)
			},
		},
		{
			name:   "health check period",
			option: db.WithHealthCheckPeriod(dbOptionDuration),
			assert: func(t *testing.T, cfg *pgxpool.Config) {
				t.Helper()
				assert.Equal(t, dbOptionDuration, cfg.HealthCheckPeriod)
			},
		},
	}

	defaults, err := pgxpool.ParseConfig(dbValidDatabaseURL)
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := pgxpool.ParseConfig(dbValidDatabaseURL)
			require.NoError(t, err)

			tt.option(cfg)

			tt.assert(t, cfg)
			assertOnlyChangedField(t, defaults, cfg, tt.name)
		})
	}
}

// assertOnlyChangedField checks every pool setting except the one the subtest
// deliberately moved still matches what pgx put there.
func assertOnlyChangedField(t *testing.T, defaults, got *pgxpool.Config, changed string) {
	t.Helper()

	untouched := map[string]func(){
		"max conns":                func() { assert.Equal(t, defaults.MaxConns, got.MaxConns) },
		"min conns":                func() { assert.Equal(t, defaults.MinConns, got.MinConns) },
		"max conn lifetime":        func() { assert.Equal(t, defaults.MaxConnLifetime, got.MaxConnLifetime) },
		"max conn lifetime jitter": func() { assert.Equal(t, defaults.MaxConnLifetimeJitter, got.MaxConnLifetimeJitter) },
		"max conn idle time":       func() { assert.Equal(t, defaults.MaxConnIdleTime, got.MaxConnIdleTime) },
		"health check period":      func() { assert.Equal(t, defaults.HealthCheckPeriod, got.HealthCheckPeriod) },
	}

	for field, check := range untouched {
		if field == changed {
			continue
		}
		check()
	}
}
