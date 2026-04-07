package middleware

import (
	"bufio"
	"compress/gzip"
	"io"
	"maps"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

const (
	headerAcceptEncoding  = "Accept-Encoding"
	headerContentEncoding = "Content-Encoding"
	headerContentLength   = "Content-Length"
	headerContentType     = "Content-Type"
	headerRange           = "Range"
	headerUpgrade         = "Upgrade"
	headerConnection      = "Connection"
	headerVary            = "Vary"

	encodingGzip      = "gzip"
	encodingWildcard  = "*"
	varyJoinSeparator = ", "
)

var defaultCompressibleMIMEs = map[string]struct{}{
	"application/json":         {},
	"application/problem+json": {},
	"text/plain":               {},
	"text/html":                {},
	"text/css":                 {},
	"application/javascript":   {},
	"text/javascript":          {},
	"application/xml":          {},
	"text/xml":                 {},
}

type GzipConfig struct {
	Level             int
	CompressibleMIMEs map[string]struct{}
}

func DefaultGzipConfig() GzipConfig {
	return GzipConfig{
		Level:             gzip.BestSpeed,
		CompressibleMIMEs: cloneMIMEs(defaultCompressibleMIMEs),
	}
}

func Gzip(cfg GzipConfig) func(http.Handler) http.Handler {
	cfg = normalizeConfig(cfg)
	pool := &sync.Pool{
		New: func() any {
			gzipWriter, err := gzip.NewWriterLevel(io.Discard, cfg.Level)
			if err != nil {
				return gzip.NewWriter(io.Discard)
			}

			return gzipWriter
		},
	}

	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead ||
				r.Header.Get(headerRange) != "" ||
				isUpgradeRequest(r) ||
				!acceptsGzip(r.Header.Get(headerAcceptEncoding)) {
				next.ServeHTTP(w, r)
				return
			}

			addVary(w.Header(), headerAcceptEncoding)

			gzipWriter := &gzipResponseWriter{
				ResponseWriter:    w,
				pool:              pool,
				compressibleMIMEs: cfg.CompressibleMIMEs,
			}

			next.ServeHTTP(gzipWriter, r)
			_ = gzipWriter.Close()
		})
	}
}

type gzipResponseWriter struct {
	http.ResponseWriter

	pool              *sync.Pool
	compressibleMIMEs map[string]struct{}
	gz                *gzip.Writer
	statusCode        int
	headerSet         bool
	wroteHeader       bool
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	if w.headerSet {
		return
	}

	w.statusCode = code
	w.headerSet = true
}

func (w *gzipResponseWriter) Write(body []byte) (int, error) {
	w.ensureHeaderAndWriter(body)
	if w.gz != nil {
		return w.gz.Write(body)
	}

	return w.ResponseWriter.Write(body)
}

func (w *gzipResponseWriter) Flush() {
	w.ensureHeaderAndWriter(nil)
	if w.gz != nil {
		_ = w.gz.Flush()
	}

	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}

	return hijacker.Hijack()
}

func (w *gzipResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}

	return pusher.Push(target, opts)
}

func (w *gzipResponseWriter) Close() error {
	if !w.wroteHeader {
		w.ensureHeaderAndWriter(nil)
	}

	if w.gz == nil {
		return nil
	}

	err := w.gz.Close()
	w.pool.Put(w.gz)
	w.gz = nil

	return err
}

func (w *gzipResponseWriter) ensureHeaderAndWriter(sample []byte) {
	if w.wroteHeader {
		return
	}

	statusCode := w.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	if canHaveBody(statusCode) &&
		w.Header().Get(headerContentEncoding) == "" &&
		isCompressibleContentType(w.Header(), sample, w.compressibleMIMEs) {
		w.Header().Set(headerContentEncoding, encodingGzip)
		w.Header().Del(headerContentLength)

		gzipWriter := w.pool.Get().(*gzip.Writer)
		gzipWriter.Reset(w.ResponseWriter)
		w.gz = gzipWriter
	}

	normalizeVaryHeader(w.Header())

	w.ResponseWriter.WriteHeader(statusCode)
	w.wroteHeader = true
}

func normalizeConfig(cfg GzipConfig) GzipConfig {
	if cfg.Level < gzip.HuffmanOnly || cfg.Level > gzip.BestCompression {
		cfg.Level = gzip.BestSpeed
	}

	if cfg.CompressibleMIMEs == nil {
		cfg.CompressibleMIMEs = cloneMIMEs(defaultCompressibleMIMEs)
	}

	return cfg
}

func canHaveBody(code int) bool {
	return code >= http.StatusOK &&
		code != http.StatusNoContent &&
		code != http.StatusNotModified
}

func isCompressibleContentType(header http.Header, sample []byte, allow map[string]struct{}) bool {
	contentType := strings.TrimSpace(header.Get(headerContentType))
	if contentType == "" && len(sample) > 0 {
		contentType = http.DetectContentType(sample)
		header.Set(headerContentType, contentType)
	}

	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		return false
	}

	_, ok := allow[strings.ToLower(mediaType)]

	return ok
}

func acceptsGzip(value string) bool {
	gzipQuality := 0.0
	hasExplicitGzip := false
	wildcardQuality := 0.0
	hasWildcard := false

	for token := range strings.SplitSeq(strings.ToLower(value), ",") {
		coding, quality, ok := parseAcceptEncodingToken(token)
		if !ok {
			continue
		}

		if coding == encodingGzip {
			hasExplicitGzip = true
			gzipQuality = quality
			continue
		}

		hasWildcard = true
		wildcardQuality = quality
	}

	if hasExplicitGzip {
		return gzipQuality > 0
	}

	if hasWildcard {
		return wildcardQuality > 0
	}

	return false
}

func parseAcceptEncodingToken(token string) (string, float64, bool) {
	trimmedToken := strings.TrimSpace(token)
	if trimmedToken == "" {
		return "", 0, false
	}

	parts := strings.Split(trimmedToken, ";")
	coding := strings.TrimSpace(parts[0])
	if coding != encodingGzip && coding != encodingWildcard {
		return "", 0, false
	}

	quality := 1.0
	for _, param := range parts[1:] {
		parsedQuality, ok := parseAcceptEncodingQuality(param)
		if ok {
			quality = parsedQuality
		}
	}

	return coding, quality, true
}

func parseAcceptEncodingQuality(param string) (float64, bool) {
	trimmedParam := strings.TrimSpace(param)
	if !strings.HasPrefix(trimmedParam, "q=") {
		return 0, false
	}

	parsedQuality, err := strconv.ParseFloat(strings.TrimPrefix(trimmedParam, "q="), 64)
	if err != nil {
		return 0, false
	}

	return parsedQuality, true
}

func isUpgradeRequest(r *http.Request) bool {
	connection := strings.ToLower(r.Header.Get(headerConnection))

	return strings.Contains(connection, "upgrade") || r.Header.Get(headerUpgrade) != ""
}

func addVary(header http.Header, value string) {
	tokens := make([]string, 0)
	seen := map[string]struct{}{}

	for _, current := range header.Values(headerVary) {
		for token := range strings.SplitSeq(current, ",") {
			trimmedToken := strings.TrimSpace(token)
			if trimmedToken == "" {
				continue
			}

			normalized := strings.ToLower(trimmedToken)
			if _, ok := seen[normalized]; ok {
				continue
			}

			seen[normalized] = struct{}{}
			if strings.EqualFold(trimmedToken, value) {
				tokens = append(tokens, value)
				continue
			}

			tokens = append(tokens, trimmedToken)
		}
	}

	normalizedValue := strings.ToLower(value)
	if _, ok := seen[normalizedValue]; !ok {
		tokens = append(tokens, value)
	}

	header.Del(headerVary)
	if len(tokens) == 0 {
		return
	}

	header.Add(headerVary, strings.Join(tokens, varyJoinSeparator))
}

func normalizeVaryHeader(header http.Header) {
	values := header.Values(headerVary)
	if len(values) == 0 {
		return
	}

	tokens := make([]string, 0)
	seen := map[string]struct{}{}
	for _, value := range values {
		for token := range strings.SplitSeq(value, ",") {
			trimmedToken := strings.TrimSpace(token)
			if trimmedToken == "" {
				continue
			}

			normalizedToken := strings.ToLower(trimmedToken)
			if _, ok := seen[normalizedToken]; ok {
				continue
			}

			seen[normalizedToken] = struct{}{}
			tokens = append(tokens, trimmedToken)
		}
	}

	header.Del(headerVary)
	if len(tokens) == 0 {
		return
	}

	header.Add(headerVary, strings.Join(tokens, varyJoinSeparator))
}

func cloneMIMEs(src map[string]struct{}) map[string]struct{} {
	dst := make(map[string]struct{}, len(src))
	maps.Copy(dst, src)

	return dst
}
