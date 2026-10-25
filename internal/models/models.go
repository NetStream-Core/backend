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
	// ArtifactSHA256 (hex) and ArtifactSignature (base64 Ed25519, signed
	// over the raw sha256 digest bytes) are set together by
	// UploadModelArtifact, from headers the uploader computed client-side
	// — the backend never has the signing key and cannot verify them
	// itself; it only stores what the uploader asserts, for `ml` to verify
	// against its own trusted public key before deserializing the
	// artifact it downloads.
	ArtifactSHA256        string    `json:"artifact_sha256"`
	ArtifactSignature     string    `json:"artifact_signature"`
	OnnxArtifactURI       string    `json:"onnx_artifact_uri"`
	OnnxArtifactSHA256    string    `json:"onnx_artifact_sha256"`
	OnnxArtifactSignature string    `json:"onnx_artifact_signature"`
	Status                string    `json:"status"`
	CreatedAt             time.Time `json:"created_at"`
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

const modelColumns = `id, name, version, algorithm, features_version, dataset_ref, git_sha, metrics,
	artifact_uri, artifact_sha256, artifact_signature,
	onnx_artifact_uri, onnx_artifact_sha256, onnx_artifact_signature, status, created_at`

func scanModel(row pgx.Row) (*Model, error) {
	var model Model
	err := row.Scan(
		&model.ID, &model.Name, &model.Version, &model.Algorithm, &model.FeaturesVersion,
		&model.DatasetRef, &model.GitSHA, &model.Metrics, &model.ArtifactURI,
		&model.ArtifactSHA256, &model.ArtifactSignature,
		&model.OnnxArtifactURI, &model.OnnxArtifactSHA256, &model.OnnxArtifactSignature,
		&model.Status, &model.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (s *Service) List(ctx context.Context) ([]Model, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+modelColumns+`
		FROM models ORDER BY name, created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer rows.Close()

	var result []Model
	for rows.Next() {
		model, err := scanModel(rows)
		if err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		result = append(result, *model)
	}
	return result, rows.Err()
}

func (s *Service) Get(ctx context.Context, name, version string) (*Model, error) {
	model, err := scanModel(s.pool.QueryRow(ctx, `
		SELECT `+modelColumns+`
		FROM models WHERE name = $1 AND version = $2
	`, name, version))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get model: %w", err)
	}
	return model, nil
}

// SetArtifact records where the model's artifact was uploaded to, together
// with the integrity values the uploader computed over its own bytes
// before uploading (sha256 digest, Ed25519 signature over that digest).
// It is separate from Register because the artifact is usually uploaded
// after the model's metadata is registered, once training has actually
// produced a file to upload.
func (s *Service) SetArtifact(
	ctx context.Context, name, version, artifactURI, sha256Hex, signatureB64 string,
) (*Model, error) {
	model, err := scanModel(s.pool.QueryRow(ctx, `
		UPDATE models SET artifact_uri = $3, artifact_sha256 = $4, artifact_signature = $5
		WHERE name = $1 AND version = $2
		RETURNING `+modelColumns+`
	`, name, version, artifactURI, sha256Hex, signatureB64))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("set artifact: %w", err)
	}
	return model, nil
}

func (s *Service) SetOnnxArtifact(
	ctx context.Context, name, version, artifactURI, sha256Hex, signatureB64 string,
) (*Model, error) {
	model, err := scanModel(s.pool.QueryRow(ctx, `
		UPDATE models SET onnx_artifact_uri = $3, onnx_artifact_sha256 = $4, onnx_artifact_signature = $5
		WHERE name = $1 AND version = $2
		RETURNING `+modelColumns+`
	`, name, version, artifactURI, sha256Hex, signatureB64))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("set onnx artifact: %w", err)
	}
	return model, nil
}
