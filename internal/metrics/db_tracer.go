package metrics

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"appointment-manager/internal/tracing"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	operationOther = "other"
	dbTracerName   = "appointment-manager/internal/db"
	dbSystem       = "postgresql"
)

// dbTraceState carries the query start time, derived operation label and the
// open span from TraceQueryStart to TraceQueryEnd, since the end callback does
// not receive the SQL text.
type dbTraceState struct {
	start     time.Time
	operation string
	span      trace.Span
}

type dbTraceStateKey struct{}

// DBTracer implements pgx.QueryTracer to record duration and error metrics for
// every database query, centralising dependency instrumentation instead of
// wrapping each repository call. The tracer is resolved once so the query hot
// path avoids a global-provider lookup per call.
type DBTracer struct {
	duration    *prometheus.HistogramVec
	errorsTotal *prometheus.CounterVec
	tracer      trace.Tracer
}

// TraceQueryStart opens a client span for the query and stores the start time,
// SQL operation and span in the returned context for TraceQueryEnd to consume.
// When no TracerProvider is configured the span is a no-op, so tracing stays
// zero-cost until enabled.
func (t *DBTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	operation := sqlOperation(data.SQL)

	ctx, span := t.tracer.Start(ctx, "db."+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", dbSystem),
			attribute.String("db.operation", operation),
		),
	)

	return context.WithValue(ctx, dbTraceStateKey{}, dbTraceState{
		start:     time.Now(),
		operation: operation,
		span:      span,
	})
}

// TraceQueryEnd observes the query duration (with a trace_id exemplar when
// sampled), increments the error counter for failures other than pgx.ErrNoRows
// (a no-row result is not an error here), and closes the query span.
func (t *DBTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	state, ok := ctx.Value(dbTraceStateKey{}).(dbTraceState)
	if !ok {
		return
	}

	observeWithExemplar(ctx, t.duration.WithLabelValues(state.operation), time.Since(state.start).Seconds())

	failed := data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows)
	if failed {
		t.errorsTotal.WithLabelValues(state.operation).Inc()
	}

	// A no-row result is not a query failure, so only a "failed" error is
	// recorded on the span; nil leaves the span status unset.
	var spanErr error
	if failed {
		spanErr = data.Err
	}
	tracing.EndSpan(state.span, spanErr)
}

// sqlOperation derives a low-cardinality operation label from the leading
// keyword of a SQL statement, collapsing anything unrecognised to "other". Only
// the first token is inspected, so it avoids tokenising the whole statement on
// every query.
func sqlOperation(sql string) string {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return operationOther
	}

	keyword := sql
	if end := strings.IndexFunc(sql, unicode.IsSpace); end != -1 {
		keyword = sql[:end]
	}

	switch strings.ToLower(keyword) {
	case "select", "with":
		return "select"
	case "insert":
		return "insert"
	case "update":
		return "update"
	case "delete":
		return "delete"
	case "begin":
		return "begin"
	case "commit":
		return "commit"
	case "rollback":
		return "rollback"
	default:
		return operationOther
	}
}
