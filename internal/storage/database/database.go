package database

import (
	"context"
	"encoding/json"
	"network-monitor-backend/internal/logger"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Database struct {
	pool *pgxpool.Pool
}

func New(dsn string) (*Database, error) {
	logger.Logger.Info("Initializing database connection")

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		logger.Logger.Error("Failed to create database connection pool")
		return nil, err
	}

	if err := pool.Ping(context.Background()); err != nil {
		logger.Logger.Error("Failed to ping database")
		pool.Close()
		return nil, err
	}

	logger.Logger.Info("Database connection established successfully")

	db := &Database{
		pool: pool,
	}
	return db, nil
}

func (db *Database) Write(data any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		logger.Logger.Error("Failed to marshal data to JSON")
		return err
	}

	query := `INSERT INTO monitoring_data (data, created_at) VALUES ($1, NOW())`
	_, err = db.pool.Exec(context.Background(), query, jsonData)
	if err != nil {
		logger.Logger.Error("Failed to write data to database")
		return err
	}

	logger.Logger.Debug("Data successfully written to database")
	return nil
}

func (db *Database) Close() error {
	if db.pool != nil {
		db.pool.Close()
		logger.Logger.Info("Database connection closed")
	}
	return nil
}
