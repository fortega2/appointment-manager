// Package metrics owns the application's Prometheus instrumentation: a private
// registry, the Go runtime/process collectors, and the custom RED, dependency
// and business metrics. A single *Metrics is built at start-up and injected
// into the HTTP middleware, the pgx pool and the appointment service, mirroring
// the manual dependency-injection style used elsewhere in the codebase.
package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	namespace             = "appt"
	subsystemHTTP         = "http"
	subsystemDB           = "db"
	subsystemAppointments = "appointments"
)

const (
	outcomeAttended  = "attended"
	outcomeCancelled = "cancelled"
	outcomeAbsent    = "absent"
	outcomeExpired   = "expired"
)

// dbDurationBuckets are latency buckets tuned for database queries, which are
// typically faster than full HTTP requests, so the lower boundaries are finer.
//
//nolint:mnd // histogram bucket boundaries are metric configuration, not magic numbers.
var dbDurationBuckets = []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}

// Metrics holds the private registry and every collector the service exports.
type Metrics struct {
	reg *prometheus.Registry

	httpRequests  *prometheus.CounterVec
	httpDuration  *prometheus.HistogramVec
	httpInFlight  prometheus.Gauge
	dbDuration    *prometheus.HistogramVec
	dbErrors      *prometheus.CounterVec
	apptCreated   prometheus.Counter
	apptFinalized *prometheus.CounterVec
}

// New builds a Metrics backed by a private registry (never the global default)
// and registers the Go runtime, process and build-info collectors alongside the
// application's own metrics.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewBuildInfoCollector(),
	)

	factory := promauto.With(reg)

	// Dashboard: sum by (status_class) (rate(appt_http_requests_total[5m]))
	// Dashboard: topk(5, sum by (route) (rate(appt_http_requests_total[5m])))
	// Alert:     sum(rate(appt_http_requests_total{status_class="5xx"}[5m])) / sum(rate(appt_http_requests_total[5m])) > 0.05
	httpRequests := factory.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemHTTP,
			Name:      "requests_total",
			Help:      "Total number of HTTP requests by method, route template and status class.",
		},
		[]string{"method", "route", "status_class"},
	)

	// Dashboard: histogram_quantile(0.99, sum(rate(appt_http_request_duration_seconds_bucket[5m])) by (le, route))
	// Alert:     histogram_quantile(0.99, sum(rate(appt_http_request_duration_seconds_bucket[5m])) by (le)) > 2
	httpDuration := factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystemHTTP,
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds by method and route template.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)

	// Dashboard: appt_http_requests_in_flight
	// Alert:     appt_http_requests_in_flight > 200
	httpInFlight := factory.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystemHTTP,
			Name:      "requests_in_flight",
			Help:      "Number of HTTP requests currently being served.",
		},
	)

	// Dashboard: histogram_quantile(0.99, sum(rate(appt_db_query_duration_seconds_bucket[5m])) by (le, operation))
	// Alert:     histogram_quantile(0.99, sum(rate(appt_db_query_duration_seconds_bucket[5m])) by (le)) > 1
	dbDuration := factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystemDB,
			Name:      "query_duration_seconds",
			Help:      "Database query duration in seconds by SQL operation.",
			Buckets:   dbDurationBuckets,
		},
		[]string{"operation"},
	)

	// Dashboard: sum by (operation) (rate(appt_db_query_errors_total[5m]))
	// Alert:     rate(appt_db_query_errors_total[5m]) > 0.5
	dbErrors := factory.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemDB,
			Name:      "query_errors_total",
			Help:      "Total number of failed database queries by SQL operation (pgx.ErrNoRows excluded).",
		},
		[]string{"operation"},
	)

	// Dashboard: increase(appt_appointments_created_total[24h])
	// Alert:     rate(appt_appointments_created_total[1h]) == 0
	apptCreated := factory.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemAppointments,
			Name:      "created_total",
			Help:      "Total number of appointments booked.",
		},
	)

	// Dashboard: sum by (outcome) (increase(appt_appointments_finalized_total[24h]))
	// Alert:     rate(appt_appointments_finalized_total{outcome="expired"}[1h]) > 5
	apptFinalized := factory.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemAppointments,
			Name:      "finalized_total",
			Help:      "Total number of appointments that reached a terminal state by outcome.",
		},
		[]string{"outcome"},
	)

	return &Metrics{
		reg:           reg,
		httpRequests:  httpRequests,
		httpDuration:  httpDuration,
		httpInFlight:  httpInFlight,
		dbDuration:    dbDuration,
		dbErrors:      dbErrors,
		apptCreated:   apptCreated,
		apptFinalized: apptFinalized,
	}
}

// Handler returns the HTTP handler that exposes the registry in the Prometheus
// text and OpenMetrics formats. OpenMetrics is enabled so exemplars can be
// added later without changing the exposition.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

// ObserveRequest records the count and duration of a completed HTTP request.
// The status class ("2xx".."5xx") labels the counter only; the duration
// histogram is kept status-free to bound cardinality and carries a trace_id
// exemplar when ctx holds a sampled span.
func (m *Metrics) ObserveRequest(ctx context.Context, method, route, statusClass string, duration time.Duration) {
	m.httpRequests.WithLabelValues(method, route, statusClass).Inc()
	observeWithExemplar(ctx, m.httpDuration.WithLabelValues(method, route), duration.Seconds())
}

// IncInFlight increments the in-flight HTTP requests gauge.
func (m *Metrics) IncInFlight() { m.httpInFlight.Inc() }

// DecInFlight decrements the in-flight HTTP requests gauge.
func (m *Metrics) DecInFlight() { m.httpInFlight.Dec() }

// RecordAppointmentCreated counts one successfully booked appointment.
func (m *Metrics) RecordAppointmentCreated() { m.apptCreated.Inc() }

// RecordAppointmentAttended counts one appointment that transitioned to attended.
func (m *Metrics) RecordAppointmentAttended() { m.apptFinalized.WithLabelValues(outcomeAttended).Inc() }

// RecordAppointmentCancelled counts one appointment cancelled outside the 24h window.
func (m *Metrics) RecordAppointmentCancelled() {
	m.apptFinalized.WithLabelValues(outcomeCancelled).Inc()
}

// RecordAppointmentAbsent counts one appointment marked absent inside the 24h window.
func (m *Metrics) RecordAppointmentAbsent() { m.apptFinalized.WithLabelValues(outcomeAbsent).Inc() }

// RecordAppointmentsExpired counts n appointments swept to absent by the overdue worker.
func (m *Metrics) RecordAppointmentsExpired(n int64) {
	m.apptFinalized.WithLabelValues(outcomeExpired).Add(float64(n))
}

// DBTracer returns a pgx query tracer that records this Metrics' database
// duration and error series for every query executed on the pool.
func (m *Metrics) DBTracer() *DBTracer {
	return &DBTracer{duration: m.dbDuration, errorsTotal: m.dbErrors}
}

// RegisterDBPool registers a collector that reports live pgx pool saturation
// gauges (acquired/idle/total/max connections) read from pool.Stat() on scrape.
func (m *Metrics) RegisterDBPool(pool *pgxpool.Pool) {
	m.reg.MustRegister(newDBPoolCollector(pool))
}

// dbPoolCollector reports pgx pool connection gauges live at scrape time.
type dbPoolCollector struct {
	pool *pgxpool.Pool
	desc *prometheus.Desc
}

func newDBPoolCollector(pool *pgxpool.Pool) *dbPoolCollector {
	return &dbPoolCollector{
		pool: pool,
		desc: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystemDB, "pool_connections"),
			"Number of pgx pool connections by state (acquired/idle/total/max).",
			[]string{"state"},
			nil,
		),
	}
}

// Describe sends the collector's single descriptor to the channel.
func (c *dbPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

// Collect reads the current pool statistics and emits one gauge per state.
func (c *dbPoolCollector) Collect(ch chan<- prometheus.Metric) {
	stat := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(stat.AcquiredConns()), "acquired")
	ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(stat.IdleConns()), "idle")
	ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(stat.TotalConns()), "total")
	ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(stat.MaxConns()), "max")
}
