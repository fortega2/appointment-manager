package mailer

import (
	"errors"
	"net/mail"
	"strings"
)

// Message is one outbound mail. The sender comes from the Config instead, so no
// caller chooses who the mail claims to be from. HTMLBody is optional and goes
// out as an alternative to TextBody.
type Message struct {
	To       string
	Subject  string
	TextBody string
	HTMLBody string
}

func (m Message) validate() error {
	errs := make([]error, 0)

	// Parsing runs only once the recipient is present, so a missing address is
	// one error and not two.
	if strings.TrimSpace(m.To) == "" {
		errs = append(errs, ErrEmptyRecipient)
	} else if _, err := mail.ParseAddress(m.To); err != nil {
		errs = append(errs, ErrInvalidRecipient)
	}
	if strings.TrimSpace(m.Subject) == "" {
		errs = append(errs, ErrEmptySubject)
	}
	if strings.TrimSpace(m.TextBody) == "" {
		errs = append(errs, ErrEmptyBody)
	}

	// A line break ends a header field and starts another, which is how an
	// unsanitised value appends its own Bcc. Subject is what this guards; To is
	// covered anyway rather than inherited from mail.ParseAddress. See ADR 0010.
	if containsLineBreak(m.To) || containsLineBreak(m.Subject) {
		errs = append(errs, ErrHeaderLineBreak)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

func containsLineBreak(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}
