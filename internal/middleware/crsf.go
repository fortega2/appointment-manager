package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
)

func CSRF(logger *slog.Logger, isDev bool, serverAddr string) (func(http.Handler) http.Handler, error) {
	cp := http.NewCrossOriginProtection()
	cp.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.ErrorContext(r.Context(), "CSRF validation failed", slog.String("path", r.URL.Path))
		http.Error(w, "Forbidden", http.StatusForbidden)
	}))

	if isDev {
		if err := cp.AddTrustedOrigin("http://localhost" + serverAddr); err != nil {
			return nil, fmt.Errorf("failed to add trusted origin: %w", err)
		}
	}

	return cp.Handler, nil
}
