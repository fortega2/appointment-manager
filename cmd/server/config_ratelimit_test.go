package main

import (
	"appointment-manager/internal/ratelimit"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envMap turns a map into the getenv func parseLoginRateLimit takes, so a case
// can set only the variables it cares about.
func envMap(values map[string]string) func(string) string {
	return func(name string) string {
		return values[name]
	}
}

func TestParseLoginRateLimitDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := parseLoginRateLimit(envMap(nil))

	require.NoError(t, err)
	assert.True(t, cfg.Enabled, "an unset variable must never read as disabled")
	assert.Equal(t, defaultLoginRateLimitAccountBurst, cfg.AccountBurst)
	assert.Equal(t, defaultLoginRateLimitAccountRefill, cfg.AccountRefill)
	assert.Equal(t, defaultLoginRateLimitIPBurst, cfg.IPBurst)
	assert.Equal(t, defaultLoginRateLimitIPRefill, cfg.IPRefill)
	assert.Equal(t, defaultLoginRateLimitMaxEntries, cfg.MaxEntries)
}

func TestParseLoginRateLimitReadsEveryVariable(t *testing.T) {
	t.Parallel()

	cfg, err := parseLoginRateLimit(envMap(map[string]string{
		loginRateLimitEnabledEnv:       "false",
		loginRateLimitAccountBurstEnv:  "9",
		loginRateLimitAccountRefillEnv: "90s",
		loginRateLimitIPBurstEnv:       "40",
		loginRateLimitIPRefillEnv:      "15s",
		loginRateLimitMaxEntriesEnv:    "500",
	}))

	require.NoError(t, err)
	assert.Equal(t, ratelimit.Config{
		Enabled:       false,
		AccountBurst:  9,
		AccountRefill: 90 * time.Second,
		IPBurst:       40,
		IPRefill:      15 * time.Second,
		MaxEntries:    500,
	}, cfg)
}

func TestParseLoginRateLimitRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		env   map[string]string
		named string
	}{
		{
			name:  "non boolean enabled",
			env:   map[string]string{loginRateLimitEnabledEnv: "maybe"},
			named: loginRateLimitEnabledEnv,
		},
		{
			name:  "non numeric account burst",
			env:   map[string]string{loginRateLimitAccountBurstEnv: notANumber},
			named: loginRateLimitAccountBurstEnv,
		},
		{
			name:  "zero account burst",
			env:   map[string]string{loginRateLimitAccountBurstEnv: "0"},
			named: loginRateLimitAccountBurstEnv,
		},
		{
			name:  "malformed account refill",
			env:   map[string]string{loginRateLimitAccountRefillEnv: notANumber},
			named: loginRateLimitAccountRefillEnv,
		},
		{
			name:  "zero account refill",
			env:   map[string]string{loginRateLimitAccountRefillEnv: zeroDuration},
			named: loginRateLimitAccountRefillEnv,
		},
		{
			name:  "negative ip burst",
			env:   map[string]string{loginRateLimitIPBurstEnv: negativeCount},
			named: loginRateLimitIPBurstEnv,
		},
		{
			name:  "zero ip refill",
			env:   map[string]string{loginRateLimitIPRefillEnv: zeroDuration},
			named: loginRateLimitIPRefillEnv,
		},
		{
			name:  "zero max entries",
			env:   map[string]string{loginRateLimitMaxEntriesEnv: "0"},
			named: loginRateLimitMaxEntriesEnv,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := parseLoginRateLimit(envMap(tt.env))

			require.Error(t, err, "a variable that is set but malformed must stop the process")
			assert.Contains(t, err.Error(), tt.named, "the error must name the variable at fault")
			assert.Equal(t, ratelimit.Config{}, cfg)
		})
	}
}

func TestParseLoginRateLimitDefaultsBuildAWorkingLimiter(t *testing.T) {
	t.Parallel()

	cfg, err := parseLoginRateLimit(envMap(nil))
	require.NoError(t, err)

	// The defaults must satisfy the limiter's own validation, or the process
	// would fail to start with nothing configured at all.
	limiter, err := ratelimit.New(cfg, nil)

	require.NoError(t, err)
	assert.NotNil(t, limiter)
}
