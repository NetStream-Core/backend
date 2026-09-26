// Package objectstore stores model artifacts as opaque blobs behind an
// S3-compatible API (SeaweedFS in `deploy`, but any S3-compatible server
// works — this package only ever calls generic S3 operations, no
// vendor-specific extensions). The registry in `internal/models` stores
// only the resulting URI; the bytes never touch Postgres.
package objectstore

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Store struct {
	client *minio.Client
	bucket string
}

func New(ctx context.Context, endpoint, accessKey, secretKey, bucket string, useSSL bool) (*Store, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create object store client: %w", err)
	}

	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket %s: %w", bucket, err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create bucket %s: %w", bucket, err)
		}
	}

	return &Store{client: client, bucket: bucket}, nil
}

// URI is the scheme this package uses in `models.artifact_uri`. It is not a
// real URL scheme any S3 SDK understands directly — Download/PresignedURL
// take the bucket-relative key, not this URI, so ParseKey exists to get
// back from one to the other.
const URIScheme = "objectstore://"

func (s *Store) uri(key string) string {
	return URIScheme + s.bucket + "/" + key
}

// ParseKey extracts the object key from a URI this package produced. It
// only accepts URIs for this store's own bucket — a mismatch usually means
// the config changed after artifacts were already registered.
func (s *Store) ParseKey(uri string) (string, error) {
	prefix := URIScheme + s.bucket + "/"
	if len(uri) <= len(prefix) || uri[:len(prefix)] != prefix {
		return "", fmt.Errorf("uri %q is not an objectstore URI for bucket %s", uri, s.bucket)
	}
	return uri[len(prefix):], nil
}

// Upload stores the artifact under `key` and returns the URI to save as
// `models.artifact_uri`.
func (s *Store) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) (string, error) {
	_, err := s.client.PutObject(ctx, s.bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("upload object %s: %w", key, err)
	}
	return s.uri(key), nil
}

func (s *Store) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("download object %s: %w", key, err)
	}
	// GetObject only returns an API error lazily, on the first read/stat —
	// force it now so callers get a clean error instead of a 200 response
	// that fails when the body is actually consumed.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, fmt.Errorf("stat object %s: %w", key, err)
	}
	return obj, nil
}

// PresignedDownloadURL is a time-limited direct link to the artifact, so a
// large model file can be served without proxying its bytes through the
// backend.
func (s *Store) PresignedDownloadURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	url, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign object %s: %w", key, err)
	}
	return url.String(), nil
}
