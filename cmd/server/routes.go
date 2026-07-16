package main

import (
	"appointment-manager/internal/middleware"
	"appointment-manager/internal/password"
	"appointment-manager/internal/session"
	"appointment-manager/internal/storage"
	"appointment-manager/internal/ui/layout"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

func initializeServerHandlers(logger *slog.Logger, sessionStore *session.Store, pool *pgxpool.Pool, storageClient *storage.Client, isDev bool) (http.Handler, error) {
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

	layout.PrescriptionsEnabled = storageClient != nil
	if storageClient != nil {
		uiPrescriptionHandler, err := initializeUIPrescriptionHandler(logger, pool, storageClient)
		if err != nil {
			logger.Error("failed to create UI prescription handler", slog.Any("error", err))
			return nil, err
		}
		uiPrescriptionHandler.RegisterUIHandlers(uiProtectedMux)
	} else {
		logger.Warn("storage client disabled, prescription UI routes are not registered")
	}

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
