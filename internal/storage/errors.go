package storage

import "errors"

var (
	ErrNilContext     = errors.New("nil context")
	ErrEmptyEndpoint  = errors.New("empty storage endpoint")
	ErrEmptyAccessKey = errors.New("empty storage access key")
	ErrEmptySecretKey = errors.New("empty storage secret key")
	ErrEmptyBucket    = errors.New("empty storage bucket")
	ErrEmptyObjectKey = errors.New("empty object key")
	ErrNilReader      = errors.New("nil object reader")
)
