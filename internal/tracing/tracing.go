// Package tracing wires OpenTelemetry tracing to an OTLP/HTTP collector (Tempo)
// and correlates logs and metrics with traces. Tracing is optional: when no
// OTLP endpoint is configured, Init installs nothing and every span becomes a
// no-op, so callers can wire it unconditionally.
package tracing

import (
	"context"
	"fmt"
	"net/url"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

// Config describes how the tracer reaches the collector and identifies the
// service in emitted spans.
type Config struct {
	// Endpoint is the OTLP/HTTP collector base URL (e.g. "http://tempo:4318").
	// When empty, tracing is disabled and Init is a no-op.
	Endpoint string
	// ServiceName and ServiceVersion populate the span resource so traces are
	// attributable to this service and release in Tempo.
	ServiceName    string
	ServiceVersion string
	// SampleRatio is the head-based sampling probability in [0,1] applied to
	// root spans; child spans follow their parent's decision.
	SampleRatio float64
}

// ShutdownFunc flushes buffered spans and releases exporter resources. It is
// safe to call once during shutdown and is a no-op when tracing is disabled.
type ShutdownFunc func(context.Context) error

// Init configures the global TracerProvider and propagators from cfg. When
// cfg.Endpoint is empty it returns a no-op shutdown and leaves the global
// provider untouched, so callers can wire it unconditionally.
func Init(ctx context.Context, cfg Config) (ShutdownFunc, error) {
	if cfg.Endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	if err := validateEndpoint(cfg.Endpoint); err != nil {
		return nil, err
	}

	exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(cfg.Endpoint))
	if err != nil {
		return nil, fmt.Errorf("creating OTLP trace exporter: %w", err)
	}

	res := resource.NewSchemaless(
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

// validateEndpoint rejects an OTLP endpoint with no URL scheme.
// otlptracehttp.WithEndpointURL silently misparses a scheme-less value (e.g.
// "tempo:4318" is parsed with "tempo" as the scheme and an empty host), so
// spans would be dropped with no error at startup or export time.
func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid OTLP endpoint %q: %w", endpoint, err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid OTLP endpoint %q: must include an http:// or https:// scheme", endpoint)
	}

	return nil
}
