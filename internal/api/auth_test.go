package api

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func certRequest(commonName string, organizationalUnit ...string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	cert := &x509.Certificate{
		Subject: pkix.Name{CommonName: commonName, OrganizationalUnit: organizationalUnit},
	}
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	return req
}

func runWithMiddleware(req *http.Request, params gin.Params, handlers ...gin.HandlerFunc) int {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = req
	ctx.Params = params
	for _, h := range handlers {
		h(ctx)
		if ctx.IsAborted() {
			break
		}
	}
	return rec.Code
}

func TestRequireClientCertRejectsAPlainRequest(t *testing.T) {
	code := runWithMiddleware(httptest.NewRequest(http.MethodGet, "/", nil), nil, requireClientCert())
	if code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d", http.StatusUnauthorized, code)
	}
}

func TestRequireClientCertAcceptsAVerifiedCertificate(t *testing.T) {
	code := runWithMiddleware(certRequest("sensor-1"), nil, requireClientCert())
	if code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, code)
	}
}

func TestRequireOperatorRejectsASensorIdentity(t *testing.T) {
	code := runWithMiddleware(certRequest("sensor-1"), nil, requireClientCert(), requireOperator())
	if code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, code)
	}
}

func TestRequireOperatorAcceptsAnOperatorIdentity(t *testing.T) {
	code := runWithMiddleware(
		certRequest("alice", "operator"), nil, requireClientCert(), requireOperator(),
	)
	if code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, code)
	}
}

func TestRequireSelfOrOperatorRejectsASensorActingOnAnotherSensor(t *testing.T) {
	code := runWithMiddleware(
		certRequest("sensor-1"),
		gin.Params{{Key: "host_id", Value: "sensor-2"}},
		requireClientCert(), requireSelfOrOperator(),
	)
	if code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, code)
	}
}

func TestRequireSelfOrOperatorAcceptsASensorActingOnItself(t *testing.T) {
	code := runWithMiddleware(
		certRequest("sensor-1"),
		gin.Params{{Key: "host_id", Value: "sensor-1"}},
		requireClientCert(), requireSelfOrOperator(),
	)
	if code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, code)
	}
}

func TestRequireSelfOrOperatorAcceptsAnOperatorActingOnAnySensor(t *testing.T) {
	code := runWithMiddleware(
		certRequest("alice", "operator"),
		gin.Params{{Key: "host_id", Value: "sensor-2"}},
		requireClientCert(), requireSelfOrOperator(),
	)
	if code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, code)
	}
}
