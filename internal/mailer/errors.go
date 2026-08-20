package mailer

import "errors"

var (
	ErrNilContext = errors.New("nil context")

	ErrEmptyHost           = errors.New("empty smtp host")
	ErrPortOutOfRange      = errors.New("smtp port out of range")
	ErrEmptyFromAddress    = errors.New("empty smtp from address")
	ErrInvalidFromAddress  = errors.New("invalid smtp from address")
	ErrPasswordWithoutUser = errors.New("smtp password set without a username")

	ErrEmptyRecipient   = errors.New("empty message recipient")
	ErrInvalidRecipient = errors.New("invalid message recipient")
	ErrEmptySubject     = errors.New("empty message subject")
	ErrEmptyBody        = errors.New("message has no body")
	ErrHeaderLineBreak  = errors.New("message header contains a line break")
)
