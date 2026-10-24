package incidents_test

import (
	"context"
	"os"
	"testing"
	"time"

	"network-monitor-backend/internal/incidents"
	"network-monitor-backend/internal/storage/postgres"
)

// requires a real Postgres; set POSTGRES_TEST_DSN to run
// (e.g. postgresql://netstream:netstream-dev@localhost:15432/netstream_control_plane?sslmode=disable)
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
		_, _ = db.Pool.Exec(ctx, "TRUNCATE sensors, agent_configs, models, incidents RESTART IDENTITY CASCADE")
		db.Close()
	})
	return db
}

func TestRecordDetectionOpensANewIncidentForAFreshHostSrcIPPair(t *testing.T) {
	db := testDB(t)
	svc := incidents.NewService(db.Pool)

	incident, opened, err := svc.RecordDetection(context.Background(), incidents.Detection{
		HostID: "sensor-1", SrcIP: "10.0.0.5", Score: 11.5, At: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordDetection: %v", err)
	}
	if !opened {
		t.Fatalf("expected a new incident to be opened")
	}
	if incident.Status != "open" || incident.DetectionCount != 1 || incident.Severity != "low" {
		t.Fatalf("unexpected incident: %+v", incident)
	}
}

func TestRecordDetectionExtendsAnAlreadyOpenIncidentInsteadOfDuplicating(t *testing.T) {
	db := testDB(t)
	svc := incidents.NewService(db.Pool)
	ctx := context.Background()
	now := time.Now().UTC()

	first, opened, err := svc.RecordDetection(ctx, incidents.Detection{
		HostID: "sensor-1", SrcIP: "10.0.0.5", Score: 5.0, At: now,
	})
	if err != nil || !opened {
		t.Fatalf("first RecordDetection: opened=%v err=%v", opened, err)
	}

	second, opened, err := svc.RecordDetection(ctx, incidents.Detection{
		HostID: "sensor-1", SrcIP: "10.0.0.5", Score: 9.0, At: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("second RecordDetection: %v", err)
	}
	if opened {
		t.Fatalf("second detection within reopenGap should extend, not open a new incident")
	}
	if second.ID != first.ID {
		t.Fatalf("expected the same incident id, got %d then %d", first.ID, second.ID)
	}
	if second.DetectionCount != 2 {
		t.Fatalf("expected detection_count 2, got %d", second.DetectionCount)
	}
	if second.MaxScore != 9.0 {
		t.Fatalf("expected max_score to track the higher score, got %v", second.MaxScore)
	}
}

func TestRecordDetectionOpensAFreshIncidentAfterTheReopenGapHasPassed(t *testing.T) {
	db := testDB(t)
	svc := incidents.NewService(db.Pool)
	ctx := context.Background()
	now := time.Now().UTC()

	first, _, err := svc.RecordDetection(ctx, incidents.Detection{
		HostID: "sensor-1", SrcIP: "10.0.0.5", Score: 5.0, At: now,
	})
	if err != nil {
		t.Fatalf("first RecordDetection: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE incidents SET status = 'closed' WHERE id = $1`, first.ID); err != nil {
		t.Fatalf("manually close the first incident: %v", err)
	}

	second, opened, err := svc.RecordDetection(ctx, incidents.Detection{
		HostID: "sensor-1", SrcIP: "10.0.0.5", Score: 5.0, At: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("second RecordDetection: %v", err)
	}
	if !opened {
		t.Fatalf("expected a new incident once the previous one is closed")
	}
	if second.ID == first.ID {
		t.Fatalf("expected a different incident id")
	}
}

func TestRecordDetectionSeverityEscalatesWithDetectionCount(t *testing.T) {
	db := testDB(t)
	svc := incidents.NewService(db.Pool)
	ctx := context.Background()
	now := time.Now().UTC()

	var last *incidents.Incident
	for i := range 6 {
		incident, _, err := svc.RecordDetection(ctx, incidents.Detection{
			HostID: "sensor-1", SrcIP: "10.0.0.5", Score: 1.0, At: now.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("RecordDetection #%d: %v", i, err)
		}
		last = incident
	}
	if last.DetectionCount != 6 {
		t.Fatalf("expected detection_count 6, got %d", last.DetectionCount)
	}
	if last.Severity != "medium" {
		t.Fatalf("expected severity to escalate to medium at 6 detections, got %q", last.Severity)
	}
}

func TestListAndGetAndClose(t *testing.T) {
	db := testDB(t)
	svc := incidents.NewService(db.Pool)
	ctx := context.Background()

	incident, _, err := svc.RecordDetection(ctx, incidents.Detection{
		HostID: "sensor-1", SrcIP: "10.0.0.5", Score: 1.0, At: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("RecordDetection: %v", err)
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(list))
	}

	fetched, err := svc.Get(ctx, incident.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fetched.ID != incident.ID {
		t.Fatalf("Get returned the wrong incident")
	}

	closed, err := svc.Close(ctx, incident.ID)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if closed.Status != "closed" {
		t.Fatalf("expected status closed, got %q", closed.Status)
	}

	if _, err := svc.Close(ctx, incident.ID); err != incidents.ErrNotFound {
		t.Fatalf("closing an already-closed incident should return ErrNotFound, got %v", err)
	}
}

func TestGetReturnsErrNotFoundForAMissingIncident(t *testing.T) {
	db := testDB(t)
	svc := incidents.NewService(db.Pool)

	if _, err := svc.Get(context.Background(), 999999); err != incidents.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
