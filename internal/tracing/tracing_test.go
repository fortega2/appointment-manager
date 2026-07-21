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
