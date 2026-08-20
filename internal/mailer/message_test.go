package mailer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	messageTo      = "assistant@example.com"
	messageSubject = "Reset your password"
	messageText    = "Open the link to choose a new password."
	messageHTML    = "<p>Open the link to choose a new password.</p>"
)

func validMessage() Message {
	return Message{
		To:       messageTo,
		Subject:  messageSubject,
		TextBody: messageText,
	}
}

func TestMessageValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mutate   func(*Message)
		expected error
		name     string
	}{
		{
			name:     "valid text only",
			mutate:   func(_ *Message) {},
			expected: nil,
		},
		{
			name:     "valid with html alternative",
			mutate:   func(m *Message) { m.HTMLBody = messageHTML },
			expected: nil,
		},
		{
			name:     "valid with a display name",
			mutate:   func(m *Message) { m.To = "Ana Gomez <" + messageTo + ">" },
			expected: nil,
		},
		{
			name:     "empty recipient",
			mutate:   func(m *Message) { m.To = "" },
			expected: ErrEmptyRecipient,
		},
		{
			name:     "blank recipient",
			mutate:   func(m *Message) { m.To = "   " },
			expected: ErrEmptyRecipient,
		},
		{
			name:     "malformed recipient",
			mutate:   func(m *Message) { m.To = "not-an-address" },
			expected: ErrInvalidRecipient,
		},
		{
			name:     "empty subject",
			mutate:   func(m *Message) { m.Subject = "" },
			expected: ErrEmptySubject,
		},
		{
			name:     "empty text body",
			mutate:   func(m *Message) { m.TextBody = "" },
			expected: ErrEmptyBody,
		},
		{
			name:     "html alone is not a body",
			mutate:   func(m *Message) { m.TextBody = ""; m.HTMLBody = messageHTML },
			expected: ErrEmptyBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := validMessage()
			tt.mutate(&msg)

			err := msg.validate()
			if tt.expected == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assert.ErrorIs(t, err, tt.expected)
		})
	}
}

// TestMessageValidateRejectsHeaderInjection covers the classic mailer hole: a
// line break lets a caller append headers of their own, Bcc included.
func TestMessageValidateRejectsHeaderInjection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mutate func(*Message)
		name   string
	}{
		{
			name:   "carriage return and newline in subject",
			mutate: func(m *Message) { m.Subject = "Reset\r\nBcc: attacker@evil.com" },
		},
		{
			name:   "bare newline in subject",
			mutate: func(m *Message) { m.Subject = "Reset\nBcc: attacker@evil.com" },
		},
		{
			name:   "bare carriage return in subject",
			mutate: func(m *Message) { m.Subject = "Reset\rBcc: attacker@evil.com" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := validMessage()
			tt.mutate(&msg)

			err := msg.validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrHeaderLineBreak)
		})
	}
}

// TestMessageValidateRejectsRecipientHeaderInjection asserts both sentinels so
// that the line-break guard is not later dropped as redundant with the parser.
func TestMessageValidateRejectsRecipientHeaderInjection(t *testing.T) {
	t.Parallel()

	msg := validMessage()
	msg.To = messageTo + "\r\nBcc: attacker@evil.com"

	err := msg.validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidRecipient)
	assert.ErrorIs(t, err, ErrHeaderLineBreak)
}

// TestMessageValidateReportsAMissingRecipientOnce pins that an absent address
// is not also reported as malformed.
func TestMessageValidateReportsAMissingRecipientOnce(t *testing.T) {
	t.Parallel()

	msg := validMessage()
	msg.To = ""

	err := msg.validate()
	require.ErrorIs(t, err, ErrEmptyRecipient)
	assert.NotErrorIs(t, err, ErrInvalidRecipient)
}
