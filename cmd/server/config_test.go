package main

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	unsetValue    = ""
	notANumber    = "not-a-number"
	validInterval = "45s"
	validBuffer   = "250"
)

func TestParseSampleRatio(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    float64
		wantErr bool
	}{
		{name: "unset falls back to default", raw: "", want: defaultTraceSampleRatio},
		{name: "valid mid-range ratio", raw: "0.25", want: 0.25},
		{name: "valid boundary zero", raw: "0", want: 0},
		{name: "valid boundary one", raw: "1", want: 1},
		{name: "negative is rejected", raw: "-0.1", wantErr: true},
		{name: "above one is rejected", raw: "1.1", wantErr: true},
		{name: "malformed is rejected", raw: "not-a-number", wantErr: true},
		{name: "NaN is rejected", raw: "NaN", wantErr: true},
		{name: "positive infinity is rejected", raw: "+Inf", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseSampleRatio(tt.raw)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.InDelta(t, tt.want, got, 0)
		})
	}
}

func TestParseNotificationConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		rawInterval  string
		rawBuffer    string
		wantInterval time.Duration
		wantBuffer   int
		wantErr      bool
	}{
		{
			name:         "both unset fall back to defaults",
			rawInterval:  unsetValue,
			rawBuffer:    unsetValue,
			wantInterval: defaultNotificationTickerInterval,
			wantBuffer:   defaultNotificationBufferSize,
		},
		{
			name:         "both set are honoured",
			rawInterval:  validInterval,
			rawBuffer:    validBuffer,
			wantInterval: 45 * time.Second,
			wantBuffer:   250,
		},
		{
			// Each falls back on its own, so setting one does not force the other.
			name:         "only the interval is set",
			rawInterval:  validInterval,
			rawBuffer:    unsetValue,
			wantInterval: 45 * time.Second,
			wantBuffer:   defaultNotificationBufferSize,
		},
		{
			name:         "whitespace counts as unset",
			rawInterval:  "   ",
			rawBuffer:    "  ",
			wantInterval: defaultNotificationTickerInterval,
			wantBuffer:   defaultNotificationBufferSize,
		},
		{name: "malformed interval is rejected", rawInterval: notANumber, rawBuffer: validBuffer, wantErr: true},
		{name: "zero interval is rejected", rawInterval: "0s", rawBuffer: validBuffer, wantErr: true},
		{name: "negative interval is rejected", rawInterval: "-1m", rawBuffer: validBuffer, wantErr: true},
		{name: "malformed buffer is rejected", rawInterval: validInterval, rawBuffer: notANumber, wantErr: true},
		{name: "zero buffer is rejected", rawInterval: validInterval, rawBuffer: "0", wantErr: true},
		{name: "negative buffer is rejected", rawInterval: validInterval, rawBuffer: "-1", wantErr: true},
		{name: "fractional buffer is rejected", rawInterval: validInterval, rawBuffer: "1.5", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger := slog.New(slog.DiscardHandler)

			interval, buffer, err := parseNotificationConfig(logger, tt.rawInterval, tt.rawBuffer)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantInterval, interval)
			assert.Equal(t, tt.wantBuffer, buffer)
		})
	}
}

// A default nobody chose governs how often notifications go out and when they
// start being dropped, so falling back must be visible in the logs rather than
// silent.
func TestParseNotificationConfigWarnsOnEachFallback(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

	_, _, err := parseNotificationConfig(logger, unsetValue, unsetValue)
	require.NoError(t, err)

	assert.Contains(t, logs.String(), notificationTickerIntervalEnv)
	assert.Contains(t, logs.String(), notificationBufferSizeEnv)

	logs.Reset()
	_, _, err = parseNotificationConfig(logger, validInterval, validBuffer)
	require.NoError(t, err)

	assert.Empty(t, logs.String(), "nothing to warn about when both are configured")
}
