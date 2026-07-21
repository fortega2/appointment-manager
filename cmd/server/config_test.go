package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSampleRatio(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    float64
		wantErr bool
	}{
		{name: "unset falls back to default", raw: "", want: defaultTraceSampleRatio},
		{name: "valid mid-range ratio", raw: "0.25", want: 0.25},
		{name: "valid boundary zero", raw: "0", want: 0},
		{name: "valid boundary one", raw: "1", want: 1},
		{name: "negative is rejected", raw: "-0.1", wantErr: true},
		{name: "above one is rejected", raw: "1.1", wantErr: true},
		{name: "malformed is rejected", raw: "not-a-number", wantErr: true},
		{name: "NaN is rejected", raw: "NaN", wantErr: true},
		{name: "positive infinity is rejected", raw: "+Inf", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseSampleRatio(tt.raw)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.InDelta(t, tt.want, got, 0)
		})
	}
}
