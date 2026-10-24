// Package pki issues (and records revocation of) the client certificates
// sensors authenticate with over mTLS (see internal/api/auth.go). Until
// now backend only ever verified a certificate someone else issued — "the
// backend already checks client certificates, but doesn't issue them" was
// an explicit gap in the architecture doc's "Дальше" section. This is the
// minimum that closes it: an operator signs a sensor-submitted CSR with a
// backend-held CA key, and can mark a certificate revoked in Postgres.
//
// What this deliberately doesn't do: enforce revocation at the TLS
// handshake. Go's tls.Config has no CRL/OCSP hook wired into
// RequireAndVerifyClientCert — a revoked certificate stays usable until
// it expires unless something is added to check sensor_certificates on
// every request (or a real CRL/OCSP responder). That's real production
// PKI, not what a thesis-scope control plane needs; revocation here
// records the fact for an operator to see and act on manually (e.g.
// updating the sensor's config to quarantine it), not an enforced control.
package pki

import (
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"
)

var (
	ErrCSRCommonNameMismatch = errors.New("CSR CommonName must equal the sensor's host_id")
	ErrCSROperatorOU         = errors.New("a sensor certificate may not carry an operator OrganizationalUnit")
)

// CA holds the backend's own signing certificate and key, loaded once at
// startup from CA_CERT_FILE/CA_KEY_FILE — see cmd/main.go.
type CA struct {
	Cert *x509.Certificate
	Key  any // crypto.Signer; kept as `any` to avoid importing every key type backend never generates itself
}

// ParseCSR decodes a PEM-encoded PKCS#10 certificate request and checks
// its signature is self-consistent (the submitter actually holds the
// private key for the public key in the request) before anything in this
// package trusts a single field of it.
func ParseCSR(pemBytes []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		return nil, errors.New("not a PEM-encoded CERTIFICATE REQUEST")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("CSR signature does not match its own public key: %w", err)
	}
	return csr, nil
}

// Issue signs csr as a client-auth certificate for hostID, valid for
// validity from now. The CSR's own CommonName must equal hostID exactly —
// a sensor doesn't get to request a certificate for a different
// identity — and its OrganizationalUnit must not claim "operator" or
// "operator-readonly": certificate issuance for a sensor can't be used to
// mint an operator credential, no matter what the CSR asks for.
func Issue(
	ca *CA, csr *x509.CertificateRequest, hostID string, validity time.Duration,
) (certPEM []byte, serialHex string, expiresAt time.Time, err error) {
	if csr.Subject.CommonName != hostID {
		return nil, "", time.Time{}, ErrCSRCommonNameMismatch
	}
	if slices.Contains(csr.Subject.OrganizationalUnit, "operator") ||
		slices.Contains(csr.Subject.OrganizationalUnit, "operator-readonly") {
		return nil, "", time.Time{}, ErrCSROperatorOU
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, "", time.Time{}, fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now()
	notAfter := now.Add(validity)
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: hostID},
		NotBefore:    now,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, ca.Cert, csr.PublicKey, ca.Key)
	if err != nil {
		return nil, "", time.Time{}, fmt.Errorf("sign certificate: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	return certPEM, serial.Text(16), notAfter, nil
}
