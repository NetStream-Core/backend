// Package sensorcerts records which certificates internal/pki has issued
// for which sensor, and which of those have been revoked — the audit
// trail for backend's certificate issuance, not an enforcement point
// (see internal/pki's package doc for why revocation isn't checked at
// the TLS handshake).
package sensorcerts

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("certificate not found")

type Certificate struct {
	ID        int64      `json:"id"`
	HostID    string     `json:"host_id"`
	Serial    string     `json:"serial"`
	IssuedAt  time.Time  `json:"issued_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

const certColumns = `id, host_id, serial, issued_at, expires_at, revoked_at`

func scanCertificate(row pgx.Row) (*Certificate, error) {
	var cert Certificate
	err := row.Scan(&cert.ID, &cert.HostID, &cert.Serial, &cert.IssuedAt, &cert.ExpiresAt, &cert.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

func (s *Service) Record(ctx context.Context, hostID, serial string, expiresAt time.Time) (*Certificate, error) {
	row := s.pool.QueryRow(ctx, `
		INSERT INTO sensor_certificates (host_id, serial, expires_at)
		VALUES ($1, $2, $3)
		RETURNING `+certColumns+`
	`, hostID, serial, expiresAt)
	return scanCertificate(row)
}

func (s *Service) ListForSensor(ctx context.Context, hostID string) ([]*Certificate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+certColumns+` FROM sensor_certificates WHERE host_id = $1 ORDER BY issued_at DESC
	`, hostID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var certs []*Certificate
	for rows.Next() {
		cert, err := scanCertificate(rows)
		if err != nil {
			return nil, err
		}
		certs = append(certs, cert)
	}
	return certs, rows.Err()
}

// RevokeLatest revokes the most recently issued, not-yet-revoked
// certificate for hostID — the one still in use, in the common case of
// one active certificate per sensor. Revoking an already-revoked (or
// never-issued) certificate returns ErrNotFound rather than silently
// doing nothing.
func (s *Service) RevokeLatest(ctx context.Context, hostID string) (*Certificate, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE sensor_certificates SET revoked_at = now()
		WHERE id = (
			SELECT id FROM sensor_certificates
			WHERE host_id = $1 AND revoked_at IS NULL
			ORDER BY issued_at DESC
			LIMIT 1
		)
		RETURNING `+certColumns+`
	`, hostID)
	return scanCertificate(row)
}
