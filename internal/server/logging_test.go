package server

import (
	"bufio"
	"bytes"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestRequestLoggingIncludesApplicationRequestDetails(t *testing.T) {
	buf := captureLogs(t)
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"token": "response-secret"})
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tools?token=query-secret", nil))

	logged := buf.String()
	for _, want := range []string{
		`"level":"INFO"`,
		`"msg":"http"`,
		`"method":"GET"`,
		`"path":"/api/tools"`,
		`"status":200`,
		`"duration":`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log missing %q: %s", want, logged)
		}
	}
	for _, secret := range []string{"query-secret", "response-secret"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("log exposed %q: %s", secret, logged)
		}
	}
}

func TestRequestLoggingSuppressesSuccessfulNonApplicationPaths(t *testing.T) {
	buf := captureLogs(t)
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if buf.Len() != 0 {
		t.Fatalf("routine non-application requests logged: %s", buf.String())
	}
}

func TestRequestLoggingIncludesErrorsOnAnyPath(t *testing.T) {
	buf := captureLogs(t)
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database closed"})
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz?token=secret", nil))

	logged := buf.String()
	for _, want := range []string{
		`"level":"ERROR"`,
		`"path":"/healthz"`,
		`"status":500`,
		`"error":"database closed"`,
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("error log missing %q: %s", want, logged)
		}
	}
	if strings.Contains(logged, "secret") {
		t.Fatalf("query string leaked: %s", logged)
	}
}

func TestRequestLoggingIncludesClientErrorsOnAnyPath(t *testing.T) {
	buf := captureLogs(t)
	h := logRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing", nil))

	logged := buf.String()
	if !strings.Contains(logged, `"status":404`) || !strings.Contains(logged, `"level":"INFO"`) {
		t.Fatalf("client error not logged at info: %s", logged)
	}
}

func TestStatusRecorderHonorsFirstStatus(t *testing.T) {
	dst := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: dst, status: http.StatusOK}
	rec.WriteHeader(http.StatusCreated)
	rec.WriteHeader(http.StatusInternalServerError)
	if rec.status != http.StatusCreated {
		t.Fatalf("recorded status = %d, want first status %d", rec.status, http.StatusCreated)
	}
	if dst.Code != http.StatusCreated {
		t.Fatalf("underlying status = %d, want %d", dst.Code, http.StatusCreated)
	}
}

type hijackWriter struct {
	header http.Header
	conn   net.Conn
}

func (w *hijackWriter) Header() http.Header         { return w.header }
func (w *hijackWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *hijackWriter) WriteHeader(int)             {}
func (w *hijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(bufio.NewReader(w.conn), bufio.NewWriter(w.conn)), nil
}

func TestStatusRecorderHijackRecordsSwitchingProtocols(t *testing.T) {
	conn, peer := net.Pipe()
	t.Cleanup(func() {
		conn.Close()
		peer.Close()
	})
	rec := &statusRecorder{
		ResponseWriter: &hijackWriter{header: make(http.Header), conn: conn},
		status:         http.StatusOK,
	}

	got, _, err := rec.Hijack()
	if err != nil {
		t.Fatal(err)
	}
	if got != conn {
		t.Fatal("hijack did not return the underlying connection")
	}
	if rec.status != http.StatusSwitchingProtocols {
		t.Fatalf("recorded status = %d, want %d", rec.status, http.StatusSwitchingProtocols)
	}
}
