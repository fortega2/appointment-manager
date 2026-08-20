package main

import (
	"appointment-manager/internal/password"
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	email          = "assistant@example.com"
	emailFlag      = "-email=" + email
	generatedRunes = 24
)

func TestParseFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    string
		timeout time.Duration
		wantErr bool
	}{
		{name: "email only", args: []string{emailFlag}, want: email, timeout: defaultTimeout},
		{
			name:    "explicit timeout",
			args:    []string{emailFlag, "-timeout=5s"},
			want:    email,
			timeout: 5 * time.Second,
		},
		{name: "surrounding spaces", args: []string{"-email=  " + email + "  "}, want: email, timeout: defaultTimeout},
		{name: "no email", args: []string{}, wantErr: true},
		{name: "blank email", args: []string{"-email=   "}, wantErr: true},
		{name: "zero timeout", args: []string{emailFlag, "-timeout=0s"}, wantErr: true},
		{name: "negative timeout", args: []string{emailFlag, "-timeout=-1s"}, wantErr: true},
		{name: "unknown flag", args: []string{emailFlag, "-password=hunter2"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, timeout, err := parseFlags(tt.args)
			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.timeout, timeout)
		})
	}
}

func TestGeneratePassword(t *testing.T) {
	t.Parallel()

	plain, err := generatePassword()
	require.NoError(t, err)

	assert.Len(t, plain, generatedRunes)
	assert.NoError(t, password.Validate(plain))

	other, err := generatePassword()
	require.NoError(t, err)
	assert.NotEqual(t, plain, other)
}

func TestRunRejectsBadUsage(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	assert.Equal(t, exitBadUsage, run(nil, &stdout, &stderr))
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "usage error")
}

// TestRunFailsWithoutDatabaseURL also pins that a failed run prints nothing on
// stdout, which is what makes the stream safe to capture as the password.
func TestRunFailsWithoutDatabaseURL(t *testing.T) {
	t.Setenv(databaseURLEnv, "")

	var stdout, stderr bytes.Buffer

	assert.Equal(t, exitFailure, run([]string{emailFlag}, &stdout, &stderr))
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), databaseURLEnv)
}
