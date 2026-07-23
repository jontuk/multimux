// Package pki generates multimux's local TLS material: a long-lived CA that is
// X.509 name-constrained to the daemon's hostnames (so trusting it cannot be
// abused to MITM other sites — relevant on managed/work machines) and a
// short-lived leaf certificate that auto-rotates.
package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	caValidity   = 10 * 365 * 24 * time.Hour
	leafValidity = 90 * 24 * time.Hour
	// leafRotateUnder must stay <= caRenewUnder: a leaf is clamped to its
	// issuer's NotAfter (see createLeaf), so if the CA were allowed to age past
	// the leaf rotation window the clamped leaf would look due for rotation on
	// every check and be rewritten daily. Renewing the CA first means a clamped
	// leaf always has at least caRenewUnder left, i.e. it is never in the
	// rotation window.
	leafRotateUnder = 30 * 24 * time.Hour
	caRenewUnder    = 30 * 24 * time.Hour
)

// now is a seam for tests that need to age the CA. Production always uses the
// real clock.
var now = time.Now

// CARegen says whether Ensure recreated the CA — and if so why, since the user
// has to re-run `multimux ca trust` and deserves to be told the actual reason.
type CARegen int

const (
	CARegenNone CARegen = iota
	// CARegenHostnames: name constraints are baked in at creation, so a changed
	// hostname set needs a new CA.
	CARegenHostnames
	// CARegenExpiring: the old CA had expired, or was inside caRenewUnder of it.
	CARegenExpiring
)

func (r CARegen) String() string {
	switch r {
	case CARegenHostnames:
		return "hostname set changed"
	case CARegenExpiring:
		return "previous CA expired or was about to"
	default:
		return "none"
	}
}

type PKI struct {
	dir string
}

func New(dir string) *PKI { return &PKI{dir: dir} }

func (p *PKI) CACertPath() string   { return filepath.Join(p.dir, "ca.pem") }
func (p *PKI) caKeyPath() string    { return filepath.Join(p.dir, "ca.key") }
func (p *PKI) LeafCertPath() string { return filepath.Join(p.dir, "cert.pem") }
func (p *PKI) LeafKeyPath() string  { return filepath.Join(p.dir, "key.pem") }

// Ensure makes CA and leaf valid for hostnames. A non-None return means the CA
// itself was recreated and the caller should tell the user to re-run
// `multimux ca trust`, quoting the reason. Creating the CA from scratch (first
// run) is not a regeneration: there is no previous trust decision to redo.
func (p *PKI) Ensure(hostnames []string) (regenerated CARegen, err error) {
	if len(hostnames) == 0 {
		return CARegenNone, errors.New("pki: no hostnames configured")
	}
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return CARegenNone, err
	}
	caCert, caKey, err := p.loadCA()
	switch {
	case err == nil:
		switch {
		case !slices.Equal(caCert.PermittedDNSDomains, hostnames):
			regenerated = CARegenHostnames
		case !now().Before(caCert.NotAfter.Add(-caRenewUnder)):
			// Renew ahead of the cliff: an expired CA breaks every browser at
			// once, and re-trusting it is a manual step on every device.
			regenerated = CARegenExpiring
		}
	case !errors.Is(err, os.ErrNotExist):
		return CARegenNone, err
	}
	if err != nil || regenerated != CARegenNone {
		// err here is only ErrNotExist: no CA on disk yet.
		caCert, caKey, err = p.createCA(hostnames)
		if err != nil {
			return regenerated, err
		}
	}
	// A regenerated CA leaves the old leaf unable to chain, which
	// leafNeedsRotation catches, so the leaf follows the CA automatically.
	if p.leafNeedsRotation(hostnames) {
		if err := p.createLeaf(caCert, caKey, hostnames); err != nil {
			return regenerated, err
		}
	}
	return regenerated, nil
}

func (p *PKI) loadCA() (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := os.ReadFile(p.CACertPath())
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := os.ReadFile(p.caKeyPath())
	if err != nil {
		return nil, nil, err
	}
	cert, err := parseCertPEM(certPEM)
	if err != nil {
		return nil, nil, err
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, nil, errors.New("pki: bad CA key PEM")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	return cert, key, err
}

func (p *PKI) createCA(hostnames []string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := randSerial()
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: fmt.Sprintf("multimux local CA (%s)", hostnames[0])},
		NotBefore:             now().Add(-time.Hour),
		NotAfter:              now().Add(caValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		// The load-bearing part: trusting this CA only vouches for these
		// exact hostnames (and their subdomains), nothing else.
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         hostnames,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	if err := p.writePair(p.CACertPath(), p.caKeyPath(), der, key); err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	return cert, key, err
}

func (p *PKI) leafNeedsRotation(hostnames []string) bool {
	raw, err := os.ReadFile(p.LeafCertPath())
	if err != nil {
		return true
	}
	cert, err := parseCertPEM(raw)
	if err != nil {
		return true
	}
	if cert.NotAfter.Sub(now()) < leafRotateUnder {
		return true
	}
	if !slices.Equal(cert.DNSNames, hostnames) {
		return true
	}
	// The cert and key land on disk as two separate renames, so a crash in
	// between leaves generation N's cert next to generation N-1's key: valid,
	// chaining and unexpired, but unusable. Re-checking the pairing here is what
	// heals it. tls.LoadX509KeyPair is the same call the serving path
	// (certReloader.get) makes, so anything it rejects — missing key, corrupt
	// PEM, wrong key type, mismatched public key — is worth a fresh leaf.
	if _, err := tls.LoadX509KeyPair(p.LeafCertPath(), p.LeafKeyPath()); err != nil {
		return true
	}
	// Verify the leaf still chains to the current CA (covers CA regeneration).
	caPEM, err := os.ReadFile(p.CACertPath())
	if err != nil {
		return true
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return true
	}
	_, err = cert.Verify(x509.VerifyOptions{Roots: roots})
	return err != nil
}

func (p *PKI) createLeaf(caCert *x509.Certificate, caKey *ecdsa.PrivateKey, hostnames []string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := randSerial()
	if err != nil {
		return err
	}
	// A leaf outliving its issuer is a cert clients reject anyway (the chain
	// fails once the CA expires), so cap it at the CA's own NotAfter. Ensure
	// renews the CA at caRenewUnder, which is >= leafRotateUnder, so a clamped
	// leaf still has enough life left not to look due for rotation — no daily
	// rewrite loop.
	notAfter := now().Add(leafValidity)
	if notAfter.After(caCert.NotAfter) {
		notAfter = caCert.NotAfter
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: hostnames[0]},
		NotBefore:    now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     hostnames,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	return p.writePair(p.LeafCertPath(), p.LeafKeyPath(), der, key)
}

func (p *PKI) writePair(certPath, keyPath string, certDER []byte, key *ecdsa.PrivateKey) error {
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	// Temp file + rename keeps each file individually whole, but the pair is
	// still two renames: a crash in between leaves a mismatched cert/key on
	// disk. leafNeedsRotation re-checks the pairing on the next Ensure and
	// regenerates, which is what makes that window recoverable.
	certDir := filepath.Dir(certPath)
	certTemp, err := os.CreateTemp(certDir, filepath.Base(certPath)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(certTemp.Name())
	if _, err := certTemp.Write(certPEM); err != nil {
		certTemp.Close()
		return err
	}
	certTemp.Close()
	if err := os.Chmod(certTemp.Name(), 0o644); err != nil {
		return err
	}

	keyDir := filepath.Dir(keyPath)
	keyTemp, err := os.CreateTemp(keyDir, filepath.Base(keyPath)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(keyTemp.Name())
	if _, err := keyTemp.Write(keyPEM); err != nil {
		keyTemp.Close()
		return err
	}
	keyTemp.Close()
	if err := os.Chmod(keyTemp.Name(), 0o600); err != nil {
		return err
	}

	if err := os.Rename(certTemp.Name(), certPath); err != nil {
		return err
	}
	return os.Rename(keyTemp.Name(), keyPath)
}

func parseCertPEM(raw []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("pki: no PEM block")
	}
	return x509.ParseCertificate(block.Bytes)
}

func randSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
}
