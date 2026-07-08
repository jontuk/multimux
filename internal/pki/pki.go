// Package pki generates multimux's local TLS material: a long-lived CA that is
// X.509 name-constrained to the daemon's hostnames (so trusting it cannot be
// abused to MITM other sites — relevant on managed/work machines) and a
// short-lived leaf certificate that auto-rotates.
package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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
	caValidity      = 10 * 365 * 24 * time.Hour
	leafValidity    = 90 * 24 * time.Hour
	leafRotateUnder = 30 * 24 * time.Hour
)

type PKI struct {
	dir string
}

func New(dir string) *PKI { return &PKI{dir: dir} }

func (p *PKI) CACertPath() string   { return filepath.Join(p.dir, "ca.pem") }
func (p *PKI) caKeyPath() string    { return filepath.Join(p.dir, "ca.key") }
func (p *PKI) LeafCertPath() string { return filepath.Join(p.dir, "cert.pem") }
func (p *PKI) LeafKeyPath() string  { return filepath.Join(p.dir, "key.pem") }

// Ensure makes CA and leaf valid for hostnames. regenerated=true means the CA
// itself was recreated (constraints changed) and the caller should tell the
// user to re-run `multimux ca trust`.
func (p *PKI) Ensure(hostnames []string) (regenerated bool, err error) {
	if len(hostnames) == 0 {
		return false, errors.New("pki: no hostnames configured")
	}
	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return false, err
	}
	caCert, caKey, err := p.loadCA()
	switch {
	case err == nil && !slices.Equal(caCert.PermittedDNSDomains, hostnames):
		// Name constraints are baked into the CA at creation; a changed
		// hostname set forces a new CA (and a fresh user trust decision).
		regenerated = true
		fallthrough
	case errors.Is(err, os.ErrNotExist):
		caCert, caKey, err = p.createCA(hostnames)
		if err != nil {
			return regenerated, err
		}
	case err != nil:
		return false, err
	}
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
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(caValidity),
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
	if time.Until(cert.NotAfter) < leafRotateUnder {
		return true
	}
	if !slices.Equal(cert.DNSNames, hostnames) {
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
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: hostnames[0]},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(leafValidity),
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
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return err
	}
	return os.WriteFile(keyPath, keyPEM, 0o600)
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
