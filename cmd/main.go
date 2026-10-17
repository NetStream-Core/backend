package main

import (
	"context"
	"errors"

	"network-monitor-backend/internal/api"
	"network-monitor-backend/internal/config"
	"network-monitor-backend/internal/logger"
	"network-monitor-backend/internal/models"
	"network-monitor-backend/internal/sensors"
	"network-monitor-backend/internal/storage/objectstore"
	"network-monitor-backend/internal/storage/postgres"

	"go.uber.org/zap"
)

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

	r := api.New(sensors.NewService(db.Pool), models.NewService(db.Pool), objectStore)

	switch {
	case cfg.Server.TLSCertFile != "" && cfg.Server.TLSKeyFile != "":
		logger.Logger.Info("Starting HTTPS server on :" + cfg.Server.Port)
		err = r.Router.RunTLS(":"+cfg.Server.Port, cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
	case cfg.Server.TLSCertFile == "" && cfg.Server.TLSKeyFile == "":
		logger.Logger.Info("Starting HTTP server on :" + cfg.Server.Port)
		err = r.Router.Run(":" + cfg.Server.Port)
	default:
		err = errors.New("TLS_CERT_FILE and TLS_KEY_FILE must both be set, or neither")
	}
	if err != nil {
		logger.Logger.Fatal("HTTP server failed", zap.Error(err))
	}
}
