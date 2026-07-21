package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"appointment-manager/internal/middleware"
)

const (
	patternAppointmentByID = "GET /appointments/{id}"
	routeAppointmentByID   = "/appointments/{id}"
)

type observation struct {
	method      string
	route       string
	statusClass string
	duration    time.Duration
}

type stubHTTPMetrics struct {
	inFlight     int
	maxInFlight  int
	observations []observation
}

func (s *stubHTTPMetrics) IncInFlight() {
	s.inFlight++
	if s.inFlight > s.maxInFlight {
		s.maxInFlight = s.inFlight
	}
}

func (s *stubHTTPMetrics) DecInFlight() { s.inFlight-- }

func (s *stubHTTPMetrics) ObserveRequest(method, route, statusClass string, duration time.Duration) {
	s.observations = append(s.observations, observation{
		method:      method,
		route:       route,
		statusClass: statusClass,
		duration:    duration,
	})
}

func newMux(pattern string, handler http.HandlerFunc) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(pattern, handler)

	return mux
}

func TestMetricsMatchedRoute(t *testing.T) {
	t.Parallel()

	rec := &stubHTTPMetrics{}
	mux := newMux(patternAppointmentByID, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := middleware.Metrics(rec)(mux)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/appointments/42", nil))

	require.Len(t, rec.observations, 1)
	assert.Equal(t, http.MethodGet, rec.observations[0].method)
	assert.Equal(t, routeAppointmentByID, rec.observations[0].route)
	assert.Equal(t, "2xx", rec.observations[0].statusClass)
	assert.Equal(t, 1, rec.maxInFlight)
	assert.Equal(t, 0, rec.inFlight)
}

func TestMetricsStatusClasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   string
	}{
		{name: "ok", status: http.StatusOK, want: "2xx"},
		{name: "redirect", status: http.StatusMovedPermanently, want: "3xx"},
		{name: "not found", status: http.StatusNotFound, want: "4xx"},
		{name: "server error", status: http.StatusInternalServerError, want: "5xx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := &stubHTTPMetrics{}
			mux := newMux(patternAppointmentByID, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			})
			handler := middleware.Metrics(rec)(mux)

			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/appointments/42", nil))

			require.Len(t, rec.observations, 1)
			assert.Equal(t, tt.want, rec.observations[0].statusClass)
		})
	}
}

func TestMetricsUnmatchedRouteUsesConstantLabel(t *testing.T) {
	t.Parallel()

	rec := &stubHTTPMetrics{}
	mux := newMux(patternAppointmentByID, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := middleware.Metrics(rec)(mux)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/does-not-exist", nil))

	require.Len(t, rec.observations, 1)
	assert.Equal(t, "unmatched", rec.observations[0].route)
	assert.Equal(t, "4xx", rec.observations[0].statusClass)
}

func TestMetricsNilRecorderIsPassThrough(t *testing.T) {
	t.Parallel()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	})
	handler := middleware.Metrics(nil)(next)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	assert.True(t, called)
	assert.Equal(t, http.StatusTeapot, recorder.Code)
}

func TestMetricsNilNextServesNotFound(t *testing.T) {
	t.Parallel()

	rec := &stubHTTPMetrics{}
	handler := middleware.Metrics(rec)(nil)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	require.Len(t, rec.observations, 1)
	assert.Equal(t, "unmatched", rec.observations[0].route)
}
