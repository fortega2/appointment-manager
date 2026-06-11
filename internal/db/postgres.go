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
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxConnIdleTime   time.Duration = 10 * time.Minute
	healthCheckPeriod time.Duration = 30 * time.Second
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func NewPostgresPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}

	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return nil, ErrEmptyDatabaseURL
	}

	if err := runMigrations(databaseURL); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse pgx pool config: %w", err)
	}

	poolConfig.MaxConns = 20
	poolConfig.MinConns = 2
	poolConfig.MaxConnLifetime = time.Hour
	poolConfig.MaxConnIdleTime = maxConnIdleTime
	poolConfig.HealthCheckPeriod = healthCheckPeriod

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

func runMigrations(databaseURL string) (retErr error) {
	migrationURL, err := toMigrationURL(databaseURL)
	if err != nil {
		return err
	}

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
