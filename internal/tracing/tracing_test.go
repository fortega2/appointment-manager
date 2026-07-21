package tracing_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"appointment-manager/internal/tracing"
)

const (
	testServiceName    = "appointment-manager"
	testServiceVersion = "1.2.3"
	testEndpoint       = "http://localhost:4318"
)

func TestInitDisabledWhenEndpointEmpty(t *testing.T) {
	t.Parallel()

	shutdown, err := tracing.Init(context.Background(), tracing.Config{
		ServiceName:    testServiceName,
		ServiceVersion: testServiceVersion,
		SampleRatio:    1,
	})

	require.NoError(t, err)
	require.NotNil(t, shutdown)
	assert.NoError(t, shutdown(context.Background()))
}

func TestInitRejectsSchemeLessEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint string
	}{
		{name: "bare host and port", endpoint: "tempo:4318"},
		{name: "unsupported scheme", endpoint: "ftp://tempo:4318"},
		{name: "unparsable url", endpoint: "://tempo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			shutdown, err := tracing.Init(context.Background(), tracing.Config{
				Endpoint:       tt.endpoint,
				ServiceName:    testServiceName,
				ServiceVersion: testServiceVersion,
				SampleRatio:    1,
			})

			require.Error(t, err)
			assert.Nil(t, shutdown)
		})
	}
}

func TestInitConfiguresProvider(t *testing.T) {
	shutdown, err := tracing.Init(context.Background(), tracing.Config{
		Endpoint:       testEndpoint,
		ServiceName:    testServiceName,
		ServiceVersion: testServiceVersion,
		SampleRatio:    0.5,
	})

	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Init installs a real SDK provider globally when an endpoint is set.
	_, isSDK := otel.GetTracerProvider().(*sdktrace.TracerProvider)
	assert.True(t, isSDK, "expected the global provider to be an SDK TracerProvider")

	assert.NoError(t, shutdown(context.Background()))
}
