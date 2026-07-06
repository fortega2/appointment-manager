package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client is a thin wrapper around a minio client bound to a single bucket. It
// speaks the S3 protocol, so it works against any S3-compatible store.
type Client struct {
	minio  *minio.Client
	bucket string
	region string
}

// NewClient validates the config, dials the object store and ensures the target
// bucket exists. Reaching the store here surfaces bad endpoints or credentials
// at startup instead of on the first upload.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	minioClient, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
		// Garage (and most self-hosted MinIO setups behind a reverse proxy) route by
		// path, not by bucket subdomain: force path-style so requests hit
		// https://endpoint/bucket instead of https://bucket.endpoint, which the
		// reverse proxy has no vhost for.
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	client := &Client{
		minio:  minioClient,
		bucket: cfg.Bucket,
		region: cfg.Region,
	}

	if err := client.ensureBucket(ctx); err != nil {
		return nil, err
	}

	return client, nil
}

// Upload stores an object under key, streaming from reader. size is the exact
// number of bytes to read (as provided by the multipart file header).
func (c *Client) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	if key == "" {
		return ErrEmptyObjectKey
	}
	if reader == nil {
		return ErrNilReader
	}

	_, err := c.minio.PutObject(ctx, c.bucket, key, reader, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("upload object %q: %w", key, err)
	}

	return nil
}

// PresignedGetURL returns a temporary, signed URL that lets a browser download
// the object directly from the store, without proxying it through the app.
func (c *Client) PresignedGetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	if key == "" {
		return "", ErrEmptyObjectKey
	}

	presignedURL, err := c.minio.PresignedGetObject(ctx, c.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign object %q: %w", key, err)
	}

	return presignedURL.String(), nil
}

// Remove deletes an object, used to roll back an upload when a later step (e.g.
// persisting the prescription row) fails and would otherwise orphan the file.
func (c *Client) Remove(ctx context.Context, key string) error {
	if key == "" {
		return ErrEmptyObjectKey
	}

	if err := c.minio.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove object %q: %w", key, err)
	}

	return nil
}

func (c *Client) ensureBucket(ctx context.Context) error {
	exists, err := c.minio.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("check bucket %q: %w", c.bucket, err)
	}
	if exists {
		return nil
	}

	if err := c.minio.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{Region: c.region}); err != nil {
		return fmt.Errorf("create bucket %q: %w", c.bucket, err)
	}

	return nil
}
