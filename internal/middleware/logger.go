package middleware

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

type responseRecorder struct {
	http.ResponseWriter

	status      int
	bytes       int64
	wroteHeader bool
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{
		ResponseWriter: w,
		status:         http.StatusOK,
	}
}

func (rw *responseRecorder) WriteHeader(status int) {
	if rw.wroteHeader {
		return
	}

	rw.status = status
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseRecorder) Write(body []byte) (int, error) {
	if !rw.wroteHeader {
		rw.wroteHeader = true
	}

	n, err := rw.ResponseWriter.Write(body)
	rw.bytes += int64(n)

	return n, err
}

func (rw *responseRecorder) Flush() {
	flusher, ok := rw.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}

	flusher.Flush()
}

func (rw *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := rw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}

	return hijacker.Hijack()
}

func (rw *responseRecorder) Push(target string, opts *http.PushOptions) error {
	pusher, ok := rw.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}

	return pusher.Push(target, opts)
}

func (rw *responseRecorder) ReadFrom(src io.Reader) (int64, error) {
	readerFrom, ok := rw.ResponseWriter.(io.ReaderFrom)
	if !ok {
		n, err := io.Copy(rw.ResponseWriter, src)
		rw.bytes += n

		return n, err
	}

	n, err := readerFrom.ReadFrom(src)
	rw.bytes += n

	return n, err
}

func (rw *responseRecorder) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	return func(next http.Handler) http.Handler {
		if next == nil {
			next = http.NotFoundHandler()
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := newResponseRecorder(w)

			next.ServeHTTP(rw, r)

			route := requestRoute(r)

			if route == "/readyz" || route == "/healthz" {
				return
			}

			level := slog.LevelInfo
			switch {
			case rw.status >= http.StatusInternalServerError:
				level = slog.LevelError
			case rw.status >= http.StatusBadRequest:
				level = slog.LevelWarn
			}

			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.Int64("response_bytes", rw.bytes),
				slog.Int64("request_content_length", r.ContentLength),
				slog.String("client_ip", remoteIP(r.RemoteAddr)),
				slog.String("user_agent", r.UserAgent()),
			}

			requestID := r.Header.Get(requestIDHeader)
			if requestID != "" {
				attrs = append(attrs, slog.String("request_id", requestID))
			}

			logger.LogAttrs(r.Context(), level, "http request completed", attrs...)
		})
	}
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}

	return host
}

func requestRoute(r *http.Request) string {
	if r.Pattern == "" {
		return r.URL.Path
	}

	_, route, hasMethodPattern := strings.Cut(r.Pattern, " ")
	if !hasMethodPattern {
		return r.Pattern
	}

	return route
}
