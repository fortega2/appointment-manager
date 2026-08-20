package mailer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomail "github.com/wneessen/go-mail"
)

const (
	configHost = "smtp.example.com"
	configFrom = "noreply@example.com"
	configUser = "relay-user"
	configPass = "relay-password"
)

func validConfig() Config {
	return Config{
		Host:        configHost,
		Port:        DefaultPort,
		FromAddress: configFrom,
		UseTLS:      true,
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mutate   func(*Config)
		expected error
		name     string
	}{
		{
			name:     "valid without credentials",
			mutate:   func(_ *Config) {},
			expected: nil,
		},
		{
			name:     "valid with credentials",
			mutate:   func(c *Config) { c.Username = configUser; c.Password = configPass },
			expected: nil,
		},
		{
			name:     "valid with a display name",
			mutate:   func(c *Config) { c.FromName = "Turnos" },
			expected: nil,
		},
		{
			name:     "empty host",
			mutate:   func(c *Config) { c.Host = "" },
			expected: ErrEmptyHost,
		},
		{
			name:     "blank host",
			mutate:   func(c *Config) { c.Host = "   " },
			expected: ErrEmptyHost,
		},
		{
			name:     "zero port",
			mutate:   func(c *Config) { c.Port = 0 },
			expected: ErrPortOutOfRange,
		},
		{
			name:     "negative port",
			mutate:   func(c *Config) { c.Port = -1 },
			expected: ErrPortOutOfRange,
		},
		{
			name:     "port above range",
			mutate:   func(c *Config) { c.Port = maxPort + 1 },
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

			err := cfg.validate()
			if tt.expected == nil {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assert.ErrorIs(t, err, tt.expected)
		})
	}
}

// TestConfigValidateReportsAMissingFromAddressOnce pins that an unset
// SMTP_FROM_ADDRESS is one line in the startup failure, not two.
func TestConfigValidateReportsAMissingFromAddressOnce(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.FromAddress = ""

	err := cfg.validate()
	require.ErrorIs(t, err, ErrEmptyFromAddress)
	assert.NotErrorIs(t, err, ErrInvalidFromAddress)
}

func TestConfigAuthenticates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		expected bool
	}{
		{name: "no username", username: "", expected: false},
		{name: "blank username", username: "   ", expected: false},
		{name: "username set", username: configUser, expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			cfg.Username = tt.username

			assert.Equal(t, tt.expected, cfg.authenticates())
		})
	}
}

// TestTLSPolicyNeverFallsBackToPlaintext guards the deliberate omission of
// TLSOpportunistic, which downgrades to plaintext in silence.
func TestTLSPolicyNeverFallsBackToPlaintext(t *testing.T) {
	t.Parallel()

	assert.Equal(t, gomail.TLSMandatory, tlsPolicy(true))
	assert.Equal(t, gomail.NoTLS, tlsPolicy(false))
	assert.NotEqual(t, gomail.TLSOpportunistic, tlsPolicy(false))
}
