package mailer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomail "github.com/wneessen/go-mail"
)

const senderDisplayName = "Turnos"

// TestNewClientRejectsBadInput covers every NewClient failure: the constructor
// does no I/O, so config validation is all there is to fail on.
func TestNewClientRejectsBadInput(t *testing.T) {
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

			client, err := NewClient(cfg)
			require.Error(t, err)
			assert.Nil(t, client)
			assert.ErrorIs(t, err, tt.expected)
		})
	}
}

// TestNewClientAcceptsAValidConfigWithoutDialing is the point of moving the dial
// out of the constructor: a client exists even when no relay is reachable.
func TestNewClientAcceptsAValidConfigWithoutDialing(t *testing.T) {
	t.Parallel()

	client, err := NewClient(validConfig())
	require.NoError(t, err)
	assert.NotNil(t, client)
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
