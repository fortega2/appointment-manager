package main

import (
	"appointment-manager/internal/metrics"
	"appointment-manager/internal/middleware"
	"appointment-manager/internal/session"
	"appointment-manager/internal/storage"
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// initializeServerHandlers builds every handler and wires it to a mux. Errors
// are returned wrapped rather than logged here: run logs them once, so the
// context of the failure is carried by the error chain itself.
func initializeServerHandlers(logger *slog.Logger, sessionStore *session.Store, deps *dependencies, storageClient *storage.Client, sendNotification func(context.Context, uuid.UUID), isDev bool, m *metrics.Metrics) (http.Handler, error) {
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
	slotHandler, err := initializeSlotHandler(logger, deps, sendNotification)
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

	// Protected routes are registered on mux itself through a guard rather than
	// on a nested mux, so the pattern the observability middlewares observe is
	// the specific route and not the catch-all (see middleware.Guard).
	apiProtected := middleware.Guard(mux, middleware.Session(sessionStore, isDev))
	assistantHandler.RegisterHandlers(apiProtected)
	appointmentHandler.RegisterHandlers(apiProtected)
	professionalHandler.RegisterHandlers(apiProtected)
	patientHandler.RegisterHandlers(apiProtected)

	prescriptionsEnabled := storageClient != nil
	uiProtected := middleware.Guard(
		mux,
		middleware.Prescriptions(prescriptionsEnabled),
		middleware.UISession(sessionStore, isDev),
	)
	uiHomeHandler.RegisterHandlers(uiProtected)
	professionalHandler.RegisterUIHandlers(uiProtected)
	patientHandler.RegisterUIHandlers(uiProtected)
	slotHandler.RegisterUIHandlers(uiProtected)
	uiAppointmentHandler.RegisterUIHandlers(uiProtected)

	if prescriptionsEnabled {
		uiPrescriptionHandler, err := initializeUIPrescriptionHandler(logger, deps, storageClient)
		if err != nil {
			return nil, err
		}
		uiPrescriptionHandler.RegisterUIHandlers(uiProtected)
	} else {
		logger.Warn("storage client disabled, prescription UI routes are not registered")
	}

	// Catch-alls for unmatched paths, keeping the pre-guard behaviour: an
	// unauthenticated request is rejected (API) or redirected to the login page
	// (UI) before it can learn whether the route exists, and an authenticated
	// one gets the mux's own 404 or 405.
	apiProtected.HandleFallback("/api/")
	uiProtected.HandleFallback("/")

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
		middleware.Metrics(m),
		otelHandler(),
	)
	return handler, nil
}

// otelHandler wraps the whole chain in an OpenTelemetry server span so every
// request is traced from the outermost layer, making the span the parent of the
// logger, metrics and downstream service spans. otelhttp calls the formatter
// once before routing (method only, r.URL.Path is not yet meaningful to name a
// span with) and again after next.ServeHTTP returns, renaming the span to
// "{method} {path}". The raw path is used rather than middleware.RouteTemplate
// so a span identifies the exact resource the request touched: a span name
// carries no cardinality cost the way a metric label does — each span already
// has a unique trace ID — and the raw path is already logged unredacted
// (internal/middleware/logger.go's "path" field), so this adds no new exposure.
func otelHandler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "http.server",
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				if r.Pattern == "" {
					return r.Method
				}
				return r.Method + " " + r.URL.Path
			}),
		)
	}
}
