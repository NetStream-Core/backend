package api

import (
	"errors"
	"io"
	"net/http"

	"network-monitor-backend/internal/logger"
	"network-monitor-backend/internal/pki"
	"network-monitor-backend/internal/sensorcerts"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const maxCSRBytes = 1 << 16 // 64 KiB — a PKCS#10 CSR is a few hundred bytes to a few KiB; this is generous headroom, not a real limit

// IssueSensorCertificate signs a CSR (PEM, request body) the operator
// submits on a sensor's behalf and records the issued certificate.
// Operator-only (requireWriteOperator): the sensor itself never has a
// certificate to call this with yet — enrollment is operator-mediated,
// not self-service, which is deliberate for a thesis-scope PKI (no
// bootstrap-token enrollment flow exists).
func (a *API) IssueSensorCertificate(ctx *gin.Context) {
	if a.ca == nil {
		ctx.JSON(http.StatusServiceUnavailable, gin.H{"error": "certificate issuance is not configured (CA_CERT_FILE/CA_KEY_FILE)"})
		return
	}
	hostID := ctx.Param("host_id")

	body, err := io.ReadAll(io.LimitReader(ctx.Request.Body, maxCSRBytes+1))
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}
	if len(body) > maxCSRBytes {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "CSR too large"})
		return
	}

	csr, err := pki.ParseCSR(body)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	certPEM, serial, expiresAt, err := pki.Issue(a.ca, csr, hostID, a.certValidity)
	if errors.Is(err, pki.ErrCSRCommonNameMismatch) || errors.Is(err, pki.ErrCSROperatorOU) {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		logger.Logger.Error("Failed to sign a sensor certificate", zap.Error(err), zap.String("host_id", hostID))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	record, err := a.sensorCerts.Record(ctx.Request.Context(), hostID, serial, expiresAt)
	if err != nil {
		logger.Logger.Error("Failed to record an issued certificate", zap.Error(err), zap.String("host_id", hostID))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.Header("Content-Type", "application/x-pem-file")
	ctx.Writer.WriteHeader(http.StatusOK)
	_, _ = ctx.Writer.Write(certPEM)
	logger.Logger.Info("Issued a sensor certificate",
		zap.String("host_id", hostID), zap.String("serial", record.Serial), zap.Time("expires_at", record.ExpiresAt))
}

func (a *API) ListSensorCertificates(ctx *gin.Context) {
	certs, err := a.sensorCerts.ListForSensor(ctx.Request.Context(), ctx.Param("host_id"))
	if err != nil {
		logger.Logger.Error("Failed to list sensor certificates", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, certs)
}

func (a *API) RevokeSensorCertificate(ctx *gin.Context) {
	cert, err := a.sensorCerts.RevokeLatest(ctx.Request.Context(), ctx.Param("host_id"))
	if errors.Is(err, sensorcerts.ErrNotFound) {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "no active certificate to revoke for this sensor"})
		return
	}
	if err != nil {
		logger.Logger.Error("Failed to revoke a sensor certificate", zap.Error(err))
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, cert)
}
