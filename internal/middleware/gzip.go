package middleware

import (
	"compress/gzip"
	"net/http"
	"strings"
)

const (
	headerAcceptEncoding  = "Accept-Encoding"
	headerContentEncoding = "Content-Encoding"
	headerContentLength   = "Content-Length"
	headerVary            = "Vary"

	encodingGzip        = "gzip"
	varyAcceptEncoding  = "Accept-Encoding"
	acceptEncodingSplit = ","
	varySplit           = ","
	varyJoinSeparator   = ", "
)

type gzipResponseWriter struct {
	http.ResponseWriter

	writer *gzip.Writer
}

func (w gzipResponseWriter) Write(body []byte) (int, error) {
	normalizeVaryHeader(w.Header())

	return w.writer.Write(body)
}

func Gzip() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !acceptsGzip(r.Header.Get(headerAcceptEncoding)) {
				next.ServeHTTP(w, r)
				return
			}

			addVaryAcceptEncoding(w.Header())
			w.Header().Set(headerContentEncoding, encodingGzip)
			w.Header().Del(headerContentLength)

			gzipWriter := gzip.NewWriter(w)
			defer func() {
				_ = gzipWriter.Close()
			}()

			next.ServeHTTP(gzipResponseWriter{ResponseWriter: w, writer: gzipWriter}, r)
		})
	}
}

func acceptsGzip(acceptEncoding string) bool {
	for value := range strings.SplitSeq(acceptEncoding, acceptEncodingSplit) {
		token, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(value)), ";")
		if token == encodingGzip {
			return true
		}
	}

	return false
}

func addVaryAcceptEncoding(header http.Header) {
	tokens := varyTokens(header)
	for _, token := range tokens {
		if strings.EqualFold(token, varyAcceptEncoding) {
			writeVaryTokens(header, tokens)
			return
		}
	}

	tokens = append(tokens, varyAcceptEncoding)
	writeVaryTokens(header, tokens)
}

func normalizeVaryHeader(header http.Header) {
	writeVaryTokens(header, varyTokens(header))
}

func varyTokens(header http.Header) []string {
	values := header.Values(headerVary)
	if len(values) == 0 {
		return nil
	}

	tokens := make([]string, 0)
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		for token := range strings.SplitSeq(value, varySplit) {
			trimmedToken := strings.TrimSpace(token)
			if trimmedToken == "" {
				continue
			}

			normalizedKey := strings.ToLower(trimmedToken)
			if _, ok := seen[normalizedKey]; ok {
				continue
			}

			seen[normalizedKey] = struct{}{}
			if strings.EqualFold(trimmedToken, varyAcceptEncoding) {
				tokens = append(tokens, varyAcceptEncoding)
				continue
			}

			tokens = append(tokens, trimmedToken)
		}
	}

	return tokens
}

func writeVaryTokens(header http.Header, tokens []string) {
	header.Del(headerVary)
	if len(tokens) == 0 {
		return
	}

	header.Add(headerVary, strings.Join(tokens, varyJoinSeparator))
}
