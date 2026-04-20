package session

import (
	"context"
)

type contextKey string

const SessionKey contextKey = "session"

func FromContext(ctx context.Context) (*Session, error) {
	session, ok := ctx.Value(SessionKey).(*Session)
	if !ok || session == nil {
		return nil, ErrSessionNotInContext
	}
	return session, nil
}
