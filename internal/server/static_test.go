package server

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

// A 304 still has to name the representation it validated, or a cache cannot
// tell which stored body it refreshed.
func TestStatic304KeepsValidatorsPerEncoding(t *testing.T) {
	s := staticTestServer(t)
	for _, enc := range []string{"", "gzip", "br"} {
		first := getStatic(s, "/assets/app.js", map[string]string{"Accept-Encoding": enc})
		etag := first.Header().Get("ETag")
		w := getStatic(s, "/assets/app.js", map[string]string{"Accept-Encoding": enc, "If-None-Match": etag})
		if w.Code != http.StatusNotModified {
			t.Fatalf("Accept-Encoding %q: status = %d, want 304", enc, w.Code)
		}
		if w.Body.Len() != 0 {
			t.Errorf("Accept-Encoding %q: 304 carried a body", enc)
		}
		for _, name := range []string{"ETag", "Cache-Control", "Vary"} {
			if got, want := w.Header().Get(name), first.Header().Get(name); got != want {
				t.Errorf("Accept-Encoding %q: 304 %s = %q, want %q", enc, name, got, want)
			}
		}
		// Another encoding's ETag validates nothing here.
		other := "gzip"
		if enc == "gzip" {
			other = "br"
		}
		otherETag := getStatic(s, "/assets/app.js", map[string]string{"Accept-Encoding": other}).Header().Get("ETag")
		w = getStatic(s, "/assets/app.js", map[string]string{"Accept-Encoding": enc, "If-None-Match": otherETag})
		if w.Code != http.StatusOK {
			t.Errorf("Accept-Encoding %q with the %q ETag = %d, want 200", enc, other, w.Code)
		}
	}
}

// Ranges count bytes of the representation sent, so a compressed variant is
// sliced as compressed bytes.
func TestStaticRange(t *testing.T) {
	s := staticTestServer(t)
	for _, tc := range []struct {
		accept, rng          string
		wantCode             int
		wantBody, wantCRange string
	}{
		{"", "bytes=2-5", http.StatusPartialContent, "plai", "bytes 2-5/10"},
		{"", "bytes=-2", http.StatusPartialContent, "js", "bytes 8-9/10"},
		{"gzip", "bytes=0-1", http.StatusPartialContent, "js", "bytes 0-1/5"},
		{"br", "bytes=3-", http.StatusPartialContent, "br", "bytes 3-4/5"},
		{"", "bytes=50-", http.StatusRequestedRangeNotSatisfiable, "", "bytes */10"},
	} {
		w := getStatic(s, "/assets/app.js", map[string]string{"Accept-Encoding": tc.accept, "Range": tc.rng})
		if w.Code != tc.wantCode {
			t.Errorf("%q Range %q = %d, want %d", tc.accept, tc.rng, w.Code, tc.wantCode)
			continue
		}
		if got := w.Header().Get("Content-Range"); got != tc.wantCRange {
			t.Errorf("%q Range %q: Content-Range = %q, want %q", tc.accept, tc.rng, got, tc.wantCRange)
		}
		if tc.wantCode != http.StatusPartialContent {
			continue
		}
		if got := w.Body.String(); got != tc.wantBody {
			t.Errorf("%q Range %q: body = %q, want %q", tc.accept, tc.rng, got, tc.wantBody)
		}
		wantEncoding := tc.accept
		if got := w.Header().Get("Content-Encoding"); got != wantEncoding {
			t.Errorf("%q Range %q: Content-Encoding = %q", tc.accept, tc.rng, got)
		}
	}
}

// http.ServeContent leaves Content-Length off an encoded response, so the
// expectation is "whatever GET sends" rather than a literal.
func TestStaticHead(t *testing.T) {
	s := staticTestServer(t)
	for _, accept := range []string{"", "br"} {
		get := getStatic(s, "/assets/app.js", map[string]string{"Accept-Encoding": accept})
		r := httptest.NewRequest(http.MethodHead, "/assets/app.js", nil)
		r.Header.Set("Accept-Encoding", accept)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("HEAD %q = %d", accept, w.Code)
		}
		if w.Body.Len() != 0 {
			t.Errorf("HEAD %q carried a body", accept)
		}
		for _, name := range []string{
			"ETag", "Cache-Control", "Vary", "Content-Encoding", "Content-Type", "Content-Length", "Accept-Ranges",
		} {
			if got, want := w.Header().Get(name), get.Header().Get(name); got != want {
				t.Errorf("HEAD %q %s = %q, GET sent %q", accept, name, got, want)
			}
		}
	}
	plain := getStatic(s, "/assets/app.js", nil)
	if got := plain.Header().Get("Content-Length"); got != "10" {
		t.Errorf("plain Content-Length = %q, want 10", got)
	}
}

// bareFS hides everything fstest.MapFS offers beyond fs.FS: no ReadFile, and
// files that can only Read, Stat and Close. It counts opens and closes.
type bareFS struct {
	fsys          fs.FS
	opens, closes atomic.Int64
}

type bareFile struct {
	fs.File
	closes *atomic.Int64
}

// Directories keep ReadDir, which fs.WalkDir needs.
type bareDir struct {
	fs.ReadDirFile
	closes *atomic.Int64
}

func (f bareFile) Close() error { f.closes.Add(1); return f.File.Close() }
func (d bareDir) Close() error  { d.closes.Add(1); return d.ReadDirFile.Close() }

func (b *bareFS) Open(name string) (fs.File, error) {
	f, err := b.fsys.Open(name)
	if err != nil {
		return nil, err
	}
	b.opens.Add(1)
	if dir, ok := f.(fs.ReadDirFile); ok {
		if info, err := f.Stat(); err == nil && info.IsDir() {
			return bareDir{dir, &b.closes}, nil
		}
	}
	return bareFile{f, &b.closes}, nil
}

// cfg.WebFS is only promised to be an fs.FS, whose files need not seek. The
// handler must serve every request shape from one, leak no file, and not go
// back to the FS per request: embed.FS.ReadFile copies the whole asset, which
// a 304 then throws away.
func TestStaticServesFromMinimalFSWithoutRereading(t *testing.T) {
	cfg, _, _ := newTestServerCfg(t, true)
	bare := &bareFS{fsys: fstest.MapFS{
		"index.html":       {Data: []byte("<html>multimux</html>")},
		"assets/app.js":    {Data: []byte("//plain js")},
		"assets/app.js.gz": {Data: []byte("js-gz")},
	}}
	cfg.WebFS = bare
	s := New(cfg)
	h := s.staticHandler()
	if opens, closes := bare.opens.Load(), bare.closes.Load(); opens == 0 || opens != closes {
		t.Fatalf("after indexing: %d opens, %d closes", opens, closes)
	}
	opened := bare.opens.Load()

	serve := func(headers map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	full := serve(map[string]string{"Accept-Encoding": "gzip"})
	if full.Code != http.StatusOK || full.Body.String() != "js-gz" || full.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("GET = %d %q (%s)", full.Code, full.Body, full.Header().Get("Content-Encoding"))
	}
	if w := serve(map[string]string{"Range": "bytes=2-5"}); w.Code != http.StatusPartialContent || w.Body.String() != "plai" {
		t.Fatalf("Range = %d %q", w.Code, w.Body)
	}
	if w := serve(map[string]string{"Accept-Encoding": "gzip", "If-None-Match": full.Header().Get("ETag")}); w.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match = %d, want 304", w.Code)
	}
	if got := bare.opens.Load(); got != opened {
		t.Fatalf("serving opened the FS %d more times", got-opened)
	}
}

// A revalidation should cost the same however large the asset is. Run with
// -benchmem: B/op must not track the 1 MiB body.
func BenchmarkStaticNotModified(b *testing.B) {
	big := bytes.Repeat([]byte("x"), 1<<20)
	s := &Server{cfg: Config{WebFS: fstest.MapFS{
		"index.html":    {Data: []byte("<html></html>")},
		"assets/app.js": {Data: big},
	}}}
	h := s.staticHandler()
	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	r := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	r.Header.Set("If-None-Match", first.Header().Get("ETag"))
	b.ReportAllocs()
	for b.Loop() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusNotModified {
			b.Fatalf("status = %d", w.Code)
		}
	}
}
