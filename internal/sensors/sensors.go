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

// ErrInvalidConfigVersion means an ack named a version this sensor was
// never actually issued (either greater than its current desired
// config_version, or with no matching row in agent_configs at all) — a
// sensor claiming to have applied a config that was never sent to it,
// whether from a bug or a compromised/misbehaving agent.
var ErrInvalidConfigVersion = errors.New("invalid config version")

type Sensor struct {
	ID            int64  `json:"id"`
	HostID        string `json:"host_id"`
	Hostname      string `json:"hostname"`
	AgentVersion  string `json:"agent_version"`
	ConfigVersion int64  `json:"config_version"`
	// ActualConfigVersion is the version the agent has acked — but see
	// ActualConfigUnappliedFields: an ack can name a version while still
	// reporting that some of that version's own fields were not actually
	// applied (`response_mode`/`quarantine_ttl_secs` need an agent
	// restart, not a hot reload — see `agent/src/control_plane.rs`), so
	// this field alone does not mean "fully converged".
	ActualConfigVersion int64 `json:"actual_config_version"`
	// ActualConfigUnappliedFields is the most recent ack's own list of
	// payload keys the agent could not apply (a JSON array of strings,
	// empty when everything was applied) — not accumulated across acks,
	// just what the sensor currently reports about ActualConfigVersion.
	ActualConfigUnappliedFields json.RawMessage `json:"actual_config_unapplied_fields"`
	LastSeen                    time.Time       `json:"last_seen"`
	CreatedAt                   time.Time       `json:"created_at"`
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

const sensorColumns = `id, host_id, hostname, agent_version, config_version,
	actual_config_version, actual_config_unapplied_fields, last_seen, created_at`

func scanSensorArgs(sensor *Sensor) []any {
	return []any{
		&sensor.ID, &sensor.HostID, &sensor.Hostname, &sensor.AgentVersion,
		&sensor.ConfigVersion, &sensor.ActualConfigVersion, &sensor.ActualConfigUnappliedFields,
		&sensor.LastSeen, &sensor.CreatedAt,
	}
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
		RETURNING `+sensorColumns+`
	`, req.HostID, req.Hostname, req.AgentVersion).Scan(scanSensorArgs(&sensor)...)
	if err != nil {
		return nil, fmt.Errorf("upsert sensor: %w", err)
	}
	return &sensor, nil
}

func (s *Service) List(ctx context.Context) ([]Sensor, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+sensorColumns+`
		FROM sensors ORDER BY host_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list sensors: %w", err)
	}
	defer rows.Close()

	var result []Sensor
	for rows.Next() {
		var sensor Sensor
		if err := rows.Scan(scanSensorArgs(&sensor)...); err != nil {
			return nil, fmt.Errorf("scan sensor: %w", err)
		}
		result = append(result, sensor)
	}
	return result, rows.Err()
}

func (s *Service) Get(ctx context.Context, hostID string) (*Sensor, error) {
	var sensor Sensor
	err := s.pool.QueryRow(ctx, `
		SELECT `+sensorColumns+`
		FROM sensors WHERE host_id = $1
	`, hostID).Scan(scanSensorArgs(&sensor)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get sensor: %w", err)
	}
	return &sensor, nil
}

// AckActualConfig records the config version a sensor reports having
// actually applied, and which of that version's own fields it could not
// apply (empty means fully applied — see Sensor.ActualConfigUnappliedFields).
//
// Rejects a version this sensor was never actually issued: greater than
// its current desired config_version, or with no matching row in
// agent_configs — previously a plain GREATEST() accepted any int64 an
// agent cared to send, including one from a config pushed to a different
// sensor, or one that simply doesn't exist yet.
//
// Uses GREATEST rather than a plain assignment for the version itself so a
// delayed retry of an older ack (network retry, agent restart replaying its
// last known state) can never move actual_config_version backwards past a
// newer ack that already arrived — and updates unapplied-fields only when
// this ack is for that same newest version, so a late retry can't
// overwrite a newer ack's unapplied-fields with its own stale list either.
func (s *Service) AckActualConfig(
	ctx context.Context, hostID string, version int64, unappliedFields []string,
) (*Sensor, error) {
	var sensorID, desiredConfigVersion int64
	err := s.pool.QueryRow(ctx, `
		SELECT id, config_version FROM sensors WHERE host_id = $1
	`, hostID).Scan(&sensorID, &desiredConfigVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("look up sensor: %w", err)
	}
	if version > desiredConfigVersion {
		return nil, fmt.Errorf(
			"%w: %d exceeds this sensor's current desired config_version %d",
			ErrInvalidConfigVersion, version, desiredConfigVersion,
		)
	}

	var issued bool
	err = s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM agent_configs WHERE sensor_id = $1 AND version = $2)
	`, sensorID, version).Scan(&issued)
	if err != nil {
		return nil, fmt.Errorf("check config version was issued: %w", err)
	}
	if !issued {
		return nil, fmt.Errorf(
			"%w: version %d was never issued to this sensor", ErrInvalidConfigVersion, version,
		)
	}

	if unappliedFields == nil {
		unappliedFields = []string{}
	}
	unappliedJSON, err := json.Marshal(unappliedFields)
	if err != nil {
		return nil, fmt.Errorf("marshal unapplied fields: %w", err)
	}

	var sensor Sensor
	err = s.pool.QueryRow(ctx, `
		UPDATE sensors SET
			actual_config_version = GREATEST(actual_config_version, $2),
			actual_config_unapplied_fields = CASE
				WHEN $2 >= actual_config_version THEN $3::jsonb
				ELSE actual_config_unapplied_fields
			END
		WHERE host_id = $1
		RETURNING `+sensorColumns+`
	`, hostID, version, unappliedJSON).Scan(scanSensorArgs(&sensor)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("ack actual config: %w", err)
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
