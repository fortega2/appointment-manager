package tracing

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

const (
	traceIDKey = "trace_id"
	spanIDKey  = "span_id"
)

// slogHandler decorates another slog.Handler, adding trace_id and span_id from
// the record's context when a valid span is active, so logs shipped to Loki can
// be pivoted to the matching trace in Tempo.
type slogHandler struct {
	inner slog.Handler
}

// NewSlogHandler wraps inner so every record emitted with a span-carrying
// context gains trace_id/span_id attributes. A nil inner defaults to a
// discarding handler, keeping the wrapper safe to install unconditionally.
func NewSlogHandler(inner slog.Handler) slog.Handler {
	if inner == nil {
		inner = slog.DiscardHandler
	}

	return &slogHandler{inner: inner}
}

// Enabled reports whether the inner handler handles records at the given level.
func (h *slogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle adds trace correlation attributes when the context carries a valid span
// and forwards the record to the inner handler.
func (h *slogHandler) Handle(ctx context.Context, record slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		record.AddAttrs(
			slog.String(traceIDKey, sc.TraceID().String()),
			slog.String(spanIDKey, sc.SpanID().String()),
		)
	}

	return h.inner.Handle(ctx, record)
}

// WithAttrs returns a new handler whose inner handler carries the given attrs.
func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &slogHandler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup returns a new handler whose inner handler opens the named group.
func (h *slogHandler) WithGroup(name string) slog.Handler {
	return &slogHandler{inner: h.inner.WithGroup(name)}
}
