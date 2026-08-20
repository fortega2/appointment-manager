package mailer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomail "github.com/wneessen/go-mail"
)

const senderDisplayName = "Turnos"

// TestNewClientRejectsBadInputBeforeDialing covers every NewClient path that
// returns without touching the network; a valid config needs the relay the
// integration test has.
func TestNewClientRejectsBadInputBeforeDialing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mutate   func(*Config)
		expected error
		name     string
	}{
		{
			name:     "empty host",
			mutate:   func(c *Config) { c.Host = "" },
			expected: ErrEmptyHost,
		},
		{
			name:     "port out of range",
			mutate:   func(c *Config) { c.Port = 0 },
			expected: ErrPortOutOfRange,
		},
		{
			name:     "empty from address",
			mutate:   func(c *Config) { c.FromAddress = "" },
			expected: ErrEmptyFromAddress,
		},
		{
			name:     "malformed from address",
			mutate:   func(c *Config) { c.FromAddress = "not-an-address" },
			expected: ErrInvalidFromAddress,
		},
		{
			name:     "password without username",
			mutate:   func(c *Config) { c.Password = configPass },
			expected: ErrPasswordWithoutUser,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			tt.mutate(&cfg)

			client, err := NewClient(t.Context(), cfg)
			require.Error(t, err)
			assert.Nil(t, client)
			assert.ErrorIs(t, err, tt.expected)
		})
	}
}

func TestNewClientNilContext(t *testing.T) {
	t.Parallel()

	//nolint:staticcheck // SA1012: passing a nil context is exactly what this guard is for.
	client, err := NewClient(nil, validConfig())
	require.Error(t, err)
	assert.Nil(t, client)
	assert.ErrorIs(t, err, ErrNilContext)
}

// TestSendRejectsBadMessageBeforeDialing pins that validation runs before the
// dial: this client has no reachable relay, so a dial would fail differently.
func TestSendRejectsBadMessageBeforeDialing(t *testing.T) {
	t.Parallel()

	client := &Client{fromAddress: configFrom}

	msg := validMessage()
	msg.To = ""

	err := client.Send(t.Context(), msg)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyRecipient)
}

func TestClientSetSender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fromName string
		expected string
	}{
		{
			name:     "address only",
			fromName: "",
			expected: configFrom,
		},
		{
			name:     "blank display name is treated as absent",
			fromName: "   ",
			expected: configFrom,
		},
		{
			name:     "display name and address",
			fromName: senderDisplayName,
			expected: senderDisplayName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &Client{fromAddress: configFrom, fromName: tt.fromName}
			message := gomail.NewMsg()

			require.NoError(t, client.setSender(message))

			from := message.GetFromString()
			require.Len(t, from, 1)
			assert.Contains(t, from[0], tt.expected)
			assert.Contains(t, from[0], configFrom)
		})
	}
}

func TestClientSetSenderInvalidAddress(t *testing.T) {
	t.Parallel()

	client := &Client{fromAddress: "not-an-address"}

	require.Error(t, client.setSender(gomail.NewMsg()))
}
