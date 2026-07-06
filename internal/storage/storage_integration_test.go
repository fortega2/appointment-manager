//go:build integration

package storage_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"appointment-manager/internal/storage"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/minio"
)

const (
	storageIntegrationImage  = "minio/minio:RELEASE.2024-01-16T16-07-38Z"
	storageIntegrationBucket = "prescriptions"
	presignExpiry            = 5 * time.Minute
)

func newStorageIntegrationClient(ctx context.Context, t *testing.T) *storage.Client {
	t.Helper()

	container, err := minio.Run(ctx, storageIntegrationImage)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testcontainers.TerminateContainer(container))
	})

	endpoint, err := container.ConnectionString(ctx)
	require.NoError(t, err)

	client, err := storage.NewClient(ctx, storage.Config{
		Endpoint:  endpoint,
		AccessKey: container.Username,
		SecretKey: container.Password,
		Bucket:    storageIntegrationBucket,
		UseSSL:    false,
	})
	require.NoError(t, err)

	return client
}

func TestClientUploadAndPresignedDownload(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	client := newStorageIntegrationClient(ctx, t)

	const (
		key     = "prescriptions/patient-1/doc.txt"
		content = "prescription document contents"
	)

	err := client.Upload(ctx, key, strings.NewReader(content), int64(len(content)), "text/plain")
	require.NoError(t, err)

	url, err := client.PresignedGetURL(ctx, key, presignExpiry)
	require.NoError(t, err)
	require.NotEmpty(t, url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, content, string(body))
}

func TestClientRemoveDeletesObject(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	client := newStorageIntegrationClient(ctx, t)

	const (
		key     = "prescriptions/patient-1/removable.txt"
		content = "to be removed"
	)

	require.NoError(t, client.Upload(ctx, key, strings.NewReader(content), int64(len(content)), "text/plain"))
	require.NoError(t, client.Remove(ctx, key))

	url, err := client.PresignedGetURL(ctx, key, presignExpiry)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}
