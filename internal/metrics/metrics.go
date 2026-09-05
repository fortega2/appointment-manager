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
	"go.opentelemetry.io/otel"
)

const (
	namespace              = "appt"
	subsystemHTTP          = "http"
	subsystemDB            = "db"
	subsystemAppointments  = "appointments"
	subsystemNotifications = "notifications"
	subsystemPassword      = "password"
	subsystemLogin         = "login"
	subsystemPasswordReset = "password_reset"
)

const (
	outcomeAttended          = "attended"
	outcomeCancelled         = "cancelled"
	outcomeCancelledByClinic = "cancelled_by_clinic"
	outcomeAbsent            = "absent"
	outcomeExpired           = "expired"
	outcomeSent              = "sent"
	outcomeFailed            = "failed"
)

// Why a caller stopped waiting for a hashing slot. Only timeout means
// saturation; client_cancelled is the caller's own context ending first.
const (
	queueWaitFailureTimeout         = "timeout"
	queueWaitFailureClientCancelled = "client_cancelled"
)

// Which key ran out of login allowance. account rising while ip stays flat is
// the signature of distributed credential stuffing — many hosts converging on
// one account — which is the case an IP-keyed edge proxy cannot see at all.
const (
	rateLimitScopeAccount = "account"
	rateLimitScopeIP      = "ip"
)

// What became of a notification the drain picked up. no_recipients is a normal
// outcome, not a failure: cancelling a slot nobody booked notifies nobody.
const (
	notificationOutcomeSent         = "sent"
	notificationOutcomeNoRecipients = "no_recipients"
	notificationOutcomeLookupFailed = "lookup_failed"
)

// dbDurationBuckets are latency buckets tuned for database queries, which are
// typically faster than full HTTP requests, so the lower boundaries are finer.
//
//nolint:mnd // histogram bucket boundaries are metric configuration, not magic numbers.
var dbDurationBuckets = []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}

// notificationSendBuckets cover one notification's resolve-and-deliver pass.
// The boundary at 5 is deliberate: it is the notification package's per-send
// timeout, so a send that timed out lands on a bucket edge and is readable as
// its own step in the histogram instead of being smeared across a wider bucket.
//
//nolint:mnd // histogram bucket boundaries are metric configuration, not magic numbers.
var notificationSendBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// passwordQueueWaitBuckets cover the wait for an Argon2 hashing slot. An
// uncontended acquire is sub-millisecond, hence the fine low end; the boundary
// at 3 is the password package's maxQueueWait, so a caller that gave up lands
// on that boundary instead of being smeared across a wider bucket.
//
//nolint:mnd // histogram bucket boundaries are metric configuration, not magic numbers.
var passwordQueueWaitBuckets = []float64{0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 3}

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

	notificationsProcessed   *prometheus.CounterVec
	notificationSendDuration *prometheus.HistogramVec

	passwordQueueWait     prometheus.Histogram
	passwordQueueTimeouts *prometheus.CounterVec

	loginRateLimitedAccount   prometheus.Counter
	loginRateLimitedIP        prometheus.Counter
	loginRateLimiterEvictions prometheus.Counter

	passwordResetMailSent   prometheus.Counter
	passwordResetMailFailed prometheus.Counter
	passwordResetsCompleted prometheus.Counter
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

	// Dashboard: sum(increase(appt_appointments_created_total[$__range]))
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

	// Dashboard: sum by (outcome) (rate(appt_notifications_processed_total[5m]))
	// Alert:     rate(appt_notifications_processed_total{outcome="lookup_failed"}[5m]) > 0
	notificationsProcessed := factory.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemNotifications,
			Name:      "processed_total",
			Help:      "Total number of notifications taken off the queue by kind and outcome.",
		},
		[]string{"kind", "outcome"},
	)

	// Dashboard: histogram_quantile(0.95, sum(rate(appt_notifications_send_duration_seconds_bucket[5m])) by (le))
	// Alert:     histogram_quantile(0.95, sum(rate(appt_notifications_send_duration_seconds_bucket[5m])) by (le)) > 2
	notificationSendDuration := factory.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystemNotifications,
			Name:      "send_duration_seconds",
			Help:      "Time spent resolving and delivering one notification, by kind.",
			Buckets:   notificationSendBuckets,
		},
		[]string{"kind"},
	)

	// Dashboard: histogram_quantile(0.95, sum(rate(appt_password_queue_wait_seconds_bucket[5m])) by (le))
	// Alert:     histogram_quantile(0.95, sum(rate(appt_password_queue_wait_seconds_bucket[5m])) by (le)) > 1
	passwordQueueWait := factory.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: subsystemPassword,
			Name:      "queue_wait_seconds",
			Help:      "Time a caller waited for an Argon2 hashing slot, whether or not it got one.",
			Buckets:   passwordQueueWaitBuckets,
		},
	)

	// Dashboard: sum by (reason) (rate(appt_password_queue_timeouts_total[5m]))
	// Alert:     increase(appt_password_queue_timeouts_total{reason="timeout"}[5m]) > 0
	passwordQueueTimeouts := factory.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemPassword,
			Name:      "queue_timeouts_total",
			Help:      "Total number of callers that gave up waiting for an Argon2 hashing slot, by reason.",
		},
		[]string{"reason"},
	)

	// Dashboard: sum by (outcome) (rate(appt_password_reset_mail_total[15m]))
	// Alert:     increase(appt_password_reset_mail_total{outcome="failed"}[15m]) > 0
	//
	// A relay the app cannot reach is otherwise invisible: the requester is told
	// the same thing either way, by design, so this counter is the only signal
	// that the flow is broken.
	passwordResetMail := factory.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemPasswordReset,
			Name:      "mail_total",
			Help:      "Total number of password reset mails, by whether the relay accepted them.",
		},
		[]string{"outcome"},
	)

	// Dashboard: increase(appt_password_resets_completed_total[24h])
	// Alert:     increase(appt_password_resets_completed_total[1h]) > 3
	passwordResetsCompleted := factory.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemPasswordReset,
			Name:      "completed_total",
			Help:      "Total number of passwords actually changed through a reset link.",
		},
	)

	// Dashboard: sum by (scope) (rate(appt_login_rate_limited_total[5m]))
	// Alert:     increase(appt_login_rate_limited_total{scope="account"}[15m]) > 0
	loginRateLimited := factory.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemLogin,
			Name:      "rate_limited_total",
			Help:      "Total number of login attempts refused for exceeding their allowance, by key.",
		},
		[]string{"scope"},
	)

	// Dashboard: rate(appt_login_rate_limiter_evictions_total[5m])
	// Alert:     increase(appt_login_rate_limiter_evictions_total[15m]) > 0
	//
	// Evicting drops a key that may still owe time, so this staying at zero is
	// what keeps the limiter's memory honest. Anything above it means the entry
	// cap is being reached: either an attack, or a cap set too low.
	loginRateLimiterEvictions := factory.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: subsystemLogin,
			Name:      "rate_limiter_evictions_total",
			Help:      "Total number of rate-limiter entries dropped to stay within the entry cap.",
		},
	)

	// WithLabelValues creates a child lazily, so a reason that never fired has no
	// series at all — and one born carrying its first burst shows increase() = 0,
	// because Prometheus never observed the step up from zero. Registering every
	// known reason here is what makes the first occurrence visible.
	passwordQueueTimeouts.WithLabelValues(queueWaitFailureTimeout)
	passwordQueueTimeouts.WithLabelValues(queueWaitFailureClientCancelled)
	loginRateLimitedAccount := loginRateLimited.WithLabelValues(rateLimitScopeAccount)
	loginRateLimitedIP := loginRateLimited.WithLabelValues(rateLimitScopeIP)

	passwordResetMailSent := passwordResetMail.WithLabelValues(outcomeSent)
	passwordResetMailFailed := passwordResetMail.WithLabelValues(outcomeFailed)

	return &Metrics{
		reg:                      reg,
		httpRequests:             httpRequests,
		httpDuration:             httpDuration,
		httpInFlight:             httpInFlight,
		dbDuration:               dbDuration,
		dbErrors:                 dbErrors,
		apptCreated:              apptCreated,
		apptFinalized:            apptFinalized,
		notificationsProcessed:   notificationsProcessed,
		notificationSendDuration: notificationSendDuration,
		passwordQueueWait:        passwordQueueWait,
		passwordQueueTimeouts:    passwordQueueTimeouts,

		loginRateLimitedAccount:   loginRateLimitedAccount,
		loginRateLimitedIP:        loginRateLimitedIP,
		passwordResetMailSent:     passwordResetMailSent,
		passwordResetMailFailed:   passwordResetMailFailed,
		passwordResetsCompleted:   passwordResetsCompleted,
		loginRateLimiterEvictions: loginRateLimiterEvictions,
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

// RecordPasswordResetMailSent counts a reset mail the relay accepted. It is the
// only evidence one left: the requester is answered the same way regardless.
func (m *Metrics) RecordPasswordResetMailSent() { m.passwordResetMailSent.Inc() }

// RecordPasswordResetMailFailed counts a reset mail the relay refused.
func (m *Metrics) RecordPasswordResetMailFailed() { m.passwordResetMailFailed.Inc() }

// RecordPasswordResetCompleted counts a password actually changed by a link.
func (m *Metrics) RecordPasswordResetCompleted() { m.passwordResetsCompleted.Inc() }

// RecordAppointmentsCancelled counts n appointments cancelled at the patient's
// request. Clinic-initiated cancellations are counted separately by
// RecordAppointmentsCancelledByClinic, so the two are comparable on a dashboard
// rather than summed into one indistinguishable series.
func (m *Metrics) RecordAppointmentsCancelled(n int64) {
	m.apptFinalized.WithLabelValues(outcomeCancelled).Add(float64(n))
}

// RecordAppointmentsCancelledByClinic counts n appointments the clinic called
// off by cancelling their slot, whether through the cancel handler or the
// reconciliation sweep.
func (m *Metrics) RecordAppointmentsCancelledByClinic(n int64) {
	m.apptFinalized.WithLabelValues(outcomeCancelledByClinic).Add(float64(n))
}

// RecordAppointmentAbsent counts one appointment marked absent inside the 24h window.
func (m *Metrics) RecordAppointmentAbsent() { m.apptFinalized.WithLabelValues(outcomeAbsent).Inc() }

// RecordAppointmentsExpired counts n appointments swept to absent by the overdue worker.
func (m *Metrics) RecordAppointmentsExpired(n int64) {
	m.apptFinalized.WithLabelValues(outcomeExpired).Add(float64(n))
}

// RecordNotificationSent counts one notification delivered to at least one
// recipient.
func (m *Metrics) RecordNotificationSent(kind string) {
	m.notificationsProcessed.WithLabelValues(kind, notificationOutcomeSent).Inc()
}

// RecordNotificationNoRecipients counts one notification that resolved to
// nobody. It is kept separate from sent so a run of empty results cannot be
// mistaken for successful delivery on a dashboard.
func (m *Metrics) RecordNotificationNoRecipients(kind string) {
	m.notificationsProcessed.WithLabelValues(kind, notificationOutcomeNoRecipients).Inc()
}

// RecordNotificationLookupFailed counts one notification whose recipients
// could not be resolved.
func (m *Metrics) RecordNotificationLookupFailed(kind string) {
	m.notificationsProcessed.WithLabelValues(kind, notificationOutcomeLookupFailed).Inc()
}

// ObserveNotificationSend records how long one notification took to resolve and
// deliver, whatever its outcome: a send that failed or timed out is exactly the
// observation this histogram exists to capture. It carries a trace_id exemplar
// when ctx holds a sampled span.
func (m *Metrics) ObserveNotificationSend(ctx context.Context, kind string, duration time.Duration) {
	observeWithExemplar(ctx, m.notificationSendDuration.WithLabelValues(kind), duration.Seconds())
}

// RecordLoginRateLimitedByAccount counts one login attempt refused because the
// account had spent its allowance. This series rising while the ip one stays
// flat is distributed credential stuffing, the case an IP-keyed edge proxy is
// structurally unable to see.
func (m *Metrics) RecordLoginRateLimitedByAccount() {
	m.loginRateLimitedAccount.Inc()
}

// RecordLoginRateLimitedByIP counts one login attempt refused because the
// address had spent its allowance.
func (m *Metrics) RecordLoginRateLimitedByIP() {
	m.loginRateLimitedIP.Inc()
}

// RecordLoginRateLimitEvicted counts one rate-limiter entry dropped to stay
// within the entry cap. An eviction can forget a key that still owed time, so
// this is expected to sit at zero and to move only under abuse.
func (m *Metrics) RecordLoginRateLimitEvicted() {
	m.loginRateLimiterEvictions.Inc()
}

// RegisterLoginRateLimiter registers gauges reporting how many keys the login
// rate limiter is tracking. Both are read on scrape rather than written on
// change, so an idle limiter costs nothing and the gauges cannot drift from the
// maps they describe.
func (m *Metrics) RegisterLoginRateLimiter(accounts, addresses func() float64) {
	factory := promauto.With(m.reg)

	// Dashboard: appt_login_rate_limiter_entries
	// Alert:     appt_login_rate_limiter_entries > 8000
	scopes := []struct {
		scope string
		value func() float64
	}{
		{rateLimitScopeAccount, accounts},
		{rateLimitScopeIP, addresses},
	}

	for _, s := range scopes {
		factory.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace:   namespace,
				Subsystem:   subsystemLogin,
				Name:        "rate_limiter_entries",
				Help:        "Number of keys the login rate limiter is currently tracking.",
				ConstLabels: prometheus.Labels{"scope": s.scope},
			},
			s.value,
		)
	}
}

// ObservePasswordQueueWait records how long a caller waited for an Argon2
// hashing slot, whether or not it got one. It carries a trace_id exemplar when
// ctx holds a sampled span, which is what ties a slow login back to the burst
// that caused it.
func (m *Metrics) ObservePasswordQueueWait(ctx context.Context, waited time.Duration) {
	observeWithExemplar(ctx, m.passwordQueueWait, waited.Seconds())
}

// RecordPasswordQueueTimedOut counts one caller that waited the full budget
// without a slot freeing up. This is the series that means saturation.
func (m *Metrics) RecordPasswordQueueTimedOut() {
	m.passwordQueueTimeouts.WithLabelValues(queueWaitFailureTimeout).Inc()
}

// RecordPasswordQueueClientCancelled counts one caller whose own context ended
// while queued. Unlike a timeout it says nothing about load, since the client
// hung up on a request nobody was going to answer anyway.
func (m *Metrics) RecordPasswordQueueClientCancelled() {
	m.passwordQueueTimeouts.WithLabelValues(queueWaitFailureClientCancelled).Inc()
}

// DBTracer returns a pgx query tracer that records this Metrics' database
// duration and error series for every query executed on the pool. The OTel
// tracer is resolved here once and reused for every query.
func (m *Metrics) DBTracer() *DBTracer {
	return &DBTracer{
		duration:    m.dbDuration,
		errorsTotal: m.dbErrors,
		tracer:      otel.Tracer(dbTracerName),
	}
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
