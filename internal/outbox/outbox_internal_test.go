package outbox

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncodePayload(t *testing.T) {
	tests := map[string]struct {
		payload any
		want    string
		wantErr bool
	}{
		"nil becomes empty object": {payload: nil, want: `{}`},
		"object payload":           {payload: map[string]string{"a": "b"}, want: `{"a":"b"}`},
		"empty struct":             {payload: struct{}{}, want: `{}`},
		"slice is not an object":   {payload: []string{"a"}, wantErr: true},
		"string is not an object":  {payload: "a", wantErr: true},
		"number is not an object":  {payload: 5, wantErr: true},
		"unmarshalable payload":    {payload: make(chan int), wantErr: true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := encodePayload(tt.payload)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}

func TestInsertNilTx(t *testing.T) {
	err := Insert(context.Background(), nil, Event{
		AggregateType: "test_aggregate",
		EventType:     "test.event",
	})
	assert.ErrorIs(t, err, ErrNilTx)
}
