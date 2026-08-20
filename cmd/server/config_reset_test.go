package main

import (
	"appointment-manager/internal/mailer"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	validBaseURL = "https://turnos.example.com"
	relayHost    = "smtp.example.com"
	relayFrom    = "noreply@example.com"
)

func envFrom(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

// TestParseAppBaseURLIsRequired pins that the reset link's origin comes from
// config. Falling back to the request would let a forged Host header put an
// attacker's domain in a mail the user trusts.
func TestParseAppBaseURLIsRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{name: "unset", raw: ""},
		{name: "blank", raw: "   "},
		{name: "no scheme", raw: "turnos.example.com"},
		{name: "wrong scheme", raw: "ftp://turnos.example.com"},
		{name: "no host", raw: "https://"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			base, err := parseAppBaseURL(envFrom(map[string]string{appBaseURLEnv: tt.raw}))
			require.Error(t, err)
			assert.Empty(t, base)
		})
	}
}

func TestParseAppBaseURLTrimsTheTrailingSlash(t *testing.T) {
	t.Parallel()

	base, err := parseAppBaseURL(envFrom(map[string]string{appBaseURLEnv: validBaseURL + "/"}))
	require.NoError(t, err)
	assert.Equal(t, validBaseURL, base)
}

func TestParseSMTPConfig(t *testing.T) {
	t.Parallel()

	cfg, err := parseSMTPConfig(envFrom(map[string]string{
		smtpHostEnv:        relayHost,
		smtpFromAddressEnv: relayFrom,
	}))
	require.NoError(t, err)

	assert.Equal(t, relayHost, cfg.Host)
	assert.Equal(t, relayFrom, cfg.FromAddress)
	assert.Equal(t, mailer.DefaultPort, cfg.Port)
	assert.True(t, cfg.UseTLS, "TLS must stay on unless it is turned off explicitly")
}

func TestParseSMTPConfigRejectsBadValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		env  map[string]string
		name string
	}{
		{name: "non numeric port", env: map[string]string{smtpPortEnv: "not-a-port"}},
		{name: "zero port", env: map[string]string{smtpPortEnv: "0"}},
		{name: "bad tls flag", env: map[string]string{smtpUseTLSEnv: "maybe"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseSMTPConfig(envFrom(tt.env))
			require.Error(t, err)
		})
	}
}

func TestParsePasswordResetTokenTTL(t *testing.T) {
	t.Parallel()

	ttl, err := parsePasswordResetTokenTTL(envFrom(nil))
	require.NoError(t, err)
	assert.Equal(t, defaultPasswordResetTokenTTL, ttl)

	ttl, err = parsePasswordResetTokenTTL(envFrom(map[string]string{passwordResetTokenTTLEnv: "10m"}))
	require.NoError(t, err)
	assert.Equal(t, 10*time.Minute, ttl)

	_, err = parsePasswordResetTokenTTL(envFrom(map[string]string{passwordResetTokenTTLEnv: "0"}))
	require.Error(t, err)
}

// TestParsePasswordResetRateLimitIsStricterThanLogin pins the point of having a
// second limiter: a granted request puts a mail in somebody else's inbox.
func TestParsePasswordResetRateLimitIsStricterThanLogin(t *testing.T) {
	t.Parallel()

	reset, err := parsePasswordResetRateLimit(envFrom(nil))
	require.NoError(t, err)

	login, err := parseLoginRateLimit(envFrom(nil))
	require.NoError(t, err)

	assert.True(t, reset.Enabled, "the reset limiter has no off switch")
	assert.Less(t, reset.AccountBurst, login.AccountBurst)
	assert.Greater(t, reset.AccountRefill, login.AccountRefill)
	assert.Less(t, reset.IPBurst, login.IPBurst)
	assert.Greater(t, reset.IPRefill, login.IPRefill)
	assert.Equal(t, login.MaxEntries, reset.MaxEntries)
}

func TestParsePasswordResetRateLimitOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := parsePasswordResetRateLimit(envFrom(map[string]string{
		passwordResetRateLimitAccountBurstEnv:  "1",
		passwordResetRateLimitAccountRefillEnv: "1h",
		passwordResetRateLimitIPBurstEnv:       "2",
		passwordResetRateLimitIPRefillEnv:      "30m",
	}))
	require.NoError(t, err)

	assert.Equal(t, 1, cfg.AccountBurst)
	assert.Equal(t, time.Hour, cfg.AccountRefill)
	assert.Equal(t, 2, cfg.IPBurst)
	assert.Equal(t, 30*time.Minute, cfg.IPRefill)
}
