package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	immutableAsset = "public, max-age=31536000, immutable"
	revalidate     = "no-cache"
)

func staticTestServer(t *testing.T) *Server {
	t.Helper()
	cfg, _, _ := newTestServerCfg(t, true)
	cfg.WebFS = fstest.MapFS{
		"index.html":       {Data: []byte("<html>multimux</html>")},
		"index.html.gz":    {Data: []byte("index-gz")},
		"icon-192.png":     {Data: []byte("png")},
		"assets/app.js":    {Data: []byte("//plain js")},
		"assets/app.js.br": {Data: []byte("js-br")},
		"assets/app.js.gz": {Data: []byte("js-gz")},
		"assets/logo.png":  {Data: []byte("png")},
	}
	return New(cfg)
}

func getStatic(s *Server, path string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestStaticCachePolicy(t *testing.T) {
	s := staticTestServer(t)
	for _, tc := range []struct{ path, want string }{
		{"/assets/app.js", immutableAsset},
		{"/assets/logo.png", immutableAsset},
		{"/", revalidate},
		{"/index.html", revalidate},
		{"/settings", revalidate},
		{"/icon-192.png", revalidate},
		// Falls back to index.html: marking this immutable would pin the app
		// shell under an asset URL for a year.
		{"/assets/gone.js", revalidate},
	} {
		w := getStatic(s, tc.path, nil)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d", tc.path, w.Code)
		}
		if got := w.Header().Get("Cache-Control"); got != tc.want {
			t.Errorf("GET %s Cache-Control = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// no-cache only saves anything if the browser can revalidate.
func TestStaticRevalidatesWith304(t *testing.T) {
	s := staticTestServer(t)
	for _, path := range []string{"/", "/settings", "/assets/app.js"} {
		first := getStatic(s, path, nil)
		etag := first.Header().Get("ETag")
		if etag == "" {
			t.Fatalf("GET %s has no ETag", path)
		}
		second := getStatic(s, path, map[string]string{"If-None-Match": etag})
		if second.Code != http.StatusNotModified {
			t.Errorf("GET %s If-None-Match = %d, want 304", path, second.Code)
		}
		if second.Body.Len() != 0 {
			t.Errorf("GET %s 304 carried a body", path)
		}
		stale := getStatic(s, path, map[string]string{"If-None-Match": `"stale"`})
		if stale.Code != http.StatusOK {
			t.Errorf("GET %s stale If-None-Match = %d, want 200", path, stale.Code)
		}
	}
}

func TestStaticServesPrecompressed(t *testing.T) {
	s := staticTestServer(t)
	for _, tc := range []struct{ accept, wantEncoding, wantBody string }{
		{"", "", "//plain js"},
		{"identity", "", "//plain js"},
		{"gzip", "gzip", "js-gz"},
		{"br", "br", "js-br"},
		{"gzip, deflate, br, zstd", "br", "js-br"},
		{"GZIP", "gzip", "js-gz"},
		{"br;q=0, gzip", "gzip", "js-gz"},
		{"br; q=0.0, gzip;q=0", "", "//plain js"},
		{"gzip;q=0.5", "gzip", "js-gz"},
		{"*", "br", "js-br"},
		{"*;q=0", "", "//plain js"},
		{"*, br;q=0", "gzip", "js-gz"},
		{"zstd", "", "//plain js"},
	} {
		w := getStatic(s, "/assets/app.js", map[string]string{"Accept-Encoding": tc.accept})
		if got := w.Header().Get("Content-Encoding"); got != tc.wantEncoding {
			t.Errorf("Accept-Encoding %q: Content-Encoding = %q, want %q", tc.accept, got, tc.wantEncoding)
		}
		if got := w.Body.String(); got != tc.wantBody {
			t.Errorf("Accept-Encoding %q: body = %q, want %q", tc.accept, got, tc.wantBody)
		}
		// The type names what the body decodes to; sniffing a compressed body or
		// taking it from the .br name would break the module load.
		if got := w.Header().Get("Content-Type"); !strings.Contains(got, "javascript") {
			t.Errorf("Accept-Encoding %q: Content-Type = %q", tc.accept, got)
		}
		if got := w.Header().Get("Vary"); got != "Accept-Encoding" {
			t.Errorf("Accept-Encoding %q: Vary = %q", tc.accept, got)
		}
	}
}

// One URL, several bodies: sharing an ETag would let a cache answer one
// encoding's revalidation with another's bytes.
func TestStaticETagDiffersByEncoding(t *testing.T) {
	s := staticTestServer(t)
	seen := map[string]string{}
	for _, enc := range []string{"", "gzip", "br"} {
		etag := getStatic(s, "/assets/app.js", map[string]string{"Accept-Encoding": enc}).Header().Get("ETag")
		if other, dup := seen[etag]; dup {
			t.Fatalf("encodings %q and %q share ETag %s", other, enc, etag)
		}
		seen[etag] = enc
	}
}

func TestStaticSPAFallbackIsCompressedToo(t *testing.T) {
	s := staticTestServer(t)
	w := getStatic(s, "/settings", map[string]string{"Accept-Encoding": "gzip"})
	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q", got)
	}
}

// The siblings are an internal detail. Answering for them directly would serve
// compressed bytes with no Content-Encoding.
func TestStaticDoesNotExposeCompressedSiblings(t *testing.T) {
	s := staticTestServer(t)
	w := getStatic(s, "/assets/app.js.br", nil)
	if got := w.Body.String(); got == "js-br" {
		t.Fatalf("sibling served directly: %q", got)
	}
}

func TestStaticFileWithoutSiblingOmitsVary(t *testing.T) {
	s := staticTestServer(t)
	w := getStatic(s, "/assets/logo.png", map[string]string{"Accept-Encoding": "br, gzip"})
	if got := w.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	if got := w.Header().Get("Vary"); got != "" {
		t.Fatalf("Vary = %q", got)
	}
}

func TestStaticMissingAssetsAnswer501(t *testing.T) {
	cfg, _, _ := newTestServerCfg(t, true)
	cfg.WebFS = fstest.MapFS{".gitkeep": {Data: []byte{}}}
	s := New(cfg)
	if w := getStatic(s, "/", nil); w.Code != http.StatusNotImplemented {
		t.Fatalf("GET / with no build = %d, want 501", w.Code)
	}
}
