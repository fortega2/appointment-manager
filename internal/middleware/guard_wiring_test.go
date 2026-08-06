package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"appointment-manager/internal/middleware"
)

const (
	routeAPIFallback = "/api/"
	routeUIFallback  = "/"
)

// newAppLikeMux mirrors cmd/server/routes.go: three guards whose middlewares
// replace the request via r.WithContext, and a fallback for the two that have
// one.
func newAppLikeMux() *http.ServeMux {
	mux := http.NewServeMux()

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	publicUI := middleware.Guard(mux, injectContext("locale"))
	publicUI.Handle("GET /login", okHandler)

	apiProtected := middleware.Guard(mux, injectContext("api-session"))
	apiProtected.Handle("GET /api/v1/appointments", okHandler)
	apiProtected.Handle("POST /api/v1/appointments", okHandler)

	uiProtected := middleware.Guard(mux, injectContext("locale"), injectContext("ui-prescriptions"), injectContext("ui-session"))
	uiProtected.Handle("GET /{$}", okHandler)
	uiProtected.Handle(patternSlotEdit, okHandler)

	apiProtected.HandleFallback(routeAPIFallback)
	uiProtected.HandleFallback(routeUIFallback)

	return mux
}

func TestGuardWiringReportsRouteAndPreservesStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		method    string
		path      string
		wantRoute string
		wantCode  int
		wantAllow string
	}{
		{
			name:      "guarded ui route reports its own pattern",
			method:    http.MethodGet,
			path:      pathSlotEdit,
			wantRoute: routeSlotEdit,
			wantCode:  http.StatusOK,
		},
		{
			name:      "guarded api route reports its own pattern",
			method:    http.MethodPost,
			path:      "/api/v1/appointments",
			wantRoute: "/api/v1/appointments",
			wantCode:  http.StatusOK,
		},
		{
			name:      "public ui route reports its own pattern",
			method:    http.MethodGet,
			path:      "/login",
			wantRoute: "/login",
			wantCode:  http.StatusOK,
		},
		{
			// 404 rather than the 405 the other guards produce: only the guard that
			// owns the fallback knows the pattern, and this one belongs to another.
			name:      "wrong method on a public ui route falls through to 404",
			method:    http.MethodDelete,
			path:      "/login",
			wantRoute: routeUIFallback,
			wantCode:  http.StatusNotFound,
		},
		{
			name:      "wrong method on a ui route stays 405",
			method:    http.MethodDelete,
			path:      pathSlotEdit,
			wantRoute: routeUIFallback,
			wantCode:  http.StatusMethodNotAllowed,
			wantAllow: "GET, HEAD",
		},
		{
			name:      "wrong method on an api route stays 405",
			method:    http.MethodDelete,
			path:      "/api/v1/appointments",
			wantRoute: routeAPIFallback,
			wantCode:  http.StatusMethodNotAllowed,
			wantAllow: "GET, HEAD, POST",
		},
		{
			name:      "unknown ui path stays 404",
			method:    http.MethodGet,
			path:      "/does-not-exist",
			wantRoute: routeUIFallback,
			wantCode:  http.StatusNotFound,
		},
		{
			name:      "unknown api path stays 404",
			method:    http.MethodGet,
			path:      "/api/v1/does-not-exist",
			wantRoute: routeAPIFallback,
			wantCode:  http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := &stubHTTPMetrics{}
			handler := middleware.Metrics(rec)(newAppLikeMux())

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil))

			assert.Equal(t, tt.wantCode, recorder.Code)
			assert.Equal(t, tt.wantAllow, recorder.Header().Get("Allow"))
			require.Len(t, rec.observations, 1)
			assert.Equal(t, tt.wantRoute, rec.observations[0].route)
		})
	}
}
