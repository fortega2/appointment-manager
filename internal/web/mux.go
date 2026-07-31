package web

import "net/http"

// Mux is the subset of *http.ServeMux that route registration needs.
// RegisterHandlers methods accept it instead of the concrete type so routes can
// be mounted through a wrapper that applies middleware per route, keeping every
// pattern registered on a single mux (see middleware.Guard).
type Mux interface {
	Handle(pattern string, handler http.Handler)
}
