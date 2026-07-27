package auth

import (
	"appointment-manager/internal/password"
	"appointment-manager/internal/web"
	"errors"
	"net/http"
)

const (
	detailIncorrectCredentials = "email or password is incorrect"
	detailVerifyFailure        = "failed to process the password"
)

// loginProblem maps a verifyCredentials error onto its RFC 9457 response.
// Unknown emails and wrong passwords intentionally share one 401 so the API
// never reveals which accounts exist.
func loginProblem(err error, path string) web.ProblemDetail {
	switch {
	case errors.Is(err, password.ErrTooManyConcurrentHashes):
		return web.NewProblem(http.StatusServiceUnavailable, web.ProblemTypeServiceUnavailable, serverBusyMsg, path)
	case errors.Is(err, errInvalidCredentials):
		return web.NewProblem(http.StatusUnauthorized, web.ProblemTypeUnauthorized, detailIncorrectCredentials, path)
	case errors.Is(err, errCredentialLookupFailed):
		return web.NewInternalServerProblem(failedGetAssistByEmailMsg, path)
	default:
		return web.NewInternalServerProblem(detailVerifyFailure, path)
	}
}
