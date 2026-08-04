package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // Register pgx v5 migrate driver.
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Option customises the pgx pool configuration before the pool is created.
type Option func(*pgxpool.Config)

// WithQueryTracer attaches a pgx query tracer to every connection in the pool,
// enabling centralised query instrumentation without touching repositories.
func WithQueryTracer(tracer pgx.QueryTracer) Option {
	return func(cfg *pgxpool.Config) {
		cfg.ConnConfig.Tracer = tracer
	}
}

// WithMaxConns caps how many connections the pool may open. Left unset, pgx uses
// the greater of 4 and runtime.NumCPU(), which is a property of the machine the
// process happens to land on rather than of the database behind it -- see the
// sizing guidance in the README before overriding it.
func WithMaxConns(maxConns int32) Option {
	return func(cfg *pgxpool.Config) {
		cfg.MaxConns = maxConns
	}
}

// WithMinConns keeps at least this many connections around, so a burst after an
// idle period does not pay the handshake on every request. Left unset, pgx keeps
// none (0). It must not exceed MaxConns; NewPostgresPool rejects that.
func WithMinConns(minConns int32) Option {
	return func(cfg *pgxpool.Config) {
		cfg.MinConns = minConns
	}
}

// WithMaxConnLifetime retires a connection this long after it was opened,
// regardless of use, which is what lets the pool drift back to a healthy
// database after a failover or a restart. Left unset, pgx uses 1 hour.
func WithMaxConnLifetime(maxConnLifetime time.Duration) Option {
	return func(cfg *pgxpool.Config) {
		cfg.MaxConnLifetime = maxConnLifetime
	}
}

// WithMaxConnLifetimeJitter spreads the retirements above over a random window
// so a pool opened all at once does not expire all at once, which would leave
// the service reconnecting everything at the same moment. Left unset, pgx adds
// no jitter (0), which is why zero stays a valid value here.
func WithMaxConnLifetimeJitter(maxConnLifetimeJitter time.Duration) Option {
	return func(cfg *pgxpool.Config) {
		cfg.MaxConnLifetimeJitter = maxConnLifetimeJitter
	}
}

// WithMaxConnIdleTime closes connections that have gone unused for this long,
// down to MinConns. Left unset, pgx uses 30 minutes.
//
// Zero is not "no limit": the health check destroys every connection idle longer
// than this value, so zero means destroying the idle pool on every sweep. The
// server rejects a zero value rather than letting it through.
func WithMaxConnIdleTime(maxConnIdleTime time.Duration) Option {
	return func(cfg *pgxpool.Config) {
		cfg.MaxConnIdleTime = maxConnIdleTime
	}
}

// WithHealthCheckPeriod sets how often the pool sweeps idle connections for
// expiry and tops itself back up to MinConns. Left unset, pgx uses 1 minute.
func WithHealthCheckPeriod(healthCheckPeriod time.Duration) Option {
	return func(cfg *pgxpool.Config) {
		cfg.HealthCheckPeriod = healthCheckPeriod
	}
}

// NewPostgresPool runs migrations and builds a configured pgx pool. Optional
// Options (e.g. WithQueryTracer) tune the pool config before it is opened.
//
// The whole configuration is resolved and validated before the migrations run:
// a bad pool setting is a startup mistake, and finding out about it after the
// schema has already been touched helps nobody.
func NewPostgresPool(ctx context.Context, databaseURL string, opts ...Option) (*pgxpool.Pool, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return nil, ErrEmptyDatabaseURL
	}

	migrationURL, err := toMigrationURL(databaseURL)
	if err != nil {
		return nil, err
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse pgx pool config: %w", err)
	}

	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = make(map[string]string)
	}
	poolConfig.ConnConfig.RuntimeParams["timezone"] = "UTC"

	for _, opt := range opts {
		opt(poolConfig)
	}

	// ParseConfig has already filled in pgx's own defaults, so these are the
	// effective values -- which is what makes this catch the case where only the
	// minimum was configured and it collided with a default that depends on the
	// host's CPU count. pgx itself does not check the pair: it would leave the
	// health check failing to reach MinConns forever, in the background, quietly.
	if poolConfig.MinConns > poolConfig.MaxConns {
		return nil, fmt.Errorf("%w: min %d, max %d", ErrMinConnsAboveMaxConns, poolConfig.MinConns, poolConfig.MaxConns)
	}

	if err := runMigrations(migrationURL); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

// runMigrations applies every pending up migration. It takes the URL already
// normalised by toMigrationURL, since callers validate the scheme up front.
func runMigrations(migrationURL string) (retErr error) {
	srcDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("create migration source driver: %w", err)
	}

	migration, err := migrate.NewWithSourceInstance("iofs", srcDriver, migrationURL)
	if err != nil {
		return fmt.Errorf("create migration instance: %w", err)
	}

	defer func() {
		sourceErr, databaseErr := migration.Close()
		if sourceErr != nil || databaseErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close migration resources: %w", errors.Join(sourceErr, databaseErr)))
		}
	}()

	if err := migration.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply up migrations: %w", err)
	}

	return nil
}

func toMigrationURL(databaseURL string) (string, error) {
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return "", ErrEmptyDatabaseURL
	}

	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidDatabaseURL, err)
	}

	if parsedURL.Scheme == "" {
		return "", ErrEmptyDatabaseURLScheme
	}

	switch parsedURL.Scheme {
	case "postgres", "postgresql", "pgx5":
	default:
		return "", fmt.Errorf("%w: %s", ErrUnknownDatabaseScheme, parsedURL.Scheme)
	}

	if parsedURL.Scheme == "pgx5" {
		return parsedURL.String(), nil
	}

	parsedURL.Scheme = "pgx5"
	return parsedURL.String(), nil
}
