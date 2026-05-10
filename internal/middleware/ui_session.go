package middleware

import (
	"appointment-manager/internal/session"
	"context"
	"net/http"
)

const looginURL string = "/login"

func UISession(store *session.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if store == nil {
				http.Redirect(w, r, looginURL, http.StatusSeeOther)
				return
			}

			cookie, err := r.Cookie(session.CookieName)
			if err != nil {
				http.Redirect(w, r, looginURL, http.StatusSeeOther)
				return
			}

			s, err := store.Get(cookie.Value)
			if err != nil {
				http.SetCookie(w, &http.Cookie{
					Name:   session.CookieName,
					Path:   "/",
					MaxAge: -1,
				})
				http.Redirect(w, r, looginURL, http.StatusSeeOther)
				return
			}

			ctx := context.WithValue(r.Context(), session.SessionKey, s)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
