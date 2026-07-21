package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const openMetricsAccept = "application/openmetrics-text; version=1.0.0"

const (
	opSelect        = "select"
	opInsert        = "insert"
	boomText        = "boom"
	metricsEndpoint = "/metrics"
)

func TestNewRegistersRuntimeCollectors(t *testing.T) {
	t.Parallel()

	m := New()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, metricsEndpoint, nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "go_goroutines")
	assert.Contains(t, body, "process_start_time_seconds")
	assert.Contains(t, body, "go_build_info")
}

func TestBusinessRecorders(t *testing.T) {
	t.Parallel()

	m := New()

	m.RecordAppointmentCreated()
	m.RecordAppointmentCreated()
	m.RecordAppointmentAttended()
	m.RecordAppointmentCancelled()
	m.RecordAppointmentAbsent()
	m.RecordAppointmentsExpired(3)

	assert.InDelta(t, 2, testutil.ToFloat64(m.apptCreated), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.apptFinalized.WithLabelValues(outcomeAttended)), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.apptFinalized.WithLabelValues(outcomeCancelled)), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.apptFinalized.WithLabelValues(outcomeAbsent)), 0)
	assert.InDelta(t, 3, testutil.ToFloat64(m.apptFinalized.WithLabelValues(outcomeExpired)), 0)
}

func TestObserveRequestAndInFlight(t *testing.T) {
	t.Parallel()

	m := New()

	m.IncInFlight()
	assert.InDelta(t, 1, testutil.ToFloat64(m.httpInFlight), 0)
	m.DecInFlight()
	assert.InDelta(t, 0, testutil.ToFloat64(m.httpInFlight), 0)

	m.ObserveRequest(context.Background(), http.MethodGet, "/appointments/{id}", "2xx", 100*time.Millisecond)

	assert.InDelta(t, 1, testutil.ToFloat64(m.httpRequests.WithLabelValues(http.MethodGet, "/appointments/{id}", "2xx")), 0)
	assert.Equal(t, 1, testutil.CollectAndCount(m.httpDuration))
}

func TestObserveRequestAttachesTraceExemplar(t *testing.T) {
	t.Parallel()

	m := New()

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	m.ObserveRequest(ctx, http.MethodGet, "/appointments/{id}", "2xx", 100*time.Millisecond)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, metricsEndpoint, nil)
	req.Header.Set("Accept", openMetricsAccept)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `trace_id="`+span.SpanContext().TraceID().String()+`"`)
}

func TestDBTracerRecordsDurationAndErrors(t *testing.T) {
	t.Parallel()

	m := New()
	tracer := m.DBTracer()

	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	assert.Equal(t, 1, testutil.CollectAndCount(m.dbDuration))
	assert.InDelta(t, 0, testutil.ToFloat64(m.dbErrors.WithLabelValues(opSelect)), 0)

	ctx = tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "INSERT INTO appointments VALUES ($1)"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New(boomText)})
	assert.InDelta(t, 1, testutil.ToFloat64(m.dbErrors.WithLabelValues(opInsert)), 0)

	ctx = tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 2"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: pgx.ErrNoRows})
	assert.InDelta(t, 0, testutil.ToFloat64(m.dbErrors.WithLabelValues(opSelect)), 0)
}

func TestDBTracerEndWithoutStartIsNoop(t *testing.T) {
	t.Parallel()

	m := New()
	tracer := m.DBTracer()

	assert.NotPanics(t, func() {
		tracer.TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{})
	})
	assert.Equal(t, 0, testutil.CollectAndCount(m.dbDuration))
}

func TestSQLOperation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want string
	}{
		{name: "select", sql: "SELECT * FROM appointments", want: opSelect},
		{name: "lowercase select", sql: "select 1", want: opSelect},
		{name: "cte resolves to select", sql: "WITH x AS (SELECT 1) SELECT * FROM x", want: opSelect},
		{name: "insert", sql: "INSERT INTO appointments VALUES ($1)", want: opInsert},
		{name: "update", sql: "UPDATE appointments SET status = $1", want: "update"},
		{name: "delete", sql: "DELETE FROM appointments", want: "delete"},
		{name: "begin", sql: "begin", want: "begin"},
		{name: "commit", sql: "commit", want: "commit"},
		{name: "rollback", sql: "rollback", want: "rollback"},
		{name: "leading whitespace", sql: "   \n select 1", want: opSelect},
		{name: "empty", sql: "", want: operationOther},
		{name: "unknown keyword", sql: "VACUUM ANALYZE", want: operationOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, sqlOperation(tt.sql))
		})
	}
}

func TestRegisterDBPoolExposesGauges(t *testing.T) {
	t.Parallel()

	pool, err := pgxpool.New(context.Background(), "postgres://localhost:5432/appointment_manager_test")
	require.NoError(t, err)
	defer pool.Close()

	collector := newDBPoolCollector(pool)
	assert.Equal(t, 4, testutil.CollectAndCount(collector, "appt_db_pool_connections"))

	m := New()
	assert.NotPanics(t, func() { m.RegisterDBPool(pool) })

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, metricsEndpoint, nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	assert.Contains(t, rec.Body.String(), "appt_db_pool_connections")
}
