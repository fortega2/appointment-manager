package main

import (
	"appointment-manager/internal/middleware"
	"appointment-manager/internal/session"
	"appointment-manager/internal/storage"
	"fmt"
	"log/slog"
	"net/http"
)

// initializeServerHandlers builds every handler and wires it to a mux. Errors
// are returned wrapped rather than logged here: run logs them once, so the
// context of the failure is carried by the error chain itself.
func initializeServerHandlers(logger *slog.Logger, sessionStore *session.Store, deps *dependencies, storageClient *storage.Client, isDev bool) (http.Handler, error) {
	authHandler, err := initializeAuthHandler(logger, sessionStore, deps, isDev)
	if err != nil {
		return nil, err
	}
	assistantHandler, err := initializeAssistantHandler(logger, deps)
	if err != nil {
		return nil, err
	}
	appointmentHandler, err := initializeAppointmentHandler(logger, deps)
	if err != nil {
		return nil, err
	}
	professionalHandler, err := initializeProfessionalHandler(logger, deps)
	if err != nil {
		return nil, err
	}
	patientHandler, err := initializePatientHandler(logger, deps)
	if err != nil {
		return nil, err
	}
	slotHandler, err := initializeSlotHandler(logger, deps)
	if err != nil {
		return nil, err
	}
	healthHandler, err := initializeHealthHandler(logger, deps)
	if err != nil {
		return nil, err
	}
	uiHomeHandler, err := initializeUIHomeHandler(logger)
	if err != nil {
		return nil, err
	}
	uiAppointmentHandler, err := initializeUIAppointmentHandler(logger, deps)
	if err != nil {
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

	prescriptionsEnabled := storageClient != nil
	if prescriptionsEnabled {
		uiPrescriptionHandler, err := initializeUIPrescriptionHandler(logger, deps, storageClient)
		if err != nil {
			return nil, err
		}
		uiPrescriptionHandler.RegisterUIHandlers(uiProtectedMux)
	} else {
		logger.Warn("storage client disabled, prescription UI routes are not registered")
	}

	mux.Handle("/api/v1/", middleware.Session(sessionStore, isDev)(apiProtectedMux))
	mux.Handle("/", middleware.Chain(
		uiProtectedMux,
		middleware.Prescriptions(prescriptionsEnabled),
		middleware.UISession(sessionStore, isDev),
	))

	csrfMiddleware, err := middleware.CSRF(logger, isDev, serverAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize CSRF middleware: %w", err)
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
