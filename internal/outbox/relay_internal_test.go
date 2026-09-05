package outbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBackoff(t *testing.T) {
	tests := map[string]struct {
		attempts int16
		want     time.Duration
	}{
		"first attempt":              {attempts: 0, want: time.Second},
		"second attempt":             {attempts: 1, want: 2 * time.Second},
		"grows exponentially":        {attempts: 8, want: 256 * time.Second},
		"capped at boundary":         {attempts: 9, want: backoffCap},
		"capped far beyond":          {attempts: 30, want: backoffCap},
		"capped past int64 overflow": {attempts: 34, want: backoffCap},
		"capped at the column limit": {attempts: 32767, want: backoffCap},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, backoff(tt.attempts))
		})
	}
}
