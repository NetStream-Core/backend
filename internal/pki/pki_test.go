package pki_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"network-monitor-backend/internal/pki"
)

func selfSignedCA(t *testing.T) *pki.CA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}
	return &pki.CA{Cert: cert, Key: key}
}

func csrPEM(t *testing.T, commonName string, ou ...string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate sensor key: %v", err)
	}
	template := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: commonName, OrganizationalUnit: ou},
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		t.Fatalf("create CSR: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func TestIssueSignsAValidCSRForTheSensorsOwnHostID(t *testing.T) {
	ca := selfSignedCA(t)
	csr, err := pki.ParseCSR(csrPEM(t, "sensor-1"))
	if err != nil {
		t.Fatalf("ParseCSR: %v", err)
	}

	certPEM, serial, expiresAt, err := pki.Issue(ca, csr, "sensor-1", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if serial == "" {
		t.Fatalf("expected a non-empty serial")
	}
	if !expiresAt.After(time.Now()) {
		t.Fatalf("expected expiresAt in the future")
	}

	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("Issue did not return a PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse issued cert: %v", err)
	}
	if cert.Subject.CommonName != "sensor-1" {
		t.Fatalf("expected CommonName sensor-1, got %q", cert.Subject.CommonName)
	}

	roots := x509.NewCertPool()
	roots.AddCert(ca.Cert)
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("issued certificate does not verify against the CA: %v", err)
	}
}

func TestIssueRejectsACSRWhoseCommonNameDoesNotMatchTheHostID(t *testing.T) {
	ca := selfSignedCA(t)
	csr, err := pki.ParseCSR(csrPEM(t, "sensor-1"))
	if err != nil {
		t.Fatalf("ParseCSR: %v", err)
	}

	if _, _, _, err := pki.Issue(ca, csr, "sensor-2", time.Hour); err != pki.ErrCSRCommonNameMismatch {
		t.Fatalf("expected ErrCSRCommonNameMismatch, got %v", err)
	}
}

func TestIssueRejectsACSRClaimingAnOperatorOU(t *testing.T) {
	ca := selfSignedCA(t)
	csr, err := pki.ParseCSR(csrPEM(t, "sensor-1", "operator"))
	if err != nil {
		t.Fatalf("ParseCSR: %v", err)
	}

	if _, _, _, err := pki.Issue(ca, csr, "sensor-1", time.Hour); err != pki.ErrCSROperatorOU {
		t.Fatalf("expected ErrCSROperatorOU, got %v", err)
	}
}

func TestIssueRejectsACSRClaimingAReadOnlyOperatorOU(t *testing.T) {
	ca := selfSignedCA(t)
	csr, err := pki.ParseCSR(csrPEM(t, "sensor-1", "operator-readonly"))
	if err != nil {
		t.Fatalf("ParseCSR: %v", err)
	}

	if _, _, _, err := pki.Issue(ca, csr, "sensor-1", time.Hour); err != pki.ErrCSROperatorOU {
		t.Fatalf("expected ErrCSROperatorOU, got %v", err)
	}
}

func TestParseCSRRejectsGarbage(t *testing.T) {
	if _, err := pki.ParseCSR([]byte("not a csr")); err == nil {
		t.Fatalf("expected an error for garbage input")
	}
}
