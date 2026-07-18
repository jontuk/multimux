package server

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// statusRecorder captures response metadata for request completion logs.
// Unwrap and Hijack preserve optional ResponseWriter behavior used by
// WebSocket upgrades.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	err         string
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.status = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if !r.wroteHeader {
		r.status = http.StatusOK
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(p)
}

func (r *statusRecorder) recordError(err string) {
	r.err = err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(r.ResponseWriter).Hijack()
	if err == nil && !r.wroteHeader {
		r.status = http.StatusSwitchingProtocols
		r.wroteHeader = true
	}
	return conn, rw, err
}

func requestLogPath(path string) string {
	for pattern, prefix := range map[string]string{
		"/api/auth/credentials/{id}": "/api/auth/credentials/",
		"/api/auth/sessions/{hash}":  "/api/auth/sessions/",
	} {
		if strings.HasPrefix(path, prefix) {
			return pattern
		}
	}
	return path
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		if !strings.HasPrefix(r.URL.Path, "/api/") &&
			!strings.HasPrefix(r.URL.Path, "/ws/") &&
			rec.status < http.StatusBadRequest {
			return
		}

		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", requestLogPath(r.URL.Path)),
			slog.Int("status", rec.status),
			slog.Duration("duration", time.Since(start)),
		}
		level := slog.LevelInfo
		if rec.status >= http.StatusInternalServerError {
			level = slog.LevelError
			if rec.err != "" {
				attrs = append(attrs, slog.String("error", rec.err))
			}
		}
		slog.LogAttrs(r.Context(), level, "http", attrs...)
	})
}
