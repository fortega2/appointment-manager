package main

import (
	"appointment-manager/internal/i18n"
	"appointment-manager/internal/metrics"
	"appointment-manager/internal/middleware"
	"appointment-manager/internal/storage"
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type handlerConfig struct {
	logger           *slog.Logger
	components       *appComponents
	storageClient    *storage.Client
	metrics          *metrics.Metrics
	sendNotification func(context.Context, uuid.UUID)
	locale           i18n.Locale
	isDev            bool
}

// initializeServerHandlers builds every handler and wires it to a mux. Errors
// are returned wrapped rather than logged here: run logs them once, so the
// context of the failure is carried by the error chain itself.
func initializeServerHandlers(cfg handlerConfig) (http.Handler, error) {
	authHandler, err := initializeAuthHandler(cfg.logger, cfg.components.sessionStore, cfg.components.deps, cfg.components.loginLimiter, cfg.isDev)
	if err != nil {
		return nil, err
	}
	assistantHandler, err := initializeAssistantHandler(cfg.logger, cfg.components.deps)
	if err != nil {
		return nil, err
	}
	appointmentHandler, err := initializeAppointmentHandler(cfg.logger, cfg.components.deps)
	if err != nil {
		return nil, err
	}
	professionalHandler, err := initializeProfessionalHandler(cfg.logger, cfg.components.deps)
	if err != nil {
		return nil, err
	}
	patientHandler, err := initializePatientHandler(cfg.logger, cfg.components.deps)
	if err != nil {
		return nil, err
	}
	slotHandler, err := initializeSlotHandler(cfg.logger, cfg.components.deps, cfg.sendNotification)
	if err != nil {
		return nil, err
	}
	healthHandler, err := initializeHealthHandler(cfg.logger, cfg.components.deps)
	if err != nil {
		return nil, err
	}
	uiHomeHandler, err := initializeUIHomeHandler(cfg.logger)
	if err != nil {
		return nil, err
	}
	uiAppointmentHandler, err := initializeUIAppointmentHandler(cfg.logger, cfg.components.deps)
	if err != nil {
		return nil, err
	}
	uiLanguageHandler, err := initializeUILanguageHandler(cfg.logger, cfg.isDev)
	if err != nil {
		return nil, err
	}
	resetHandler, err := initializeResetHandler(cfg.logger, cfg.components, cfg.metrics)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	healthHandler.RegisterHandlers(mux)

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("internal/ui/static"))))

	// The locale middleware goes in a guard and not in the Chain below: it
	// replaces the request, which from inside the Chain would hide r.Pattern from
	// the observability middlewares (see middleware.Guard). The login page needs
	// it too, hence a guard for the routes that have no session yet.
	localeMiddleware := middleware.Locale(cfg.locale)

	publicUI := middleware.Guard(mux, localeMiddleware)
	authHandler.RegisterHandlers(publicUI)
	resetHandler.RegisterHandlers(publicUI)
	uiLanguageHandler.RegisterHandlers(publicUI)

	// Protected routes are registered on mux itself through a guard rather than
	// on a nested mux, so the pattern the observability middlewares observe is
	// the specific route and not the catch-all (see middleware.Guard).
	apiProtected := middleware.Guard(mux, middleware.Session(cfg.components.sessionStore, cfg.isDev))
	assistantHandler.RegisterHandlers(apiProtected)
	appointmentHandler.RegisterHandlers(apiProtected)
	professionalHandler.RegisterHandlers(apiProtected)
	patientHandler.RegisterHandlers(apiProtected)

	prescriptionsEnabled := cfg.storageClient != nil
	uiProtected := middleware.Guard(
		mux,
		localeMiddleware,
		middleware.Prescriptions(prescriptionsEnabled),
		middleware.UISession(cfg.components.sessionStore, cfg.isDev),
	)
	uiHomeHandler.RegisterHandlers(uiProtected)
	professionalHandler.RegisterUIHandlers(uiProtected)
	patientHandler.RegisterUIHandlers(uiProtected)
	slotHandler.RegisterUIHandlers(uiProtected)
	uiAppointmentHandler.RegisterUIHandlers(uiProtected)

	if prescriptionsEnabled {
		uiPrescriptionHandler, err := initializeUIPrescriptionHandler(cfg.logger, cfg.components.deps, cfg.storageClient)
		if err != nil {
			return nil, err
		}
		uiPrescriptionHandler.RegisterUIHandlers(uiProtected)
	} else {
		cfg.logger.Warn("storage client disabled, prescription UI routes are not registered")
	}

	// Catch-alls for unmatched paths, keeping the pre-guard behaviour: an
	// unauthenticated request is rejected (API) or redirected to the login page
	// (UI) before it can learn whether the route exists, and an authenticated
	// one gets the mux's own 404 or 405.
	apiProtected.HandleFallback("/api/")
	uiProtected.HandleFallback("/")

	csrfMiddleware, err := middleware.CSRF(cfg.logger, cfg.isDev, serverAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize CSRF middleware: %w", err)
	}
	handler := middleware.Chain(
		mux,
		csrfMiddleware,
		middleware.Gzip(middleware.DefaultGzipConfig()),
		middleware.RequestID(),
		middleware.RequestLogger(cfg.logger),
		middleware.Metrics(cfg.metrics),
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
