package middleware_test

import (
	"appointment-manager/internal/middleware"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	acceptEncodingHeader  = "Accept-Encoding"
	contentEncodingHeader = "Content-Encoding"
	contentLengthHeader   = "Content-Length"
	varyHeader            = "Vary"

	gzipEncoding        = "gzip"
	jsonContentType     = "application/json"
	apiPath             = "/api/v1/assistants"
	jsonResponseBody    = `{"name":"jane"}`
	gzipAcceptEncoding  = "gzip, deflate"
	mixedAcceptEncoding = "br, gzip;q=0.7"
)

func TestGzipCompressesResponseWhenClientSupportsGzip(t *testing.T) {
	t.Parallel()

	handler := middleware.Gzip()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", jsonContentType)
		_, _ = w.Write([]byte(jsonResponseBody))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, apiPath, nil)
	req.Header.Set(acceptEncodingHeader, gzipAcceptEncoding)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, gzipEncoding, rec.Header().Get(contentEncodingHeader))
	assert.Equal(t, "", rec.Header().Get(contentLengthHeader))
	assert.Equal(t, 1, varyTokenCount(rec.Header(), acceptEncodingHeader))
	assert.Equal(t, jsonResponseBody, decodeGzipBody(t, rec.Body.Bytes()))
}

func TestGzipPassesThroughWhenClientDoesNotSupportGzip(t *testing.T) {
	t.Parallel()

	handler := middleware.Gzip()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(jsonResponseBody))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, apiPath, nil)
	req.Header.Set(acceptEncodingHeader, "br")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "", rec.Header().Get(contentEncodingHeader))
	assert.Equal(t, "", rec.Header().Get(varyHeader))
	assert.Equal(t, jsonResponseBody, rec.Body.String())
}

func TestGzipAcceptsEncodingTokenWithQualityValue(t *testing.T) {
	t.Parallel()

	handler := middleware.Gzip()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(jsonResponseBody))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, apiPath, nil)
	req.Header.Set(acceptEncodingHeader, mixedAcceptEncoding)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, gzipEncoding, rec.Header().Get(contentEncodingHeader))
	assert.Equal(t, jsonResponseBody, decodeGzipBody(t, rec.Body.Bytes()))
}

func TestGzipAddsVaryAcceptEncodingWithoutDuplicates(t *testing.T) {
	t.Parallel()

	handler := middleware.Gzip()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add(varyHeader, acceptEncodingHeader)
		_, _ = w.Write([]byte(jsonResponseBody))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, apiPath, nil)
	req.Header.Set(acceptEncodingHeader, gzipEncoding)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, 1, varyTokenCount(rec.Header(), acceptEncodingHeader))
}

func decodeGzipBody(t *testing.T, body []byte) string {
	t.Helper()

	gzipReader, err := gzip.NewReader(bytes.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, gzipReader.Close())
	})

	decodedBody, err := io.ReadAll(gzipReader)
	require.NoError(t, err)

	return string(decodedBody)
}

func varyTokenCount(header http.Header, token string) int {
	count := 0
	for _, value := range header.Values(varyHeader) {
		for valueToken := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(valueToken), token) {
				count++
			}
		}
	}

	return count
}
