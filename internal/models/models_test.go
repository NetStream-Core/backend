package models_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"network-monitor-backend/internal/models"
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
		_, _ = db.Pool.Exec(ctx, "TRUNCATE sensors, agent_configs, models, incidents RESTART IDENTITY CASCADE")
		db.Close()
	})
	return db
}

func TestRegisterRequiresNameAndVersion(t *testing.T) {
	db := testDB(t)
	svc := models.NewService(db.Pool)

	_, err := svc.Register(context.Background(), models.RegisterRequest{Name: "iforest"})
	if err == nil {
		t.Fatal("expected an error when version is missing")
	}
}

func TestRegisterIsIdempotentByNameAndVersion(t *testing.T) {
	db := testDB(t)
	svc := models.NewService(db.Pool)
	ctx := context.Background()

	first, err := svc.Register(ctx, models.RegisterRequest{
		Name: "iforest", Version: "v1", Algorithm: "IsolationForest",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	second, err := svc.Register(ctx, models.RegisterRequest{
		Name: "iforest", Version: "v1", Algorithm: "IsolationForest", ArtifactURI: "/updated/path",
	})
	if err != nil {
		t.Fatalf("register again: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected the same model row, got ids %d and %d", first.ID, second.ID)
	}
	if second.ArtifactURI != "/updated/path" {
		t.Fatalf("expected re-registering to update artifact_uri, got %q", second.ArtifactURI)
	}
}

func TestRegisterDefaultsMetricsToAnEmptyObject(t *testing.T) {
	db := testDB(t)
	svc := models.NewService(db.Pool)

	model, err := svc.Register(context.Background(), models.RegisterRequest{Name: "iforest", Version: "v1"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	var metrics map[string]float64
	if err := json.Unmarshal(model.Metrics, &metrics); err != nil {
		t.Fatalf("unmarshal metrics: %v", err)
	}
	if len(metrics) != 0 {
		t.Fatalf("expected empty metrics, got %v", metrics)
	}
}

func TestGetFailsForAnUnknownModel(t *testing.T) {
	db := testDB(t)
	svc := models.NewService(db.Pool)

	_, err := svc.Get(context.Background(), "no-such-model", "v1")
	if !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListOrdersByNameThenNewestFirst(t *testing.T) {
	db := testDB(t)
	svc := models.NewService(db.Pool)
	ctx := context.Background()

	for _, v := range []string{"v1", "v2"} {
		if _, err := svc.Register(ctx, models.RegisterRequest{Name: "iforest", Version: v}); err != nil {
			t.Fatalf("register %s: %v", v, err)
		}
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].Version != "v2" || list[1].Version != "v1" {
		t.Fatalf("expected v2 before v1 (newest first), got %+v", list)
	}
}
