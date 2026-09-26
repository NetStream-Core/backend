// Package sensors is the control-plane's view of the fleet: which sensors
// exist, what configuration each one should be running (desired), and what
// it last reported (actual). It intentionally knows nothing about how a
// sensor enforces that configuration — that is the agent's job.
package sensors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("sensor not found")

type Sensor struct {
	ID            int64     `json:"id"`
	HostID        string    `json:"host_id"`
	Hostname      string    `json:"hostname"`
	AgentVersion  string    `json:"agent_version"`
	ConfigVersion int64     `json:"config_version"`
	LastSeen      time.Time `json:"last_seen"`
	CreatedAt     time.Time `json:"created_at"`
}

type Config struct {
	SensorID  int64           `json:"sensor_id"`
	Version   int64           `json:"version"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type RegisterRequest struct {
	HostID       string `json:"host_id"`
	Hostname     string `json:"hostname"`
	AgentVersion string `json:"agent_version"`
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Register upserts a sensor by host_id and refreshes last_seen — called by
// the agent on startup and on every heartbeat. It never touches
// config_version: that only advances through SetDesiredConfig, so a
// heartbeat can't accidentally roll a sensor onto a stale config.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*Sensor, error) {
	if req.HostID == "" {
		return nil, fmt.Errorf("host_id is required")
	}

	var sensor Sensor
	err := s.pool.QueryRow(ctx, `
		INSERT INTO sensors (host_id, hostname, agent_version, last_seen)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (host_id) DO UPDATE SET
			hostname = EXCLUDED.hostname,
			agent_version = EXCLUDED.agent_version,
			last_seen = now()
		RETURNING id, host_id, hostname, agent_version, config_version, last_seen, created_at
	`, req.HostID, req.Hostname, req.AgentVersion).Scan(
		&sensor.ID, &sensor.HostID, &sensor.Hostname, &sensor.AgentVersion,
		&sensor.ConfigVersion, &sensor.LastSeen, &sensor.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert sensor: %w", err)
	}
	return &sensor, nil
}

func (s *Service) List(ctx context.Context) ([]Sensor, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, host_id, hostname, agent_version, config_version, last_seen, created_at
		FROM sensors ORDER BY host_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list sensors: %w", err)
	}
	defer rows.Close()

	var result []Sensor
	for rows.Next() {
		var sensor Sensor
		if err := rows.Scan(
			&sensor.ID, &sensor.HostID, &sensor.Hostname, &sensor.AgentVersion,
			&sensor.ConfigVersion, &sensor.LastSeen, &sensor.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan sensor: %w", err)
		}
		result = append(result, sensor)
	}
	return result, rows.Err()
}

func (s *Service) Get(ctx context.Context, hostID string) (*Sensor, error) {
	var sensor Sensor
	err := s.pool.QueryRow(ctx, `
		SELECT id, host_id, hostname, agent_version, config_version, last_seen, created_at
		FROM sensors WHERE host_id = $1
	`, hostID).Scan(
		&sensor.ID, &sensor.HostID, &sensor.Hostname, &sensor.AgentVersion,
		&sensor.ConfigVersion, &sensor.LastSeen, &sensor.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get sensor: %w", err)
	}
	return &sensor, nil
}

// SetDesiredConfig stores a new config version as a transactional snapshot:
// the whole payload is inserted and sensors.config_version is bumped in one
// transaction, so a reader never observes a config_version that doesn't yet
// have a matching row in agent_configs.
func (s *Service) SetDesiredConfig(ctx context.Context, hostID string, payload json.RawMessage) (*Config, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sensorID, nextVersion int64
	err = tx.QueryRow(ctx, `
		UPDATE sensors SET config_version = config_version + 1
		WHERE host_id = $1
		RETURNING id, config_version
	`, hostID).Scan(&sensorID, &nextVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("bump config version: %w", err)
	}

	var config Config
	err = tx.QueryRow(ctx, `
		INSERT INTO agent_configs (sensor_id, version, payload)
		VALUES ($1, $2, $3)
		RETURNING sensor_id, version, payload, created_at
	`, sensorID, nextVersion, payload).Scan(
		&config.SensorID, &config.Version, &config.Payload, &config.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert config version: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit config version: %w", err)
	}
	return &config, nil
}

// GetDesiredConfig returns the latest config snapshot for a sensor — what
// the agent should converge to, regardless of what it last reported.
func (s *Service) GetDesiredConfig(ctx context.Context, hostID string) (*Config, error) {
	var config Config
	err := s.pool.QueryRow(ctx, `
		SELECT ac.sensor_id, ac.version, ac.payload, ac.created_at
		FROM agent_configs ac
		JOIN sensors s ON s.id = ac.sensor_id
		WHERE s.host_id = $1
		ORDER BY ac.version DESC
		LIMIT 1
	`, hostID).Scan(&config.SensorID, &config.Version, &config.Payload, &config.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get desired config: %w", err)
	}
	return &config, nil
}
