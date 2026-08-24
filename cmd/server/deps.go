package main

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/health"
	"appointment-manager/internal/healthinsurance"
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/mailer"
	"appointment-manager/internal/metrics"
	"appointment-manager/internal/notification"
	"appointment-manager/internal/password"
	"appointment-manager/internal/passwordreset"
	"appointment-manager/internal/patient"
	"appointment-manager/internal/prescription"
	"appointment-manager/internal/professional"
	"appointment-manager/internal/ratelimit"
	"appointment-manager/internal/session"
	"appointment-manager/internal/slot"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// dependencies holds the repositories, queries and services shared by the
// handlers and the worker. Every value is immutable once built and stateless
// over the pool, so one instance per type is enough: sharing them keeps the
// nil-pool check in a single place instead of in each handler constructor.
type dependencies struct {
	passwordHasher *password.Argon2
	readinessCheck health.CheckReady

	appointmentQuery   *appointment.Query
	appointmentService *appointment.Service

	assistantRepo *assistant.PostgresRepository

	healthInsuranceRepo *healthinsurance.Repository

	patientRepo *patient.Repository

	prescriptionRepo  *prescription.Repository
	prescriptionQuery *prescription.Query

	professionalRepo *professional.Repository

	passwordResetRepo *passwordreset.PostgresRepository

	sessionRepo *session.PostgresRepository

	slotRepo  *slot.Repository
	slotQuery *slot.Query
}

// newDependencies builds every shared collaborator from the pool. Each
// constructor here fails only when the pool is nil, so an error means the
// process cannot serve anything and must not start.
func newDependencies(pool *pgxpool.Pool, appMetrics *metrics.Metrics) (*dependencies, error) {
	appointmentRepo, err := appointment.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment postgres repository: %w", err)
	}
	appointmentQuery, err := appointment.NewQuery(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment query: %w", err)
	}
	appointmentService, err := appointment.NewService(appointmentRepo, appMetrics)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment service: %w", err)
	}

	assistantRepo, err := assistant.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create assistant postgres repository: %w", err)
	}

	healthInsuranceRepo, err := healthinsurance.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create health insurance repository: %w", err)
	}

	patientRepo, err := patient.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create patient repository: %w", err)
	}

	prescriptionRepo, err := prescription.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create prescription repository: %w", err)
	}
	prescriptionQuery, err := prescription.NewQuery(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create prescription query: %w", err)
	}

	professionalRepo, err := professional.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create professional repository: %w", err)
	}

	passwordResetRepo, err := passwordreset.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create password reset postgres repository: %w", err)
	}

	sessionRepo, err := session.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create session postgres repository: %w", err)
	}

	slotRepo, err := slot.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create slot repository: %w", err)
	}
	slotQuery, err := slot.NewQuery(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create slot query: %w", err)
	}

	readinessCheck, err := health.NewPgxReadinessCheck(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create health readiness checker: %w", err)
	}

	return &dependencies{
		passwordHasher:      password.NewArgon2(appMetrics),
		readinessCheck:      readinessCheck,
		appointmentQuery:    appointmentQuery,
		appointmentService:  appointmentService,
		assistantRepo:       assistantRepo,
		healthInsuranceRepo: healthInsuranceRepo,
		patientRepo:         patientRepo,
		prescriptionRepo:    prescriptionRepo,
		prescriptionQuery:   prescriptionQuery,
		professionalRepo:    professionalRepo,
		passwordResetRepo:   passwordResetRepo,
		sessionRepo:         sessionRepo,
		slotRepo:            slotRepo,
		slotQuery:           slotQuery,
	}, nil
}

// appComponents holds what run builds once the pool is up and hands to the
// handlers and the workers.
type appComponents struct {
	deps                *dependencies
	notificationService *notification.Service
	sessionStore        *session.Store
	resetTokenStore     *passwordreset.Store
	loginLimiter        *ratelimit.Limiter
	resetLimiter        *ratelimit.Limiter
	mailClient          *mailer.Client
	appBaseURL          string
	locale              i18n.Locale
	isDev               bool
}

// initializeAppComponents builds everything between the pool and the handlers.
// Errors are wrapped rather than logged: run logs them once.
func initializeAppComponents(
	ctx context.Context,
	logger *slog.Logger,
	pool *pgxpool.Pool,
	appMetrics *metrics.Metrics,
) (*appComponents, error) {
	deps, err := newDependencies(pool, appMetrics)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize dependencies: %w", err)
	}

	notificationService, err := initializeNotificationService(logger, deps, appMetrics)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize notification service: %w", err)
	}

	locale, err := parseDefaultLocale(os.Getenv(defaultLocaleEnv))
	if err != nil {
		return nil, fmt.Errorf("failed to parse default locale: %w", err)
	}

	sessionStore, err := session.NewStore(deps.sessionRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize session store: %w", err)
	}

	loginLimiterCfg, err := parseLoginRateLimit(os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("failed to parse login rate limit: %w", err)
	}

	loginLimiter, err := ratelimit.New(loginLimiterCfg, appMetrics)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize login rate limiter: %w", err)
	}

	appMetrics.RegisterLoginRateLimiter(
		func() float64 { return float64(loginLimiter.TrackedAccounts()) },
		func() float64 { return float64(loginLimiter.TrackedAddresses()) },
	)

	logger.Info("login rate limit configured",
		slog.Bool("enabled", loginLimiterCfg.Enabled),
		slog.Int("account_burst", loginLimiterCfg.AccountBurst),
		slog.Duration("account_refill", loginLimiterCfg.AccountRefill),
		slog.Int("ip_burst", loginLimiterCfg.IPBurst),
		slog.Duration("ip_refill", loginLimiterCfg.IPRefill),
		slog.Int("max_entries", loginLimiterCfg.MaxEntries))

	appBaseURL, err := parseAppBaseURL(os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("failed to parse app base url: %w", err)
	}

	resetTokenTTL, err := parsePasswordResetTokenTTL(os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("failed to parse password reset token ttl: %w", err)
	}

	resetTokenStore, err := passwordreset.NewStore(deps.passwordResetRepo, resetTokenTTL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize password reset token store: %w", err)
	}

	resetLimiterCfg, err := parsePasswordResetRateLimit(os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("failed to parse password reset rate limit: %w", err)
	}

	// A separate limiter, not the login one: sharing would let reset requests
	// spend the login allowance, and a successful login would refill the reset
	// budget. See ADR 0010.
	// The nil metrics are deliberate: ratelimit reports through the appt_login_*
	// counters, so instrumenting this one would fire the credential-stuffing alert.
	resetLimiter, err := ratelimit.New(resetLimiterCfg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize password reset rate limiter: %w", err)
	}

	mailClient, err := initializeMailer(ctx, logger)
	if err != nil {
		return nil, err
	}

	logger.Info("password reset configured",
		slog.String("base_url", appBaseURL),
		slog.Duration("token_ttl", resetTokenTTL),
		slog.Int("account_burst", resetLimiterCfg.AccountBurst),
		slog.Duration("account_refill", resetLimiterCfg.AccountRefill),
		slog.Int("ip_burst", resetLimiterCfg.IPBurst),
		slog.Duration("ip_refill", resetLimiterCfg.IPRefill))

	env := strings.TrimSpace(os.Getenv(environmentEnv))

	return &appComponents{
		deps:                deps,
		notificationService: notificationService,
		sessionStore:        sessionStore,
		resetTokenStore:     resetTokenStore,
		loginLimiter:        loginLimiter,
		resetLimiter:        resetLimiter,
		mailClient:          mailClient,
		appBaseURL:          appBaseURL,
		locale:              locale,
		isDev:               env == "" || strings.EqualFold(env, environmentDevelopment),
	}, nil
}
