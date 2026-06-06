package main

import (
	"appointment-manager/internal/appointment"
	"appointment-manager/internal/assistant"
	"appointment-manager/internal/auth"
	"appointment-manager/internal/db"
	"appointment-manager/internal/health"
	"appointment-manager/internal/healthinsurance"
	"appointment-manager/internal/middleware"
	"appointment-manager/internal/password"
	"appointment-manager/internal/patient"
	"appointment-manager/internal/professional"
	"appointment-manager/internal/server"
	"appointment-manager/internal/session"
	"appointment-manager/internal/slot"
	"appointment-manager/internal/ui/home"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	databaseURLEnv          = "DATABASE_URL"
	environmentEnv          = "ENV"
	environmentDevelopment  = "development"
	serverAddr              = ":8080"
	serverReadHeaderTimeout = 5 * time.Second
	serverReadTimeout       = 10 * time.Second
	serverWriteTimeout      = 15 * time.Second
	serverIdleTimeout       = 60 * time.Second
	serverMaxHeaderBytes    = 1 << 20
	serverShutdownTimeout   = 3 * time.Second
	readinessTimeout        = 300 * time.Millisecond
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	logger.Info("starting server")

	databaseURL := strings.TrimSpace(os.Getenv(databaseURLEnv))
	if databaseURL == "" {
		logger.Error("database URL is not set")
		return fmt.Errorf("%s is required", databaseURLEnv)
	}

	pool, err := db.NewPostgresPool(context.Background(), databaseURL)
	if err != nil {
		logger.Error("failed to initialize postgres pool", slog.Any("error", err))
		return err
	}
	defer pool.Close()

	env := strings.TrimSpace(os.Getenv(environmentEnv))
	isDev := env == "" || strings.EqualFold(env, environmentDevelopment)

	sessionStore := session.NewStore()
	handler, err := initializeServerHandlers(logger, sessionStore, pool, isDev)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := server.Start(ctx, logger, handler, serverAddr, server.Config{
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
		MaxHeaderBytes:    serverMaxHeaderBytes,
		ShutdownTimeout:   serverShutdownTimeout,
	}); err != nil {
		logger.ErrorContext(ctx, "server error", slog.Any("error", err))
		return err
	}

	return nil
}

func initializeServerHandlers(logger *slog.Logger, sessionStore *session.Store, pool *pgxpool.Pool, isDev bool) (http.Handler, error) {
	authHandler, err := initializeAuthHandler(logger, sessionStore, pool, password.NewArgon2(), isDev)
	if err != nil {
		logger.Error("failed to create auth handler", slog.Any("error", err))
		return nil, err
	}
	assistantHandler, err := initializeAssistantHandler(logger, pool)
	if err != nil {
		logger.Error("failed to create assistant handler", slog.Any("error", err))
		return nil, err
	}
	appointmentHandler, err := initializeAppointmentHandler(logger, pool)
	if err != nil {
		logger.Error("failed to create appointment handler", slog.Any("error", err))
		return nil, err
	}
	professionalHandler, err := initializeProfessionalHandler(logger, pool)
	if err != nil {
		logger.Error("failed to create professional handler", slog.Any("error", err))
		return nil, err
	}
	patientHandler, err := initializePatientHandler(logger, pool)
	if err != nil {
		logger.Error("failed to create patient handler", slog.Any("error", err))
		return nil, err
	}
	slotHandler, err := initializeSlotHandler(logger, pool)
	if err != nil {
		logger.Error("failed to create slot handler", slog.Any("error", err))
		return nil, err
	}
	healthHandler, err := initializeHealthHandler(logger, pool)
	if err != nil {
		logger.Error("failed to create health handler", slog.Any("error", err))
		return nil, err
	}
	uiHomeHandler, err := initializeUIHomeHandler(logger)
	if err != nil {
		logger.Error("failed to create UI home handler", slog.Any("error", err))
		return nil, err
	}
	uiAppointmentHandler, err := initializeUIAppointmentHandler(logger, pool)
	if err != nil {
		logger.Error("failed to create UI appointment handler", slog.Any("error", err))
		return nil, err
	}

	mux := http.NewServeMux()
	healthHandler.RegisterHandlers(mux)
	authHandler.RegisterHandlers(mux)

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("internal/ui/static"))))

	apiProtectedMux := http.NewServeMux()
	assistantHandler.RegisterHandlers(apiProtectedMux)
	appointmentHandler.RegisterHandlers(apiProtectedMux)
	professionalHandler.RegisterHandlers(apiProtectedMux)
	patientHandler.RegisterHandlers(apiProtectedMux)

	uiProtectedMux := http.NewServeMux()
	uiHomeHandler.RegisterHandlers(uiProtectedMux)
	professionalHandler.RegisterUIHandlers(uiProtectedMux)
	patientHandler.RegisterUIHandlers(uiProtectedMux)
	slotHandler.RegisterUIHandlers(uiProtectedMux)
	uiAppointmentHandler.RegisterUIHandlers(uiProtectedMux)

	mux.Handle("/api/v1/", middleware.Session(sessionStore, isDev)(apiProtectedMux))
	mux.Handle("/", middleware.UISession(sessionStore, isDev)(uiProtectedMux))

	csrfMiddleware, err := middleware.CSRF(logger, isDev, serverAddr)
	if err != nil {
		logger.Error("failed to initialize CSRF middleware", slog.Any("error", err))
		return nil, err
	}
	handler := middleware.Chain(
		mux,
		csrfMiddleware,
		middleware.Gzip(middleware.DefaultGzipConfig()),
		middleware.RequestID(),
		middleware.RequestLogger(logger),
	)
	return handler, nil
}

func initializeAuthHandler(logger *slog.Logger, store *session.Store, pool *pgxpool.Pool, pass *password.Argon2, isDev bool) (*auth.Handler, error) {
	authRepo, err := assistant.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth postgres repository: %w", err)
	}
	authHandler, err := auth.NewHandler(logger, store, authRepo, pass, isDev)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth handler: %w", err)
	}

	return authHandler, nil
}

func initializeAssistantHandler(logger *slog.Logger, pool *pgxpool.Pool) (*assistant.Handler, error) {
	assistantRepo, err := assistant.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create assistant postgres repository: %w", err)
	}
	passwordHasher := password.NewArgon2()
	assistantService, err := assistant.NewService(assistantRepo, passwordHasher)
	if err != nil {
		return nil, fmt.Errorf("failed to create assistant service: %w", err)
	}
	assistantHandler, err := assistant.NewHandler(logger, assistantService)
	if err != nil {
		return nil, fmt.Errorf("failed to create assistant handler: %w", err)
	}

	return assistantHandler, nil
}

func initializeAppointmentHandler(logger *slog.Logger, pool *pgxpool.Pool) (*appointment.Handler, error) {
	appointmentRepo, err := appointment.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment postgres repository: %w", err)
	}
	appointmentService, err := appointment.NewService(appointmentRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment service: %w", err)
	}
	appointmentHandler, err := appointment.NewHandler(logger, appointmentService)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment handler: %w", err)
	}

	return appointmentHandler, nil
}

func initializeProfessionalHandler(logger *slog.Logger, pool *pgxpool.Pool) (*professional.Handler, error) {
	professionalRepo, err := professional.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create professional repository: %w", err)
	}
	professionalHandler, err := professional.NewHandler(logger, professionalRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create professional handler: %w", err)
	}

	return professionalHandler, nil
}

func initializePatientHandler(logger *slog.Logger, pool *pgxpool.Pool) (*patient.Handler, error) {
	patientRepo, err := patient.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create patient repository: %w", err)
	}
	healthInsuranceRepo, err := healthinsurance.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create health insurance repository: %w", err)
	}
	patientHandler, err := patient.NewHandler(logger, patientRepo, healthInsuranceRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create patient handler: %w", err)
	}

	return patientHandler, nil
}

func initializeSlotHandler(logger *slog.Logger, pool *pgxpool.Pool) (*slot.Handler, error) {
	repo, err := slot.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create slot repository: %w", err)
	}
	query, err := slot.NewQuery(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create slot query: %w", err)
	}
	pRepo, err := professional.NewRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create professional repository for slot handler: %w", err)
	}

	h, err := slot.NewHandler(logger, repo, query, pRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create slot handler: %w", err)
	}

	return h, nil
}

func initializeHealthHandler(logger *slog.Logger, pool *pgxpool.Pool) (*health.Handler, error) {
	checkReady, err := health.NewPgxReadinessCheck(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create health readiness checker: %w", err)
	}
	handler, err := health.NewHandler(logger, checkReady, readinessTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create health handler: %w", err)
	}

	return handler, nil
}

func initializeUIHomeHandler(logger *slog.Logger) (*home.Handler, error) {
	homeHandler, err := home.NewHandler(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create home handler: %w", err)
	}

	return homeHandler, nil
}

func initializeUIAppointmentHandler(logger *slog.Logger, pool *pgxpool.Pool) (*appointment.UIHandler, error) {
	appointmentRepo, err := appointment.NewPostgresRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment postgres repository: %w", err)
	}
	appointmentQuery, err := appointment.NewQuery(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment query: %w", err)
	}
	appointmentService, err := appointment.NewService(appointmentRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment service: %w", err)
	}
	appointmentHandler, err := appointment.NewUIHandler(logger, appointmentService, appointmentQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to create appointment UI handler: %w", err)
	}

	return appointmentHandler, nil
}
