package middleware

import (
	"net/http"

	"appointment-manager/internal/web"
)

// GuardedMux registers routes on a root mux with a fixed middleware chain
// applied to each handler rather than around the mux.
type GuardedMux struct {
	mux         *http.ServeMux
	fallback    *http.ServeMux
	middlewares []func(http.Handler) http.Handler
}

var _ web.Mux = GuardedMux{}

// Guard returns a GuardedMux that registers every route on mux with
// middlewares applied to the handler itself. Middlewares are ordered as in
// Chain: the last one is the outermost, so it runs first.
//
// Mounting a nested mux instead (mux.Handle("/", Chain(nested, mw...))) hides
// the matched route from observability: middlewares that inject request values
// call r.WithContext, which allocates a copy of the *http.Request, so the
// nested mux records its specific pattern on that copy while the outer metrics
// and logger middlewares still hold the original request stamped with the
// catch-all pattern. Registering every pattern on one mux keeps r.Pattern
// accurate for RouteTemplate and the request logger.
func Guard(mux *http.ServeMux, middlewares ...func(http.Handler) http.Handler) GuardedMux {
	return GuardedMux{mux: mux, fallback: http.NewServeMux(), middlewares: middlewares}
}

// Handle registers pattern on the root mux with the guard chain applied. The
// pattern is mirrored onto the guard's fallback mux so HandleFallback can
// reproduce the method-negotiation decision for it.
func (g GuardedMux) Handle(pattern string, handler http.Handler) {
	g.mux.Handle(pattern, Chain(handler, g.middlewares...))
	g.fallback.Handle(pattern, http.NotFoundHandler())
}

// HandleFallback registers pattern as this guard's catch-all, behind the same
// middleware chain.
//
// The catch-all cannot simply be http.NotFoundHandler: a method-less pattern on
// the root mux always matches, so http.ServeMux stops before the step that
// answers 405 Method Not Allowed with an Allow header, and every wrong-method
// request to a registered path would collapse to 404. Asking the guard's
// fallback mux — the same patterns, no catch-all of its own — for the handler
// it would use reproduces the 404/405 decision the mux would have made on its
// own. The handler is resolved rather than served so the fallback mux does not
// overwrite the catch-all pattern the root mux recorded on the request: only
// ServeHTTP assigns r.Pattern, Handler leaves the request untouched. Requests
// reach this only after the guard chain has run, so it can never disclose a
// route to a caller the middlewares would have rejected.
func (g GuardedMux) HandleFallback(pattern string) {
	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler, _ := g.fallback.Handler(r)
		handler.ServeHTTP(w, r)
	})

	g.mux.Handle(pattern, Chain(fallback, g.middlewares...))
}
