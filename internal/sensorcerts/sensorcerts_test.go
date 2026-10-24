package sensorcerts_test

import (
	"context"
	"os"
	"testing"
	"time"

	"network-monitor-backend/internal/sensorcerts"
	"network-monitor-backend/internal/storage/postgres"
)

// requires a real Postgres; set POSTGRES_TEST_DSN to run
func testDB(t *testing.T) *postgres.Postgres {
	t.Helper()
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN not set, skipping integration test")
	}
	ctx := context.Background()
	db, err := postgres.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(ctx, "TRUNCATE sensors, agent_configs, models, incidents, sensor_certificates RESTART IDENTITY CASCADE")
		db.Close()
	})
	return db
}

func TestRecordAndListForSensor(t *testing.T) {
	db := testDB(t)
	svc := sensorcerts.NewService(db.Pool)
	ctx := context.Background()

	cert, err := svc.Record(ctx, "sensor-1", "aa11", time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if cert.HostID != "sensor-1" || cert.Serial != "aa11" || cert.RevokedAt != nil {
		t.Fatalf("unexpected certificate: %+v", cert)
	}

	list, err := svc.ListForSensor(ctx, "sensor-1")
	if err != nil {
		t.Fatalf("ListForSensor: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 certificate, got %d", len(list))
	}
}

func TestRevokeLatestRevokesTheMostRecentActiveCertificate(t *testing.T) {
	db := testDB(t)
	svc := sensorcerts.NewService(db.Pool)
	ctx := context.Background()

	if _, err := svc.Record(ctx, "sensor-1", "serial-old", time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("Record (old): %v", err)
	}
	latest, err := svc.Record(ctx, "sensor-1", "serial-new", time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("Record (new): %v", err)
	}

	revoked, err := svc.RevokeLatest(ctx, "sensor-1")
	if err != nil {
		t.Fatalf("RevokeLatest: %v", err)
	}
	if revoked.Serial != latest.Serial {
		t.Fatalf("expected to revoke the most recently issued certificate %q, revoked %q", latest.Serial, revoked.Serial)
	}
	if revoked.RevokedAt == nil {
		t.Fatalf("expected RevokedAt to be set")
	}
}

func TestRevokeLatestReturnsErrNotFoundWhenNothingIsActive(t *testing.T) {
	db := testDB(t)
	svc := sensorcerts.NewService(db.Pool)
	ctx := context.Background()

	if _, err := svc.Record(ctx, "sensor-1", "serial-1", time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := svc.RevokeLatest(ctx, "sensor-1"); err != nil {
		t.Fatalf("first RevokeLatest: %v", err)
	}

	if _, err := svc.RevokeLatest(ctx, "sensor-1"); err != sensorcerts.ErrNotFound {
		t.Fatalf("expected ErrNotFound on a second revoke, got %v", err)
	}
}
