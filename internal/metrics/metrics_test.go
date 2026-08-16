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
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const openMetricsAccept = "application/openmetrics-text; version=1.0.0"

const (
	opSelect        = "select"
	opInsert        = "insert"
	boomText        = "boom"
	metricsEndpoint = "/metrics"

	kindSlotCancelled = "slot_cancelled"
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

func TestNewInitialisesFailureReasonsAtZero(t *testing.T) {
	t.Parallel()

	m := New()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, metricsEndpoint, nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	// Without a zero-valued series to step up from, the first burst creates the
	// child already carrying its count and increase() reports nothing happened.
	body := rec.Body.String()
	assert.Contains(t, body, `appt_password_queue_timeouts_total{reason="timeout"} 0`)
	assert.Contains(t, body, `appt_password_queue_timeouts_total{reason="client_cancelled"} 0`)
	assert.Contains(t, body, `appt_notifications_dropped_total{reason="queue_full"} 0`)
	assert.Contains(t, body, `appt_notifications_dropped_total{reason="unknown_kind"} 0`)
	assert.Contains(t, body, `appt_login_rate_limited_total{scope="account"} 0`)
	assert.Contains(t, body, `appt_login_rate_limited_total{scope="ip"} 0`)
}

func TestLoginRateLimitRecorders(t *testing.T) {
	t.Parallel()

	m := New()

	m.RecordLoginRateLimitedByAccount()
	m.RecordLoginRateLimitedByAccount()
	m.RecordLoginRateLimitedByIP()
	m.RecordLoginRateLimitEvicted()

	assert.InDelta(t, 2.0, testutil.ToFloat64(m.loginRateLimited.WithLabelValues(rateLimitScopeAccount)), 0.0001)
	assert.InDelta(t, 1.0, testutil.ToFloat64(m.loginRateLimited.WithLabelValues(rateLimitScopeIP)), 0.0001)
	assert.InDelta(t, 1.0, testutil.ToFloat64(m.loginRateLimiterEvictions), 0.0001)
}

func TestRegisterLoginRateLimiterExposesBothScopes(t *testing.T) {
	t.Parallel()

	m := New()

	m.RegisterLoginRateLimiter(func() float64 { return 3 }, func() float64 { return 7 })

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, metricsEndpoint, nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	// Both gauges share a name and differ only by a const label, so this also
	// pins that registering them side by side does not collide.
	body := rec.Body.String()
	assert.Contains(t, body, `appt_login_rate_limiter_entries{scope="account"} 3`)
	assert.Contains(t, body, `appt_login_rate_limiter_entries{scope="ip"} 7`)
}

func TestBusinessRecorders(t *testing.T) {
	t.Parallel()

	m := New()

	m.RecordAppointmentCreated()
	m.RecordAppointmentCreated()
	m.RecordAppointmentAttended()
	m.RecordAppointmentsCancelled(1)
	m.RecordAppointmentsCancelled(4)
	m.RecordAppointmentsCancelledByClinic(2)
	m.RecordAppointmentAbsent()
	m.RecordAppointmentsExpired(3)

	assert.InDelta(t, 2, testutil.ToFloat64(m.apptCreated), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.apptFinalized.WithLabelValues(outcomeAttended)), 0)
	assert.InDelta(t, 5, testutil.ToFloat64(m.apptFinalized.WithLabelValues(outcomeCancelled)), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.apptFinalized.WithLabelValues(outcomeAbsent)), 0)
	assert.InDelta(t, 3, testutil.ToFloat64(m.apptFinalized.WithLabelValues(outcomeExpired)), 0)

	// The two cancellation series must stay separable: a clinic cancellation
	// landing in the patient-initiated series would make either one unreadable.
	assert.InDelta(t, 2, testutil.ToFloat64(m.apptFinalized.WithLabelValues(outcomeCancelledByClinic)), 0)
}

func TestNotificationRecorders(t *testing.T) {
	t.Parallel()

	m := New()

	m.RecordNotificationDroppedQueueFull()
	m.RecordNotificationDroppedQueueFull()
	m.RecordNotificationDroppedUnknownKind()
	m.RecordNotificationSent(kindSlotCancelled)
	m.RecordNotificationNoRecipients(kindSlotCancelled)
	m.RecordNotificationLookupFailed(kindSlotCancelled)
	m.ObserveNotificationSend(context.Background(), kindSlotCancelled, 250*time.Millisecond)

	// The two drop reasons must stay separable: a queue-full drop is a sizing
	// signal, an unknown kind is a bug, and they call for opposite responses.
	assert.InDelta(t, 2, testutil.ToFloat64(m.notificationsDropped.WithLabelValues(dropReasonQueueFull)), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.notificationsDropped.WithLabelValues(dropReasonUnknownKind)), 0)

	assert.InDelta(t, 1, testutil.ToFloat64(m.notificationsProcessed.WithLabelValues(kindSlotCancelled, notificationOutcomeSent)), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.notificationsProcessed.WithLabelValues(kindSlotCancelled, notificationOutcomeNoRecipients)), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(m.notificationsProcessed.WithLabelValues(kindSlotCancelled, notificationOutcomeLookupFailed)), 0)

	assert.Equal(t, 1, testutil.CollectAndCount(m.notificationSendDuration))
}

func TestObserveNotificationSendAttachesTraceExemplar(t *testing.T) {
	t.Parallel()

	m := New()

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	m.ObserveNotificationSend(ctx, kindSlotCancelled, 250*time.Millisecond)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, metricsEndpoint, nil)
	req.Header.Set("Accept", openMetricsAccept)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `trace_id="`+span.SpanContext().TraceID().String()+`"`)
}

func TestRegisterNotificationQueueExposesLiveGauges(t *testing.T) {
	t.Parallel()

	m := New()

	// A real queue stands in for the channel: the point of a GaugeFunc is that
	// the value is read on collection, so a gauge written once at registration
	// would pass a single scrape and then drift.
	queue := make(chan struct{}, 4)
	m.RegisterNotificationQueue(
		func() float64 { return float64(len(queue)) },
		func() float64 { return float64(cap(queue)) },
	)

	scrape := func() string {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, metricsEndpoint, nil)
		rec := httptest.NewRecorder()
		m.Handler().ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)

		return rec.Body.String()
	}

	body := scrape()
	assert.Contains(t, body, "appt_notifications_queue_depth 0")
	assert.Contains(t, body, "appt_notifications_queue_capacity 4")

	queue <- struct{}{}
	queue <- struct{}{}

	assert.Contains(t, scrape(), "appt_notifications_queue_depth 2", "depth must be read at scrape time, not at registration")
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

func TestDBTracerRecordsSpanAttributes(t *testing.T) {
	t.Parallel()

	const (
		query         = "SELECT id FROM appointments WHERE patient_id = $1"
		sensitiveArg  = "6f1b2c3d-patient-uuid"
		attrSystem    = "db.system.name"
		attrOperation = "db.operation.name"
		attrQueryText = "db.query.text"
	)

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	m := New()
	tracer := m.DBTracer()
	tracer.tracer = tp.Tracer("test")

	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  query,
		Args: []any{sensitiveArg},
	})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	ended := recorder.Ended()
	require.Len(t, ended, 1)

	span := ended[0]
	assert.Equal(t, "db."+opSelect, span.Name())
	assert.Equal(t, trace.SpanKindClient, span.SpanKind())

	attrs := make(map[attribute.Key]string, len(span.Attributes()))
	for _, kv := range span.Attributes() {
		attrs[kv.Key] = kv.Value.String()
	}

	assert.Equal(t, "postgresql", attrs[attrSystem])
	assert.Equal(t, opSelect, attrs[attrOperation])
	assert.Equal(t, query, attrs[attrQueryText])

	// Query arguments must never reach the trace backend: the parameterised SQL
	// is safe to record, the bound values are not.
	for key, value := range attrs {
		assert.NotContains(t, value, sensitiveArg, "argument value leaked into attribute %q", key)
	}
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
