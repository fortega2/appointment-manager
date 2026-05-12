package middleware

import (
	"appointment-manager/internal/session"
	"appointment-manager/internal/web"
	"context"
	"net/http"
)

func Session(store *session.Store, isDevelopment bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if store == nil {
				web.WriteProblem(w, web.NewInternalServerProblem("session store is not configured", r.URL.Path))
				return
			}

			cookie, err := r.Cookie(session.CookieName)
			if err != nil {
				web.WriteProblem(w, web.NewProblem(
					http.StatusUnauthorized,
					web.ProblemTypeUnauthorized,
					"authentication required",
					r.URL.Path,
				))
				return
			}

			s, err := store.Get(cookie.Value)
			if err != nil {
				http.SetCookie(w, &http.Cookie{
					Name:     session.CookieName,
					Path:     "/",
					MaxAge:   -1,
					Secure:   !isDevelopment,
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
				})
				web.WriteProblem(w, web.NewProblem(
					http.StatusUnauthorized,
					web.ProblemTypeUnauthorized,
					"session is invalid or expired",
					r.URL.Path,
				))
				return
			}

			ctx := context.WithValue(r.Context(), session.SessionKey, s)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
