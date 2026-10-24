// Package incidents turns a stream of detections (`ml`'s online detector,
// reading netstream.detections.v1) into incidents an operator can act on:
// repeated attack detections for the same (host_id, src_ip) collapse into
// one open incident instead of one row per detection, the way the
// external review's "incidents table exists, no handlers yet" gap asked
// for. This is not the Correlation Engine the architecture doc explicitly
// defers past the thesis (clustering across hosts, alert suppression
// rules, SLOs) — just the minimum needed for "detect → block" to end in
// something an operator can see and close, not a raw detection firehose.
package incidents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("incident not found")

// reopenGap bounds how long after an incident's last detection a new one
// for the same (host_id, src_ip) still counts as the same incident,
// rather than a new one. A long-quiet source starting up again later is a
// new event, not a continuation of whatever triggered the old incident.
const reopenGap = 5 * time.Minute

type Severity string

const (
	SeverityLow    Severity = "low"
	SeverityMedium Severity = "medium"
	SeverityHigh   Severity = "high"
)

// severityFor is a simple, documented step function on detection_count —
// not a tunable model, just enough to tell an operator "this one fired
// once" from "this one has been going for a while" at a glance in a list.
func severityFor(detectionCount int64) Severity {
	switch {
	case detectionCount >= 20:
		return SeverityHigh
	case detectionCount >= 5:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

type Incident struct {
	ID             int64     `json:"id"`
	HostID         string    `json:"host_id"`
	SrcIP          string    `json:"src_ip"`
	FirstSeen      time.Time `json:"first_seen"`
	LastSeen       time.Time `json:"last_seen"`
	Status         string    `json:"status"`
	Severity       string    `json:"severity"`
	DetectionCount int64     `json:"detection_count"`
	MaxScore       float64   `json:"max_score"`
	CreatedAt      time.Time `json:"created_at"`
}

// Detection is the subset of ml's DetectionResult (netstream.detections.v1)
// this package needs — the Kafka consumer decodes the full JSON message,
// but only attack detections with these fields ever reach RecordDetection.
type Detection struct {
	HostID string
	SrcIP  string
	Score  float64
	At     time.Time
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

const incidentColumns = `
	id, host_id, src_ip, first_seen, last_seen, status, severity,
	detection_count, max_score, created_at
`

func scanIncident(row pgx.Row) (*Incident, error) {
	var incident Incident
	err := row.Scan(
		&incident.ID, &incident.HostID, &incident.SrcIP, &incident.FirstSeen, &incident.LastSeen,
		&incident.Status, &incident.Severity, &incident.DetectionCount, &incident.MaxScore,
		&incident.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &incident, nil
}

// RecordDetection is the one write path into this package, called once
// per attack detection consumed from netstream.detections.v1. It either
// extends the (host_id, src_ip)'s currently open incident (if its
// last_seen is within reopenGap) or opens a new one — opened is true only
// the second case, telling the caller whether this is worth an alert.
func (s *Service) RecordDetection(ctx context.Context, d Detection) (incident *Incident, opened bool, err error) {
	if d.HostID == "" || d.SrcIP == "" {
		return nil, false, fmt.Errorf("host_id and src_ip are required")
	}

	existing, err := s.openIncidentFor(ctx, d.HostID, d.SrcIP)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, false, fmt.Errorf("look up open incident: %w", err)
	}
	if err == nil && d.At.Sub(existing.LastSeen) <= reopenGap {
		updated, updateErr := s.extendIncident(ctx, existing.ID, d)
		if updateErr != nil {
			return nil, false, fmt.Errorf("extend incident: %w", updateErr)
		}
		return updated, false, nil
	}

	created, createErr := s.openIncident(ctx, d)
	if createErr != nil {
		return nil, false, fmt.Errorf("open incident: %w", createErr)
	}
	return created, true, nil
}

func (s *Service) openIncidentFor(ctx context.Context, hostID, srcIP string) (*Incident, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+incidentColumns+` FROM incidents
		WHERE host_id = $1 AND src_ip = $2 AND status = 'open'
	`, hostID, srcIP)
	return scanIncident(row)
}

func (s *Service) extendIncident(ctx context.Context, id int64, d Detection) (*Incident, error) {
	// severity depends on the post-increment detection_count; CASE on
	// `detection_count + 1` inside the same UPDATE keeps this one
	// statement instead of a read-then-write race between two queries.
	row := s.pool.QueryRow(ctx, `
		UPDATE incidents SET
			last_seen = $2,
			detection_count = detection_count + 1,
			max_score = GREATEST(max_score, $3),
			severity = CASE
				WHEN detection_count + 1 >= 20 THEN 'high'
				WHEN detection_count + 1 >= 5 THEN 'medium'
				ELSE 'low'
			END
		WHERE id = $1
		RETURNING `+incidentColumns+`
	`, id, d.At, d.Score)
	return scanIncident(row)
}

func (s *Service) openIncident(ctx context.Context, d Detection) (*Incident, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO incidents (host_id, src_ip, first_seen, last_seen, status, severity, detection_count, max_score)
		VALUES ($1, $2, $3, $3, 'open', $4, 1, $5)
		RETURNING `+incidentColumns+`
	`, d.HostID, d.SrcIP, d.At, string(severityFor(1)), d.Score)
	return scanIncident(row)
}

func (s *Service) List(ctx context.Context) ([]*Incident, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+incidentColumns+` FROM incidents ORDER BY last_seen DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var incidents []*Incident
	for rows.Next() {
		incident, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		incidents = append(incidents, incident)
	}
	return incidents, rows.Err()
}

func (s *Service) Get(ctx context.Context, id int64) (*Incident, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+incidentColumns+` FROM incidents WHERE id = $1`, id)
	return scanIncident(row)
}

// Close marks an open incident resolved, e.g. once an operator has
// investigated and confirmed the response (block/quarantine) was
// sufficient. A later detection for the same (host_id, src_ip) opens a
// fresh incident rather than reopening this one (see reopenGap and the
// partial unique index in migration 0005).
func (s *Service) Close(ctx context.Context, id int64) (*Incident, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE incidents SET status = 'closed' WHERE id = $1 AND status = 'open'
		RETURNING `+incidentColumns+`
	`, id)
	return scanIncident(row)
}
