package server

import (
	"bytes"
	"net/http"

	"github.com/jontuk/multimux/internal/pki"
)

func (s *Server) currentCA(w http.ResponseWriter) ([]byte, pki.CAInfo, bool) {
	if s.cfg.ReadCA == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "CA unavailable"})
		return nil, pki.CAInfo{}, false
	}
	raw, err := s.cfg.ReadCA()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "CA unavailable"})
		return nil, pki.CAInfo{}, false
	}
	info, err := pki.InspectCA(raw)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "CA unavailable"})
		return nil, pki.CAInfo{}, false
	}
	return append(bytes.TrimSpace(raw), '\n'), info, true
}

func noStoreCertificateHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func (s *Server) handleCADownload(w http.ResponseWriter, _ *http.Request) {
	noStoreCertificateHeaders(w)
	raw, _, ok := s.currentCA(w)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", `attachment; filename="multimux-ca.crt"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Server) handleCAInfo(w http.ResponseWriter, _ *http.Request) {
	noStoreCertificateHeaders(w)
	_, info, ok := s.currentCA(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, info)
}
