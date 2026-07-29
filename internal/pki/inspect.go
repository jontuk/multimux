package pki

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

type CAInfo struct {
	Subject             string   `json:"subject"`
	PermittedDNSDomains []string `json:"permittedDNSDomains"`
	Expires             string   `json:"expires"`
	SHA256Fingerprint   string   `json:"sha256Fingerprint"`
}

func InspectCA(raw []byte) (CAInfo, error) {
	trimmed := bytes.TrimSpace(raw)
	if !bytes.HasPrefix(trimmed, []byte("-----BEGIN CERTIFICATE-----")) {
		return CAInfo{}, errors.New("pki: CA must be one PEM CERTIFICATE block")
	}
	block, rest := pem.Decode(trimmed)
	if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
		return CAInfo{}, errors.New("pki: CA must be one PEM CERTIFICATE block")
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		return CAInfo{}, errors.New("pki: CA PEM contains additional data")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return CAInfo{}, fmt.Errorf("pki: parse CA certificate: %w", err)
	}
	if !cert.IsCA {
		return CAInfo{}, errors.New("pki: certificate is not a CA")
	}
	sum := sha256.Sum256(cert.Raw)
	plain := strings.ToUpper(hex.EncodeToString(sum[:]))
	pairs := make([]string, 0, len(plain)/2)
	for len(plain) > 0 {
		pairs, plain = append(pairs, plain[:2]), plain[2:]
	}
	return CAInfo{
		Subject:             cert.Subject.CommonName,
		PermittedDNSDomains: append([]string(nil), cert.PermittedDNSDomains...),
		Expires:             cert.NotAfter.UTC().Format(time.RFC3339),
		SHA256Fingerprint:   strings.Join(pairs, ":"),
	}, nil
}

func FormatCAInfo(info CAInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CA: %s\n", info.Subject)
	if len(info.PermittedDNSDomains) > 0 {
		fmt.Fprintf(&b, "  constrained to: %s\n", strings.Join(info.PermittedDNSDomains, ", "))
	}
	fmt.Fprintf(&b, "  expires: %s\n", info.Expires)
	fmt.Fprintf(&b, "  SHA-256: %s\n", info.SHA256Fingerprint)
	return b.String()
}
