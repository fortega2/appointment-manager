package mailer

import (
	"errors"
	"net/mail"
	"strings"
)

// DefaultPort is the SMTP submission port relays accept application mail on.
const DefaultPort = 587

const (
	minPort = 1
	maxPort = 65535
)

// Config holds the connection settings for an SMTP relay. Clearing UseTLS
// exposes the credentials below and is meant for a local catcher only; see
// ADR 0010.
type Config struct {
	Host        string
	Username    string
	Password    string
	FromAddress string
	FromName    string
	Port        int
	UseTLS      bool
}

func (c Config) validate() error {
	errs := make([]error, 0)

	if strings.TrimSpace(c.Host) == "" {
		errs = append(errs, ErrEmptyHost)
	}
	if c.Port < minPort || c.Port > maxPort {
		errs = append(errs, ErrPortOutOfRange)
	}

	// Parsing runs only once the address is present, so an unset variable is one
	// error and not two.
	if strings.TrimSpace(c.FromAddress) == "" {
		errs = append(errs, ErrEmptyFromAddress)
	} else if _, err := mail.ParseAddress(c.FromAddress); err != nil {
		errs = append(errs, ErrInvalidFromAddress)
	}
	// Half-configured credentials would otherwise authenticate as nobody and
	// fail at the relay with a far less obvious message.
	if c.Password != "" && strings.TrimSpace(c.Username) == "" {
		errs = append(errs, ErrPasswordWithoutUser)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// authenticates reports whether the relay expects credentials.
func (c Config) authenticates() bool {
	return strings.TrimSpace(c.Username) != ""
}
