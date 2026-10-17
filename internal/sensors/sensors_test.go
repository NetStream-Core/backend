package sensors_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"network-monitor-backend/internal/sensors"
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

func TestRegisterIsIdempotentByHostID(t *testing.T) {
	db := testDB(t)
	svc := sensors.NewService(db.Pool)
	ctx := context.Background()

	first, err := svc.Register(ctx, sensors.RegisterRequest{HostID: "h1", Hostname: "a", AgentVersion: "0.1"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	second, err := svc.Register(ctx, sensors.RegisterRequest{HostID: "h1", Hostname: "b", AgentVersion: "0.2"})
	if err != nil {
		t.Fatalf("register again: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected the same sensor row, got ids %d and %d", first.ID, second.ID)
	}
	if second.Hostname != "b" || second.AgentVersion != "0.2" {
		t.Fatalf("expected the second register to update hostname/agent_version, got %+v", second)
	}
}

func TestSetDesiredConfigFailsForAnUnknownSensor(t *testing.T) {
	db := testDB(t)
	svc := sensors.NewService(db.Pool)

	_, err := svc.SetDesiredConfig(context.Background(), "no-such-sensor", json.RawMessage(`{}`))
	if !errors.Is(err, sensors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSetDesiredConfigBumpsVersionAtomicallyWithTheSnapshot(t *testing.T) {
	db := testDB(t)
	svc := sensors.NewService(db.Pool)
	ctx := context.Background()

	sensor, err := svc.Register(ctx, sensors.RegisterRequest{HostID: "h2"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if sensor.ConfigVersion != 0 {
		t.Fatalf("expected a freshly registered sensor to start at config_version 0, got %d", sensor.ConfigVersion)
	}

	config1, err := svc.SetDesiredConfig(ctx, "h2", json.RawMessage(`{"response_mode":"monitor"}`))
	if err != nil {
		t.Fatalf("set config 1: %v", err)
	}
	if config1.Version != 1 {
		t.Fatalf("expected version 1, got %d", config1.Version)
	}

	config2, err := svc.SetDesiredConfig(ctx, "h2", json.RawMessage(`{"response_mode":"gateway"}`))
	if err != nil {
		t.Fatalf("set config 2: %v", err)
	}
	if config2.Version != 2 {
		t.Fatalf("expected version 2, got %d", config2.Version)
	}

	got, err := svc.Get(ctx, "h2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ConfigVersion != 2 {
		t.Fatalf("expected sensor's config_version to follow the latest snapshot, got %d", got.ConfigVersion)
	}

	desired, err := svc.GetDesiredConfig(ctx, "h2")
	if err != nil {
		t.Fatalf("get desired config: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(desired.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if desired.Version != 2 || payload["response_mode"] != "gateway" {
		t.Fatalf("expected the latest snapshot to come back, got version=%d payload=%s", desired.Version, desired.Payload)
	}
}

func TestGetDesiredConfigFailsWhenNoneWasEverSet(t *testing.T) {
	db := testDB(t)
	svc := sensors.NewService(db.Pool)
	ctx := context.Background()

	if _, err := svc.Register(ctx, sensors.RegisterRequest{HostID: "h3"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, err := svc.GetDesiredConfig(ctx, "h3")
	if !errors.Is(err, sensors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAckActualConfigFailsForAnUnknownSensor(t *testing.T) {
	db := testDB(t)
	svc := sensors.NewService(db.Pool)

	_, err := svc.AckActualConfig(context.Background(), "no-such-sensor", 1)
	if !errors.Is(err, sensors.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestAckActualConfigRecordsWhatTheAgentApplied(t *testing.T) {
	db := testDB(t)
	svc := sensors.NewService(db.Pool)
	ctx := context.Background()

	sensor, err := svc.Register(ctx, sensors.RegisterRequest{HostID: "h4"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if sensor.ActualConfigVersion != 0 {
		t.Fatalf("expected a freshly registered sensor to start at actual_config_version 0, got %d", sensor.ActualConfigVersion)
	}

	if _, err := svc.SetDesiredConfig(ctx, "h4", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("set desired config: %v", err)
	}
	if _, err := svc.SetDesiredConfig(ctx, "h4", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("set desired config again: %v", err)
	}

	acked, err := svc.AckActualConfig(ctx, "h4", 2)
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	if acked.ActualConfigVersion != 2 {
		t.Fatalf("expected actual_config_version 2, got %d", acked.ActualConfigVersion)
	}
	if acked.ConfigVersion != 2 {
		t.Fatalf("expected desired config_version to stay at 2, got %d", acked.ConfigVersion)
	}
}

func TestAckActualConfigNeverMovesBackwards(t *testing.T) {
	db := testDB(t)
	svc := sensors.NewService(db.Pool)
	ctx := context.Background()

	if _, err := svc.Register(ctx, sensors.RegisterRequest{HostID: "h5"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := svc.AckActualConfig(ctx, "h5", 5); err != nil {
		t.Fatalf("ack 5: %v", err)
	}

	// A delayed retry of an older ack (e.g. the agent's request to the
	// control plane was retried after it had already moved on to a newer
	// version) must not roll actual_config_version back.
	acked, err := svc.AckActualConfig(ctx, "h5", 3)
	if err != nil {
		t.Fatalf("ack 3: %v", err)
	}
	if acked.ActualConfigVersion != 5 {
		t.Fatalf("expected actual_config_version to stay at 5, got %d", acked.ActualConfigVersion)
	}
}

func TestListOrdersByHostID(t *testing.T) {
	db := testDB(t)
	svc := sensors.NewService(db.Pool)
	ctx := context.Background()

	for _, hostID := range []string{"z-sensor", "a-sensor"} {
		if _, err := svc.Register(ctx, sensors.RegisterRequest{HostID: hostID}); err != nil {
			t.Fatalf("register %s: %v", hostID, err)
		}
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].HostID != "a-sensor" || list[1].HostID != "z-sensor" {
		t.Fatalf("expected sensors ordered by host_id, got %+v", list)
	}
}
