package metrics

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
)

const operationOther = "other"

// dbTraceState carries the query start time and derived operation label from
// TraceQueryStart to TraceQueryEnd, since the end callback does not receive the
// SQL text.
type dbTraceState struct {
	start     time.Time
	operation string
}

type dbTraceStateKey struct{}

// DBTracer implements pgx.QueryTracer to record duration and error metrics for
// every database query, centralising dependency instrumentation instead of
// wrapping each repository call.
type DBTracer struct {
	duration    *prometheus.HistogramVec
	errorsTotal *prometheus.CounterVec
}

// TraceQueryStart stores the start time and SQL operation in the returned
// context for TraceQueryEnd to consume.
func (t *DBTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, dbTraceStateKey{}, dbTraceState{
		start:     time.Now(),
		operation: sqlOperation(data.SQL),
	})
}

// TraceQueryEnd observes the query duration and increments the error counter for
// failures other than pgx.ErrNoRows (a no-row result is not an error here).
func (t *DBTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	state, ok := ctx.Value(dbTraceStateKey{}).(dbTraceState)
	if !ok {
		return
	}

	t.duration.WithLabelValues(state.operation).Observe(time.Since(state.start).Seconds())

	if data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows) {
		t.errorsTotal.WithLabelValues(state.operation).Inc()
	}
}

// sqlOperation derives a low-cardinality operation label from the leading
// keyword of a SQL statement, collapsing anything unrecognised to "other".
func sqlOperation(sql string) string {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return operationOther
	}

	switch strings.ToLower(fields[0]) {
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
