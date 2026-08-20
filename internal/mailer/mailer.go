// Package mailer delivers outbound mail through an SMTP relay. It speaks plain
// SMTP rather than any provider's API, so the same code works against any relay
// or a local catcher with nothing but a credential change.
package mailer

import (
	"context"
	"fmt"
	"strings"
	"time"

	gomail "github.com/wneessen/go-mail"
)

const (
	dialTimeout = 10 * time.Second
	sendTimeout = 30 * time.Second
)

// Client is a thin wrapper around an SMTP client bound to one sender identity.
type Client struct {
	mail        *gomail.Client
	fromAddress string
	fromName    string
}

// NewClient validates the config, then opens and closes one connection so that
// a wrong host or bad credentials fail at startup rather than at the first
// send. See ADR 0010.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	options := []gomail.Option{
		gomail.WithPort(cfg.Port),
		gomail.WithTimeout(dialTimeout),
		gomail.WithTLSPortPolicy(tlsPolicy(cfg.UseTLS)),
	}
	if cfg.authenticates() {
		options = append(options,
			gomail.WithSMTPAuth(gomail.SMTPAuthAutoDiscover),
			gomail.WithUsername(cfg.Username),
			gomail.WithPassword(cfg.Password),
		)
	} else {
		options = append(options, gomail.WithSMTPAuth(gomail.SMTPAuthNoAuth))
	}

	mailClient, err := gomail.NewClient(cfg.Host, options...)
	if err != nil {
		// The config stays out of the message: it carries the relay password.
		return nil, fmt.Errorf("create smtp client: %w", err)
	}

	client := &Client{
		mail:        mailClient,
		fromAddress: cfg.FromAddress,
		fromName:    cfg.FromName,
	}

	if err := client.verifyConnection(ctx); err != nil {
		return nil, err
	}

	return client, nil
}

// Send delivers one message, dialing the relay and closing the connection
// again. It imposes its own deadline rather than trusting the caller's, because
// nothing else in this application cancels an inherited request context; see
// ADR 0010.
func (c *Client) Send(ctx context.Context, msg Message) error {
	if err := msg.validate(); err != nil {
		return err
	}

	message := gomail.NewMsg()
	if err := c.setSender(message); err != nil {
		return err
	}
	if err := message.To(msg.To); err != nil {
		// The address stays out of the message: errors reach the log backend,
		// which has a different audience than the database it came from.
		return fmt.Errorf("set recipient: %w", err)
	}

	message.Subject(msg.Subject)
	message.SetBodyString(gomail.TypeTextPlain, msg.TextBody)
	if strings.TrimSpace(msg.HTMLBody) != "" {
		message.AddAlternativeString(gomail.TypeTextHTML, msg.HTMLBody)
	}

	// Without these two headers most filters read the mail as spam.
	message.SetMessageID()
	message.SetDate()

	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	if err := c.mail.DialAndSendWithContext(sendCtx, message); err != nil {
		return fmt.Errorf("send mail: %w", err)
	}

	return nil
}

func (c *Client) setSender(message *gomail.Msg) error {
	if strings.TrimSpace(c.fromName) == "" {
		if err := message.From(c.fromAddress); err != nil {
			return fmt.Errorf("set from address: %w", err)
		}

		return nil
	}

	if err := message.FromFormat(c.fromName, c.fromAddress); err != nil {
		return fmt.Errorf("set from address: %w", err)
	}

	return nil
}

func (c *Client) verifyConnection(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	if err := c.mail.DialWithContext(dialCtx); err != nil {
		return fmt.Errorf("dial smtp relay: %w", err)
	}

	if err := c.mail.Close(); err != nil {
		return fmt.Errorf("close smtp probe: %w", err)
	}

	return nil
}

// tlsPolicy maps the config's boolean onto go-mail's policy. TLSOpportunistic
// is never returned: it falls back to plaintext silently. See ADR 0010.
func tlsPolicy(useTLS bool) gomail.TLSPolicy {
	if useTLS {
		return gomail.TLSMandatory
	}

	return gomail.NoTLS
}
