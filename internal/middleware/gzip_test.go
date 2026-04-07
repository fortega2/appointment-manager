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
	contentTypeHeader     = "Content-Type"
	rangeHeader           = "Range"
	varyHeader            = "Vary"

	gzipEncoding              = "gzip"
	apiPath                   = "/api/v1/assistants"
	jsonResponseBody          = `{"name":"jane"}`
	nonCompressibleBody       = "\x89PNG\r\n\x1a\n"
	gzipAcceptEncoding        = "gzip, deflate"
	mixedAcceptEncoding       = "br, gzip;q=0.7"
	rejectedGzipAcceptEncoder = "gzip;q=0"
	wildcardAcceptEncoding    = "gzip;q=0, *;q=1"
	compressibleJSONType      = "application/json"
	nonCompressiblePNGType    = "image/png"
)

func TestGzipCompressesResponseWhenClientSupportsGzip(t *testing.T) {
	t.Parallel()

	handler := middleware.Gzip(middleware.DefaultGzipConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentTypeHeader, compressibleJSONType)
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

	handler := middleware.Gzip(middleware.DefaultGzipConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

	handler := middleware.Gzip(middleware.DefaultGzipConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(jsonResponseBody))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, apiPath, nil)
	req.Header.Set(acceptEncodingHeader, mixedAcceptEncoding)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, gzipEncoding, rec.Header().Get(contentEncodingHeader))
	assert.Equal(t, jsonResponseBody, decodeGzipBody(t, rec.Body.Bytes()))
}

func TestGzipSkipsWhenEncodingQualityIsZero(t *testing.T) {
	t.Parallel()

	handler := middleware.Gzip(middleware.DefaultGzipConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(jsonResponseBody))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, apiPath, nil)
	req.Header.Set(acceptEncodingHeader, rejectedGzipAcceptEncoder)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "", rec.Header().Get(contentEncodingHeader))
	assert.Equal(t, jsonResponseBody, rec.Body.String())
}

func TestGzipSkipsWhenGzipRejectedEvenWithWildcard(t *testing.T) {
	t.Parallel()

	handler := middleware.Gzip(middleware.DefaultGzipConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(jsonResponseBody))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, apiPath, nil)
	req.Header.Set(acceptEncodingHeader, wildcardAcceptEncoding)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "", rec.Header().Get(contentEncodingHeader))
	assert.Equal(t, jsonResponseBody, rec.Body.String())
}

func TestGzipAddsVaryAcceptEncodingWithoutDuplicates(t *testing.T) {
	t.Parallel()

	handler := middleware.Gzip(middleware.DefaultGzipConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add(varyHeader, acceptEncodingHeader)
		_, _ = w.Write([]byte(jsonResponseBody))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, apiPath, nil)
	req.Header.Set(acceptEncodingHeader, gzipEncoding)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, 1, varyTokenCount(rec.Header(), acceptEncodingHeader))
}

func TestGzipSkipsCompressionForHeadRequests(t *testing.T) {
	t.Parallel()

	handler := middleware.Gzip(middleware.DefaultGzipConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentTypeHeader, compressibleJSONType)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodHead, apiPath, nil)
	req.Header.Set(acceptEncodingHeader, gzipEncoding)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "", rec.Header().Get(contentEncodingHeader))
}

func TestGzipSkipsCompressionForRangeRequests(t *testing.T) {
	t.Parallel()

	handler := middleware.Gzip(middleware.DefaultGzipConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentTypeHeader, compressibleJSONType)
		_, _ = w.Write([]byte(jsonResponseBody))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, apiPath, nil)
	req.Header.Set(acceptEncodingHeader, gzipEncoding)
	req.Header.Set(rangeHeader, "bytes=0-10")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "", rec.Header().Get(contentEncodingHeader))
	assert.Equal(t, jsonResponseBody, rec.Body.String())
}

func TestGzipSkipsCompressionForNonCompressibleContentType(t *testing.T) {
	t.Parallel()

	handler := middleware.Gzip(middleware.DefaultGzipConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentTypeHeader, nonCompressiblePNGType)
		_, _ = w.Write([]byte(nonCompressibleBody))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, apiPath, nil)
	req.Header.Set(acceptEncodingHeader, gzipEncoding)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "", rec.Header().Get(contentEncodingHeader))
	assert.Equal(t, nonCompressibleBody, rec.Body.String())
}

func TestGzipSkipsCompressionWhenResponseAlreadyEncoded(t *testing.T) {
	t.Parallel()

	handler := middleware.Gzip(middleware.DefaultGzipConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentEncodingHeader, "br")
		w.Header().Set(contentTypeHeader, compressibleJSONType)
		_, _ = w.Write([]byte(jsonResponseBody))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, apiPath, nil)
	req.Header.Set(acceptEncodingHeader, gzipEncoding)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "br", rec.Header().Get(contentEncodingHeader))
	assert.Equal(t, jsonResponseBody, rec.Body.String())
}

func TestGzipPreservesFirstWriteHeader(t *testing.T) {
	t.Parallel()

	handler := middleware.Gzip(middleware.DefaultGzipConfig())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentTypeHeader, compressibleJSONType)
		w.WriteHeader(http.StatusInternalServerError)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(jsonResponseBody))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, apiPath, nil)
	req.Header.Set(acceptEncodingHeader, gzipEncoding)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, gzipEncoding, rec.Header().Get(contentEncodingHeader))
	assert.Equal(t, jsonResponseBody, decodeGzipBody(t, rec.Body.Bytes()))
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
