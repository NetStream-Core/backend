package storage

import "network-monitor-backend/internal/storage/database"

func NewDatabaseStorage(dsn string) (Storage, error) {
	return database.New(dsn)
}
