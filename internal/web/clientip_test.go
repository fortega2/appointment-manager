package web_test

import (
	"appointment-manager/internal/web"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

const clientIPPath = "/login"

func TestClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		realIP     string
		remoteAddr string
		expected   string
		expectedOK bool
	}{
		{
			name:       "prefers the proxy header over the peer address",
			realIP:     "177.10.20.30",
			remoteAddr: "172.21.0.5:54321",
			expected:   "177.10.20.30",
			expectedOK: true,
		},
		{
			name:       "trims surrounding space",
			realIP:     "  177.10.20.30\t",
			remoteAddr: "172.21.0.5:54321",
			expected:   "177.10.20.30",
			expectedOK: true,
		},
		{
			name:       "falls back to the peer address without a header",
			remoteAddr: "203.0.113.7:44321",
			expected:   "203.0.113.7",
			expectedOK: true,
		},
		{
			name:       "ignores a header that is not an address",
			realIP:     "not-an-ip; drop table",
			remoteAddr: "203.0.113.7:44321",
			expected:   "203.0.113.7",
			expectedOK: true,
		},
		{
			name:       "unwraps an IPv4-mapped IPv6 address",
			realIP:     "::ffff:203.0.113.7",
			remoteAddr: "172.21.0.5:54321",
			expected:   "203.0.113.7",
			expectedOK: true,
		},
		{
			name:       "strips the zone from a link-local address",
			realIP:     "fe80::1%eth0",
			remoteAddr: "172.21.0.5:54321",
			expected:   "fe80::1",
			expectedOK: true,
		},
		{
			name:       "keeps an IPv6 address",
			realIP:     "2001:db8::42",
			remoteAddr: "172.21.0.5:54321",
			expected:   "2001:db8::42",
			expectedOK: true,
		},
		{
			name:       "reports nothing for a malformed peer address",
			remoteAddr: "malformed-addr",
			expectedOK: false,
		},
		{
			name:       "reports nothing when the peer host is not an address",
			remoteAddr: "example.com:8080",
			expectedOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, clientIPPath, nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.realIP != "" {
				req.Header.Set(web.RealIPHeader, tt.realIP)
			}

			addr, ok := web.ClientIP(req)

			assert.Equal(t, tt.expectedOK, ok)
			if !tt.expectedOK {
				assert.False(t, addr.IsValid())

				return
			}
			assert.Equal(t, tt.expected, addr.String())
		})
	}
}

func TestClientIPGivesOneKeyPerClient(t *testing.T) {
	t.Parallel()

	forms := []string{"203.0.113.7", "::ffff:203.0.113.7"}
	keys := make(map[string]struct{}, len(forms))

	for _, form := range forms {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, clientIPPath, nil)
		req.RemoteAddr = "172.21.0.5:54321"
		req.Header.Set(web.RealIPHeader, form)

		addr, ok := web.ClientIP(req)
		assert.True(t, ok)
		keys[addr.String()] = struct{}{}
	}

	assert.Len(t, keys, 1, "one client must not be able to present itself as two rate-limit keys")
}
