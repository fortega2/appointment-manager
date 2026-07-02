package middleware

import (
	"appointment-manager/internal/session"
	"context"
	"net/http"
)

const loginURL string = "/login"

func UISession(store *session.Store, isDevelopment bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if store == nil {
				http.Redirect(w, r, loginURL, http.StatusSeeOther)
				return
			}

			cookie, err := r.Cookie(session.CookieName)
			if err != nil {
				http.Redirect(w, r, loginURL, http.StatusSeeOther)
				return
			}

			s, err := store.Get(cookie.Value)
			if err != nil {
				//nolint:gosec // G124 false positive: Secure is dynamically !isDevelopment (true in prod, false only for local HTTP dev); HttpOnly/SameSite are already set.
				http.SetCookie(w, &http.Cookie{
					Name:     session.CookieName,
					Path:     "/",
					MaxAge:   -1,
					Secure:   !isDevelopment,
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
				})
				http.Redirect(w, r, loginURL, http.StatusSeeOther)
				return
			}

			ctx := context.WithValue(r.Context(), session.SessionKey, s)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
