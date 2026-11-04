package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// Vite puts a content hash in every filename under assets/, so a URL there
	// never changes meaning.
	cacheImmutable = "public, max-age=31536000, immutable"
	// Everything else keeps its URL across builds. Browsers may store it but must
	// revalidate, which is what lets a new daemon's index.html reach them.
	cacheRevalidate = "no-cache"
)

// staticEncodings lists the precompressed siblings web/precompress.ts writes
// next to each compressible file, best compression first.
var staticEncodings = []struct{ name, suffix string }{
	{"br", ".br"},
	{"gzip", ".gz"},
}

// staticVariant is one representation of a file. embed.FS has no modification
// times, which leaves http.ServeContent nothing to revalidate against, so each
// variant carries a content-derived ETag instead.
type staticVariant struct {
	path, encoding, etag string
}

type staticFile struct {
	plain      staticVariant
	compressed []staticVariant // in staticEncodings order
}

// negotiate picks the best variant the client accepts.
func (f staticFile) negotiate(acceptEncoding string) staticVariant {
	for _, v := range f.compressed {
		if acceptsEncoding(acceptEncoding, v.encoding) {
			return v
		}
	}
	return f.plain
}

// acceptsEncoding applies the RFC 9110 Accept-Encoding rules: a name that is
// listed decides for itself (q=0 refuses it), otherwise "*" decides, otherwise
// the encoding is not acceptable.
func acceptsEncoding(header, encoding string) bool {
	wildcard := false
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(part, ";")
		name = strings.TrimSpace(name)
		if strings.EqualFold(name, encoding) {
			return !refusedByQuality(params)
		}
		if name == "*" {
			wildcard = !refusedByQuality(params)
		}
	}
	return wildcard
}

func refusedByQuality(params string) bool {
	value, found := strings.CutPrefix(strings.ToLower(strings.TrimSpace(params)), "q=")
	if !found {
		return false
	}
	q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return err == nil && q == 0
}

// loadStaticFiles indexes webFS by URL path. Compressed siblings are attached
// to the file they compress rather than listed, so they cannot be requested
// directly and arrive with no Content-Encoding. Hashing runs once per process:
// the FS is embedded, so its contents cannot change.
func loadStaticFiles(webFS fs.FS) map[string]staticFile {
	etags := map[string]string{}
	err := fs.WalkDir(webFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := fs.ReadFile(webFS, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		etags[path] = `"` + hex.EncodeToString(sum[:8]) + `"`
		return nil
	})
	if err != nil {
		slog.Error("reading embedded web assets", "err", err)
	}

	isSibling := func(path string) bool {
		for _, enc := range staticEncodings {
			if base, found := strings.CutSuffix(path, enc.suffix); found {
				if _, hasBase := etags[base]; hasBase {
					return true
				}
			}
		}
		return false
	}
	files := make(map[string]staticFile, len(etags))
	for path, etag := range etags {
		if isSibling(path) {
			continue
		}
		file := staticFile{plain: staticVariant{path: path, etag: etag}}
		for _, enc := range staticEncodings {
			if siblingETag, ok := etags[path+enc.suffix]; ok {
				file.compressed = append(file.compressed, staticVariant{
					path: path + enc.suffix, encoding: enc.name, etag: siblingETag,
				})
			}
		}
		files[path] = file
	}
	return files
}

// staticHandler serves the embedded SPA with index.html fallback for client
// routes (any path that isn't a file).
func (s *Server) staticHandler() http.Handler {
	files := loadStaticFiles(s.cfg.WebFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		file, found := files[path]
		cacheControl := cacheRevalidate
		if found && strings.HasPrefix(path, "assets/") {
			cacheControl = cacheImmutable
		}
		if !found {
			path = "index.html"
			if file, found = files[path]; !found {
				http.Error(w, "web assets missing — build web/ first", http.StatusNotImplemented)
				return
			}
		}

		variant := file.negotiate(r.Header.Get("Accept-Encoding"))
		content, err := fs.ReadFile(s.cfg.WebFS, variant.path)
		if err != nil {
			http.Error(w, "web asset unreadable", http.StatusInternalServerError)
			return
		}
		header := w.Header()
		header.Set("Cache-Control", cacheControl)
		header.Set("ETag", variant.etag)
		if len(file.compressed) > 0 {
			header.Set("Vary", "Accept-Encoding")
		}
		if variant.encoding != "" {
			header.Set("Content-Encoding", variant.encoding)
		}
		// path, not variant.path: the Content-Type comes from this name, and it
		// has to describe what the body decodes to.
		http.ServeContent(w, r, path, time.Time{}, bytes.NewReader(content))
	})
}
