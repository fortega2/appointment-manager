package storage

import "strings"

// Config holds the connection settings for an S3-compatible object store
// Region is optional; UseSSL toggles HTTPS.
type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	UseSSL    bool
}

func (c Config) validate() error {
	if strings.TrimSpace(c.Endpoint) == "" {
		return ErrEmptyEndpoint
	}
	if strings.TrimSpace(c.AccessKey) == "" {
		return ErrEmptyAccessKey
	}
	if strings.TrimSpace(c.SecretKey) == "" {
		return ErrEmptySecretKey
	}
	if strings.TrimSpace(c.Bucket) == "" {
		return ErrEmptyBucket
	}
	return nil
}
