package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"appointment-manager/internal/middleware"
)

const (
	patternSlotEdit = "GET /slots/{id}/edit"
	routeSlotEdit   = "/slots/{id}/edit"
	slotID          = "019fb386-b744-7430-8632-5f8f63b0f44d"
	pathSlotEdit    = "/slots/" + slotID + "/edit"
)

type injectedKey struct{}

// injectContext mimics the session middlewares: it hands the next handler a
// copy of the request carrying a new context, which is what used to hide the
// matched pattern from the outer middlewares when a nested mux was mounted.
func injectContext(value string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), injectedKey{}, value)))
		})
	}
}

func TestGuardReportsSpecificRouteToOuterMiddleware(t *testing.T) {
	t.Parallel()

	var (
		gotPathValue string
		gotInjected  any
	)
	mux := http.NewServeMux()
	middleware.Guard(mux, injectContext("session")).Handle(
		patternSlotEdit,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPathValue = r.PathValue("id")
			gotInjected = r.Context().Value(injectedKey{})
			w.WriteHeader(http.StatusOK)
		}),
	)

	rec := &stubHTTPMetrics{}
	handler := middleware.Metrics(rec)(mux)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathSlotEdit, nil))

	require.Len(t, rec.observations, 1)
	assert.Equal(t, routeSlotEdit, rec.observations[0].route)
	assert.Equal(t, "2xx", rec.observations[0].statusClass)
	assert.Equal(t, slotID, gotPathValue)
	assert.Equal(t, "session", gotInjected)
}

// newGuardedMuxWithFallback registers patternSlotEdit and a "/" fallback on a
// fresh mux, reporting whether the guard middleware ran.
func newGuardedMuxWithFallback(guarded *bool) *http.ServeMux {
	mux := http.NewServeMux()
	guard := middleware.Guard(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*guarded = true
			next.ServeHTTP(w, r)
		})
	})
	guard.Handle(patternSlotEdit, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	guard.HandleFallback("/")

	return mux
}

func TestGuardFallbackReportsCatchAllRoute(t *testing.T) {
	t.Parallel()

	guarded := false
	mux := newGuardedMuxWithFallback(&guarded)

	rec := &stubHTTPMetrics{}
	handler := middleware.Metrics(rec)(mux)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/does-not-exist", nil))

	require.Len(t, rec.observations, 1)
	assert.Equal(t, "/", rec.observations[0].route)
	assert.Equal(t, "4xx", rec.observations[0].statusClass)
	assert.True(t, guarded, "the fallback must still run the guard middlewares")
}

func TestGuardFallbackKeepsMethodNotAllowed(t *testing.T) {
	t.Parallel()

	guarded := false
	mux := newGuardedMuxWithFallback(&guarded)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodDelete, pathSlotEdit, nil))

	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
	assert.Equal(t, "GET, HEAD", recorder.Header().Get("Allow"))
	assert.True(t, guarded, "the fallback must reject the request before negotiating the method")
}

func TestGuardFallbackKeepsNotFoundForUnknownPath(t *testing.T) {
	t.Parallel()

	guarded := false
	mux := newGuardedMuxWithFallback(&guarded)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/does-not-exist", nil))

	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.Empty(t, recorder.Header().Get("Allow"))
	assert.True(t, guarded)
}

func TestGuardRunsMiddlewaresOutermostLast(t *testing.T) {
	t.Parallel()

	var order []string
	record := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	mux := http.NewServeMux()
	middleware.Guard(mux, record("inner"), record("outer")).Handle(
		patternSlotEdit,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			order = append(order, "handler")
			w.WriteHeader(http.StatusOK)
		}),
	)

	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathSlotEdit, nil))

	assert.Equal(t, []string{"outer", "inner", "handler"}, order)
}

func TestGuardWithoutMiddlewaresRegistersHandler(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	middleware.Guard(mux).Handle(patternSlotEdit, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, pathSlotEdit, nil))

	assert.Equal(t, http.StatusTeapot, recorder.Code)
}
