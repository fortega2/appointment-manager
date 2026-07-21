package tracing_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"appointment-manager/internal/tracing"
)

const logMessage = "handled request"

// newSampledSpanContext returns a context carrying a recording, sampled span so
// the handler has valid trace and span IDs to copy onto log records.
func newSampledSpanContext(t *testing.T) context.Context {
	t.Helper()

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	t.Cleanup(func() { span.End() })

	return ctx
}

func TestSlogHandlerAddsTraceIDs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := tracing.NewSlogHandler(slog.NewJSONHandler(&buf, nil))

	ctx := newSampledSpanContext(t)
	slog.New(handler).InfoContext(ctx, logMessage)

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))

	assert.NotEmpty(t, record["trace_id"])
	assert.NotEmpty(t, record["span_id"])
	assert.Equal(t, logMessage, record["msg"])
}

func TestSlogHandlerWithoutSpanOmitsTraceIDs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := tracing.NewSlogHandler(slog.NewJSONHandler(&buf, nil))

	slog.New(handler).InfoContext(context.Background(), logMessage)

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))

	assert.NotContains(t, record, "trace_id")
	assert.NotContains(t, record, "span_id")
}

func TestSlogHandlerWithAttrsAndGroupPreserveTraceIDs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	handler := tracing.NewSlogHandler(slog.NewJSONHandler(&buf, nil)).
		WithAttrs([]slog.Attr{slog.String("component", "test")}).
		WithGroup("req")

	ctx := newSampledSpanContext(t)
	slog.New(handler).InfoContext(ctx, logMessage)

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))

	// WithAttrs preset lands before the group opens, so it stays top level; the
	// record-time trace attributes nest under the opened "req" group.
	assert.Equal(t, "test", record["component"])
	group, ok := record["req"].(map[string]any)
	require.True(t, ok, "expected a req group in the log record")
	assert.NotEmpty(t, group["trace_id"])
	assert.NotEmpty(t, group["span_id"])
}

func TestNewSlogHandlerNilInnerDoesNotPanic(t *testing.T) {
	t.Parallel()

	handler := tracing.NewSlogHandler(nil)

	// A nil inner falls back to the discarding handler, which reports disabled.
	assert.False(t, handler.Enabled(context.Background(), slog.LevelError))
	assert.NotPanics(t, func() {
		slog.New(handler).InfoContext(newSampledSpanContext(t), logMessage)
	})
}
