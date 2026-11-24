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
//
// content is the file's bytes, read once at startup. Going back to the FS per
// request would cost a copy of the whole asset each time — embed.FS.ReadFile
// returns a fresh slice — including for a 304 that sends none of it.
type staticVariant struct {
	encoding, etag string
	content        []byte
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
// directly and arrive with no Content-Encoding. Reading and hashing run once
// per process: the FS is embedded, so its contents cannot change.
func loadStaticFiles(webFS fs.FS) map[string]staticFile {
	variants := map[string]staticVariant{}
	err := fs.WalkDir(webFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := fs.ReadFile(webFS, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		variants[path] = staticVariant{etag: `"` + hex.EncodeToString(sum[:8]) + `"`, content: content}
		return nil
	})
	if err != nil {
		slog.Error("reading embedded web assets", "err", err)
	}

	isSibling := func(path string) bool {
		for _, enc := range staticEncodings {
			if base, found := strings.CutSuffix(path, enc.suffix); found {
				if _, hasBase := variants[base]; hasBase {
					return true
				}
			}
		}
		return false
	}
	files := make(map[string]staticFile, len(variants))
	for path, plain := range variants {
		if isSibling(path) {
			continue
		}
		file := staticFile{plain: plain}
		for _, enc := range staticEncodings {
			if sibling, ok := variants[path+enc.suffix]; ok {
				sibling.encoding = enc.name
				file.compressed = append(file.compressed, sibling)
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
		header := w.Header()
		header.Set("Cache-Control", cacheControl)
		header.Set("ETag", variant.etag)
		if len(file.compressed) > 0 {
			header.Set("Vary", "Accept-Encoding")
		}
		if variant.encoding != "" {
			header.Set("Content-Encoding", variant.encoding)
		}
		// path is the plain file's name even when a compressed variant is sent:
		// the Content-Type comes from it, and it has to describe what the body
		// decodes to.
		http.ServeContent(w, r, path, time.Time{}, bytes.NewReader(variant.content))
	})
}
