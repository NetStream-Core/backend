package main

import (
	"network-monitor-backend/internal/api"
	"network-monitor-backend/internal/config"
	"network-monitor-backend/internal/logger"
	"network-monitor-backend/internal/storage/database"

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

	dsn := cfg.Database.BuildDSN()
	logger.Logger.Info("Database DSN configured")
	db, err := database.New(dsn)
	if err != nil {
		logger.Logger.Fatal("Failed to initialize database", zap.Error(err))
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Logger.Error("Database could not be closed", zap.Error(err))
		}
	}()

	r := api.New()
	go func() {
		logger.Logger.Info("Starting HTTP server on :" + cfg.Server.Port)
		if err := r.Router.Run(":" + cfg.Server.Port); err != nil {
			logger.Logger.Fatal("HTTP server failed", zap.Error(err))
		}
	}()

	logger.Logger.Info("Starting gRPC server on :50051")
	if err := api.RunGRPCServer(db, ":50051"); err != nil {
		logger.Logger.Fatal("gRPC server failed", zap.Error(err))
	}
}
