package database

import (
	"context"
	"fmt"
	"network-monitor-backend/internal/logger"
	"network-monitor-backend/proto"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type Database struct {
	pool *pgxpool.Pool
}

func New(dsn string) (*Database, error) {
	logger.Logger.Info("Initializing database connection")
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		logger.Logger.Error("Failed to create database connection pool", zap.Error(err))
		return nil, err
	}
	if err := pool.Ping(context.Background()); err != nil {
		logger.Logger.Error("Failed to ping database", zap.Error(err))
		pool.Close()
		return nil, err
	}
	logger.Logger.Info("Database connection established successfully")
	db := &Database{
		pool: pool,
	}
	return db, nil
}

func (d *Database) Write(data any) error {
	metric, ok := data.(*proto.PacketMetric)
	if !ok {
		logger.Logger.Error("Invalid data type for metric", zap.String("type", fmt.Sprintf("%T", data)))
		return fmt.Errorf("invalid data type: expected *proto.PacketMetric, got %T", data)
	}

	timestamp := time.Unix(int64(metric.Timestamp), 0)

	query := `
        INSERT INTO metrics (
            time, protocol, count, src_ip, dst_ip, src_port, dst_port, payload_size
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
    `
	_, err := d.pool.Exec(context.Background(), query,
		timestamp,
		metric.Protocol,
		metric.Count,
		metric.SrcIp,
		metric.DstIp,
		metric.SrcPort,
		metric.DstPort,
		metric.PayloadSize,
	)
	if err != nil {
		logger.Logger.Error("Failed to write data to database", zap.Error(err))
		return err
	}
	logger.Logger.Debug("Data successfully written to database",
		zap.Time("time", timestamp),
		zap.Uint32("protocol", metric.Protocol),
		zap.Uint64("count", metric.Count),
		zap.String("src_ip", metric.SrcIp),
		zap.String("dst_ip", metric.DstIp),
		zap.Uint32("src_port", metric.SrcPort),
		zap.Uint32("dst_port", metric.DstPort),
		zap.Uint32("payload_size", metric.PayloadSize))
	return nil
}

func (d *Database) Close() error {
	if d.pool != nil {
		d.pool.Close()
		logger.Logger.Info("Database connection closed")
	}
	return nil
}
