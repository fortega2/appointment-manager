package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func validConfig() Config {
	return Config{
		Endpoint:  "localhost:9000",
		AccessKey: "access",
		SecretKey: "secret",
		Bucket:    "prescriptions",
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	if err := validConfig().validate(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestConfigValidateErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr error
	}{
		{"empty endpoint", func(c *Config) { c.Endpoint = "" }, ErrEmptyEndpoint},
		{"whitespace endpoint", func(c *Config) { c.Endpoint = "   " }, ErrEmptyEndpoint},
		{"empty access key", func(c *Config) { c.AccessKey = "" }, ErrEmptyAccessKey},
		{"empty secret key", func(c *Config) { c.SecretKey = "" }, ErrEmptySecretKey},
		{"empty bucket", func(c *Config) { c.Bucket = "" }, ErrEmptyBucket},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			tt.mutate(&cfg)

			if err := cfg.validate(); !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestNewClientNilContext(t *testing.T) {
	t.Parallel()

	var ctx context.Context // deliberately nil to exercise the guard

	if _, err := NewClient(ctx, validConfig()); !errors.Is(err, ErrNilContext) {
		t.Fatalf("expected ErrNilContext, got %v", err)
	}
}

func TestNewClientInvalidConfig(t *testing.T) {
	t.Parallel()

	if _, err := NewClient(context.Background(), Config{}); !errors.Is(err, ErrEmptyEndpoint) {
		t.Fatalf("expected ErrEmptyEndpoint, got %v", err)
	}
}

func TestUploadGuards(t *testing.T) {
	t.Parallel()

	client := &Client{bucket: "prescriptions"}

	if err := client.Upload(context.Background(), "", strings.NewReader("x"), 1, ""); !errors.Is(err, ErrEmptyObjectKey) {
		t.Fatalf("expected ErrEmptyObjectKey, got %v", err)
	}

	if err := client.Upload(context.Background(), "key", nil, 0, ""); !errors.Is(err, ErrNilReader) {
		t.Fatalf("expected ErrNilReader, got %v", err)
	}
}

func TestPresignedGetURLEmptyKey(t *testing.T) {
	t.Parallel()

	client := &Client{bucket: "prescriptions"}

	if _, err := client.PresignedGetURL(context.Background(), "", 0); !errors.Is(err, ErrEmptyObjectKey) {
		t.Fatalf("expected ErrEmptyObjectKey, got %v", err)
	}
}

func TestRemoveEmptyKey(t *testing.T) {
	t.Parallel()

	client := &Client{bucket: "prescriptions"}

	if err := client.Remove(context.Background(), ""); !errors.Is(err, ErrEmptyObjectKey) {
		t.Fatalf("expected ErrEmptyObjectKey, got %v", err)
	}
}
