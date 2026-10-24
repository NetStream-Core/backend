package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"network-monitor-backend/internal/api"
	"network-monitor-backend/internal/config"
	"network-monitor-backend/internal/detections"
	"network-monitor-backend/internal/incidents"
	"network-monitor-backend/internal/logger"
	"network-monitor-backend/internal/models"
	"network-monitor-backend/internal/pki"
	"network-monitor-backend/internal/sensorcerts"
	"network-monitor-backend/internal/sensors"
	"network-monitor-backend/internal/storage/objectstore"
	"network-monitor-backend/internal/storage/postgres"

	"go.uber.org/zap"
)

// buildClientCAPool reads TLSClientCAFile, if set, into a pool the server
// verifies client certificates against. Returns a nil pool (mTLS off) if
// the file isn't configured.
func buildClientCAPool(path string) (*x509.CertPool, error) {
	if path == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read TLS_CLIENT_CA_FILE: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("TLS_CLIENT_CA_FILE %s contains no valid certificates", path)
	}
	return pool, nil
}

// loadCA reads CACertFile/CAKeyFile, if both set, into the CA this
// process signs sensor certificates with. Returns a nil *pki.CA (issuance
// off) if neither is configured — same "empty means not configured"
// convention as buildClientCAPool and the Kafka/incidents feature.
func loadCA(certPath, keyPath string) (*pki.CA, error) {
	if certPath == "" && keyPath == "" {
		return nil, nil
	}
	if certPath == "" || keyPath == "" {
		return nil, errors.New("CA_CERT_FILE and CA_KEY_FILE must both be set, or neither")
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read CA_CERT_FILE: %w", err)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, fmt.Errorf("CA_CERT_FILE %s contains no PEM block", certPath)
	}
	caCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA_CERT_FILE: %w", err)
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read CA_KEY_FILE: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("CA_KEY_FILE %s contains no PEM block", keyPath)
	}
	caKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA_KEY_FILE (expected PKCS#8): %w", err)
	}

	return &pki.CA{Cert: caCert, Key: caKey}, nil
}

const shutdownTimeout = 10 * time.Second

func main() {
	if err := logger.New(); err != nil {
		panic("Failed to initialize logger")
	}
	if err := logger.Logger.Sync(); err != nil {
		logger.Logger.Warn("Buffer is not flushing")
	}
	logger.Logger.Info("Backend started")

	cfg, err := config.NewConfig()
	if err != nil {
		logger.Logger.Fatal("Failed to load configuration", zap.Error(err))
	}
	logger.Logger.Info("Loaded SERVER_PORT", zap.String("port", cfg.Server.Port))

	ctx := context.Background()
	db, err := postgres.New(ctx, cfg.Postgres.BuildDSN())
	if err != nil {
		logger.Logger.Fatal("Failed to connect to control-plane database", zap.Error(err))
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		logger.Logger.Fatal("Failed to migrate control-plane database", zap.Error(err))
	}

	objectStore, err := objectstore.New(
		ctx,
		cfg.ObjectStore.Endpoint,
		cfg.ObjectStore.AccessKey,
		cfg.ObjectStore.SecretKey,
		cfg.ObjectStore.Bucket,
		cfg.ObjectStore.UseSSL,
	)
	if err != nil {
		logger.Logger.Fatal("Failed to connect to object store", zap.Error(err))
	}

	clientCAs, err := buildClientCAPool(cfg.Server.TLSClientCAFile)
	if err != nil {
		logger.Logger.Fatal("Failed to load client CA", zap.Error(err))
	}
	mtlsEnabled := clientCAs != nil
	if mtlsEnabled && (cfg.Server.TLSCertFile == "" || cfg.Server.TLSKeyFile == "") {
		logger.Logger.Fatal("TLS_CLIENT_CA_FILE requires TLS_CERT_FILE and TLS_KEY_FILE to also be set")
	}
	if !mtlsEnabled {
		logger.Logger.Warn(
			"Starting without mTLS (TLS_CLIENT_CA_FILE not set) — the control API accepts " +
				"requests from anyone who can reach this port; do not expose it beyond " +
				"localhost or a trusted network",
		)
	}

	ca, err := loadCA(cfg.PKI.CACertFile, cfg.PKI.CAKeyFile)
	if err != nil {
		logger.Logger.Fatal("Failed to load issuance CA", zap.Error(err))
	}
	if ca == nil {
		logger.Logger.Info("CA_CERT_FILE/CA_KEY_FILE not set — sensor certificate issuance disabled")
	}
	certValidity := time.Duration(cfg.PKI.CertValidityDays) * 24 * time.Hour

	incidentService := incidents.NewService(db.Pool)

	if cfg.Kafka.BootstrapServers != "" {
		detectionsCtx, cancelDetections := context.WithCancel(context.Background())
		defer cancelDetections()
		go detections.Run(detectionsCtx, detections.Config{
			BootstrapServers: cfg.Kafka.BootstrapServers,
			DetectionsTopic:  cfg.Kafka.DetectionsTopic,
			AlertsTopic:      cfg.Kafka.AlertsTopic,
			ConsumerGroup:    cfg.Kafka.ConsumerGroup,
		}, incidentService)
	} else {
		logger.Logger.Info("KAFKA_BOOTSTRAP_SERVERS not set — incident correlator disabled")
	}

	r := api.New(
		sensors.NewService(db.Pool), models.NewService(db.Pool), incidentService,
		sensorcerts.NewService(db.Pool), objectStore, ca, certValidity, mtlsEnabled,
	)
	srv := &http.Server{Addr: ":" + cfg.Server.Port, Handler: r.Router}
	if mtlsEnabled {
		srv.TLSConfig = &tls.Config{
			ClientCAs:  clientCAs,
			ClientAuth: tls.RequireAndVerifyClientCert,
			MinVersion: tls.VersionTLS12,
		}
	}

	serveErr := make(chan error, 1)
	go func() {
		switch {
		case cfg.Server.TLSCertFile != "" && cfg.Server.TLSKeyFile != "":
			logger.Logger.Info("Starting HTTPS server on :" + cfg.Server.Port)
			serveErr <- srv.ListenAndServeTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
		case cfg.Server.TLSCertFile == "" && cfg.Server.TLSKeyFile == "":
			logger.Logger.Info("Starting HTTP server on :" + cfg.Server.Port)
			serveErr <- srv.ListenAndServe()
		default:
			serveErr <- errors.New("TLS_CERT_FILE and TLS_KEY_FILE must both be set, or neither")
		}
	}()

	signalCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Logger.Fatal("HTTP server failed", zap.Error(err))
		}
	case <-signalCtx.Done():
		stop()
		logger.Logger.Info("Shutdown signal received, draining in-flight requests")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Logger.Error("Graceful shutdown did not complete cleanly", zap.Error(err))
		}
	}

	logger.Logger.Info("Backend stopped")
}
