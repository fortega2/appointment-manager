package db_test

import (
	"appointment-manager/internal/db"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	dbInvalidDatabaseURL = "://invalid-url"
	dbUnknownSchemeURL   = "mysql://localhost:3306/app"
	dbValidDatabaseURL   = "postgres://localhost:5432/app?sslmode=disable"
)

func TestNewPostgresPoolValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		ctx         context.Context
		databaseURL string
		expectedErr error
	}{
		{
			name:        "nil context",
			ctx:         nil,
			databaseURL: dbValidDatabaseURL,
			expectedErr: db.ErrNilContext,
		},
		{
			name:        "empty database url",
			ctx:         context.Background(),
			databaseURL: "   ",
			expectedErr: db.ErrEmptyDatabaseURL,
		},
		{
			name:        "invalid database url",
			ctx:         context.Background(),
			databaseURL: dbInvalidDatabaseURL,
			expectedErr: db.ErrInvalidDatabaseURL,
		},
		{
			name:        "unknown database url scheme",
			ctx:         context.Background(),
			databaseURL: dbUnknownSchemeURL,
			expectedErr: db.ErrUnknownDatabaseScheme,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pool, err := db.NewPostgresPool(tt.ctx, tt.databaseURL)

			require.Error(t, err)
			assert.Nil(t, pool)
			assert.True(t, errors.Is(err, tt.expectedErr))
		})
	}
}
