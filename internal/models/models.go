// Package models is the model registry: metadata about a trained model
// (which dataset, which feature set, which metrics, where the artifact
// lives), not the artifact itself. The artifact stays wherever `ml` writes
// it — the registry is just a catalog entry pointing at it.
package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("model not found")

type Model struct {
	ID              int64           `json:"id"`
	Name            string          `json:"name"`
	Version         string          `json:"version"`
	Algorithm       string          `json:"algorithm"`
	FeaturesVersion string          `json:"features_version"`
	DatasetRef      string          `json:"dataset_ref"`
	GitSHA          string          `json:"git_sha"`
	Metrics         json.RawMessage `json:"metrics"`
	ArtifactURI     string          `json:"artifact_uri"`
	Status          string          `json:"status"`
	CreatedAt       time.Time       `json:"created_at"`
}

type RegisterRequest struct {
	Name            string          `json:"name"`
	Version         string          `json:"version"`
	Algorithm       string          `json:"algorithm"`
	FeaturesVersion string          `json:"features_version"`
	DatasetRef      string          `json:"dataset_ref"`
	GitSHA          string          `json:"git_sha"`
	Metrics         json.RawMessage `json:"metrics"`
	ArtifactURI     string          `json:"artifact_uri"`
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) Register(ctx context.Context, req RegisterRequest) (*Model, error) {
	if req.Name == "" || req.Version == "" {
		return nil, fmt.Errorf("name and version are required")
	}
	metrics := req.Metrics
	if metrics == nil {
		metrics = json.RawMessage(`{}`)
	}

	var model Model
	err := s.pool.QueryRow(ctx, `
		INSERT INTO models (name, version, algorithm, features_version, dataset_ref, git_sha, metrics, artifact_uri)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (name, version) DO UPDATE SET
			algorithm = EXCLUDED.algorithm,
			features_version = EXCLUDED.features_version,
			dataset_ref = EXCLUDED.dataset_ref,
			git_sha = EXCLUDED.git_sha,
			metrics = EXCLUDED.metrics,
			artifact_uri = EXCLUDED.artifact_uri
		RETURNING id, name, version, algorithm, features_version, dataset_ref, git_sha, metrics, artifact_uri, status, created_at
	`, req.Name, req.Version, req.Algorithm, req.FeaturesVersion, req.DatasetRef, req.GitSHA, metrics, req.ArtifactURI).Scan(
		&model.ID, &model.Name, &model.Version, &model.Algorithm, &model.FeaturesVersion,
		&model.DatasetRef, &model.GitSHA, &model.Metrics, &model.ArtifactURI, &model.Status, &model.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("register model: %w", err)
	}
	return &model, nil
}

func (s *Service) List(ctx context.Context) ([]Model, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, version, algorithm, features_version, dataset_ref, git_sha, metrics, artifact_uri, status, created_at
		FROM models ORDER BY name, created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer rows.Close()

	var result []Model
	for rows.Next() {
		var model Model
		if err := rows.Scan(
			&model.ID, &model.Name, &model.Version, &model.Algorithm, &model.FeaturesVersion,
			&model.DatasetRef, &model.GitSHA, &model.Metrics, &model.ArtifactURI, &model.Status, &model.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		result = append(result, model)
	}
	return result, rows.Err()
}

func (s *Service) Get(ctx context.Context, name, version string) (*Model, error) {
	var model Model
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, version, algorithm, features_version, dataset_ref, git_sha, metrics, artifact_uri, status, created_at
		FROM models WHERE name = $1 AND version = $2
	`, name, version).Scan(
		&model.ID, &model.Name, &model.Version, &model.Algorithm, &model.FeaturesVersion,
		&model.DatasetRef, &model.GitSHA, &model.Metrics, &model.ArtifactURI, &model.Status, &model.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get model: %w", err)
	}
	return &model, nil
}

// SetArtifactURI records where the model's artifact was uploaded to. It is
// separate from Register because the artifact is usually uploaded after
// the model's metadata is registered, once training has actually produced
// a file to upload.
func (s *Service) SetArtifactURI(ctx context.Context, name, version, artifactURI string) (*Model, error) {
	var model Model
	err := s.pool.QueryRow(ctx, `
		UPDATE models SET artifact_uri = $3
		WHERE name = $1 AND version = $2
		RETURNING id, name, version, algorithm, features_version, dataset_ref, git_sha, metrics, artifact_uri, status, created_at
	`, name, version, artifactURI).Scan(
		&model.ID, &model.Name, &model.Version, &model.Algorithm, &model.FeaturesVersion,
		&model.DatasetRef, &model.GitSHA, &model.Metrics, &model.ArtifactURI, &model.Status, &model.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("set artifact uri: %w", err)
	}
	return &model, nil
}
