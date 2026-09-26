package main

import (
	"network-monitor-backend/internal/api"
	"network-monitor-backend/internal/config"
	"network-monitor-backend/internal/logger"

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

	r := api.New()
	logger.Logger.Info("Starting HTTP server on :" + cfg.Server.Port)
	if err := r.Router.Run(":" + cfg.Server.Port); err != nil {
		logger.Logger.Fatal("HTTP server failed", zap.Error(err))
	}
}
