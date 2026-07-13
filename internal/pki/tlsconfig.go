package pki

import (
	"crypto/tls"
	"os"
	"sync"
	"time"
)

// certReloader serves the leaf key pair from disk, re-reading it when the
// certificate file changes. This is what makes the daily rotation (Ensure)
// take effect on a long-running daemon: http.ListenAndServeTLS loads the pair
// exactly once, so without reloading the listener would keep serving a leaf
// past its expiry even though a fresh one sits on disk.
type certReloader struct {
	certPath, keyPath string

	mu      sync.Mutex
	cert    *tls.Certificate
	modTime time.Time
}

func (r *certReloader) get() (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	info, err := os.Stat(r.certPath)
	if err == nil && (r.cert == nil || !info.ModTime().Equal(r.modTime)) {
		if cert, lerr := tls.LoadX509KeyPair(r.certPath, r.keyPath); lerr == nil {
			r.cert, r.modTime = &cert, info.ModTime()
		} else {
			err = lerr
		}
	}
	if r.cert == nil {
		return nil, err
	}
	// Load failures with a cached pair keep serving the cache: writePair
	// renames cert and key separately, so a read between the two renames sees
	// a mismatched pair; the next handshake picks up the completed rotation.
	return r.cert, nil
}

// TLSConfig returns a tls.Config that always serves the current leaf from
// disk, picking up rotations without a listener restart.
func (p *PKI) TLSConfig() *tls.Config {
	r := &certReloader{certPath: p.LeafCertPath(), keyPath: p.LeafKeyPath()}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return r.get()
		},
	}
}
