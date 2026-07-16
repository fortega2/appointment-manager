package main

import (
	"appointment-manager/internal/storage"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

const (
	storageEndpointEnv  = "STORAGE_ENDPOINT"
	storageAccessKeyEnv = "STORAGE_ACCESS_KEY"
	storageSecretKeyEnv = "STORAGE_SECRET_KEY"
	storageBucketEnv    = "STORAGE_BUCKET"
	storageRegionEnv    = "STORAGE_REGION"
	storageUseSSLEnv    = "STORAGE_USE_SSL"
)

// initializeStorageClient builds the object-storage client from the STORAGE_*
// env vars. When STORAGE_ENDPOINT is unset the client is disabled (returns nil)
// so the server still boots in dev without Garage; a set-but-misconfigured
// store fails fast here rather than on the first upload.
func initializeStorageClient(ctx context.Context, logger *slog.Logger) (*storage.Client, error) {
	endpoint := strings.TrimSpace(os.Getenv(storageEndpointEnv))
	if endpoint == "" {
		logger.Warn("storage endpoint is not set, object storage is disabled")
		return nil, nil //nolint:nilnil // nil client + nil error is the documented "disabled" signal; callers check client == nil, not err.
	}

	useSSL := true
	if raw := strings.TrimSpace(os.Getenv(storageUseSSLEnv)); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", storageUseSSLEnv, err)
		}
		useSSL = parsed
	}

	client, err := storage.NewClient(ctx, storage.Config{
		Endpoint:  endpoint,
		AccessKey: strings.TrimSpace(os.Getenv(storageAccessKeyEnv)),
		SecretKey: strings.TrimSpace(os.Getenv(storageSecretKeyEnv)),
		Bucket:    strings.TrimSpace(os.Getenv(storageBucketEnv)),
		Region:    strings.TrimSpace(os.Getenv(storageRegionEnv)),
		UseSSL:    useSSL,
	})
	if err != nil {
		logger.Error("failed to initialize storage client", slog.Any("error", err))
		return nil, err
	}

	logger.Info("storage client initialized", slog.String("endpoint", endpoint))
	return client, nil
}
