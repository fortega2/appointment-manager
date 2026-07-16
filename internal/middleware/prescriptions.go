package middleware

import (
	"appointment-manager/internal/ui/layout"
	"net/http"
)

// Prescriptions tells the layout whether the prescription UI routes are
// registered, so the nav can hide the link when object storage is disabled.
func Prescriptions(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := layout.WithPrescriptions(r.Context(), enabled)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
