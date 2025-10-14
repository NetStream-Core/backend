package main

import (
	"network-monitor-backend/internal/api"
	"network-monitor-backend/internal/config"
	"network-monitor-backend/internal/logger"
	"network-monitor-backend/internal/storage/database"
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
		logger.Logger.Fatal("Failed to load configuration")
	}

	dsn := cfg.Database.BuildDSN()
	logger.Logger.Info("Database DSN configured")

	db, err := database.New(dsn)
	if err != nil {
		logger.Logger.Fatal("Failed to initialize database")
	}

	r := api.New()

	if err := r.Router.Run(":" + cfg.Server.Port); err != nil {
		logger.Logger.Fatal("Backend is down :(")
	}

	if err := db.Close(); err != nil {
		panic("Database could not be closed :o")
	}
}
