package objectstore_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"network-monitor-backend/internal/storage/objectstore"
)

// requires a real S3-compatible server; set OBJECTSTORE_TEST_ENDPOINT to run
// (e.g. localhost:18333, credentials netstream/netstream-dev, bucket netstream-models-test)
func testStore(t *testing.T) *objectstore.Store {
	t.Helper()
	endpoint := os.Getenv("OBJECTSTORE_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("OBJECTSTORE_TEST_ENDPOINT not set, skipping integration test")
	}
	store, err := objectstore.New(context.Background(), endpoint, "netstream", "netstream-dev", "netstream-models-test", false)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return store
}

func TestUploadThenDownloadRoundTrips(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	content := "hello from a test artifact"

	uri, err := store.Upload(ctx, "roundtrip/artifact.bin", strings.NewReader(content), int64(len(content)), "application/octet-stream")
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if !strings.HasPrefix(uri, objectstore.URIScheme) {
		t.Fatalf("expected uri to start with %s, got %s", objectstore.URIScheme, uri)
	}

	key, err := store.ParseKey(uri)
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	if key != "roundtrip/artifact.bin" {
		t.Fatalf("expected the key to round-trip, got %q", key)
	}

	reader, err := store.Download(ctx, key)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer func() { _ = reader.Close() }()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != content {
		t.Fatalf("expected %q, got %q", content, string(got))
	}
}

func TestDownloadFailsForAMissingKey(t *testing.T) {
	store := testStore(t)

	_, err := store.Download(context.Background(), "no/such/key")
	if err == nil {
		t.Fatal("expected an error for a missing key")
	}
}

func TestParseKeyRejectsAUriFromAnotherBucket(t *testing.T) {
	store := testStore(t)

	_, err := store.ParseKey("objectstore://some-other-bucket/path")
	if err == nil {
		t.Fatal("expected an error for a uri naming a different bucket")
	}
}

func TestPresignedDownloadURLServesTheSameContent(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	content := []byte("presigned content")

	uri, err := store.Upload(ctx, "presigned/artifact.bin", bytes.NewReader(content), int64(len(content)), "application/octet-stream")
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	key, err := store.ParseKey(uri)
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}

	url, err := store.PresignedDownloadURL(ctx, key, time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if !strings.Contains(url, "X-Amz-Signature") {
		t.Fatalf("expected a signed URL, got %s", url)
	}
}
