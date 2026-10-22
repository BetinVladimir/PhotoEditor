package objectstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

// Exercise a real signed GET, including the version selection in its signature.
func TestIntegrationSignedDownloadPinsVersion(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	payload := []byte("original verified presentation bytes")
	object, err := s.PutImmutable(ctx, "presentation.jpg", bytes.NewReader(payload), int64(len(payload)), digest(payload), "image/jpeg")
	if err != nil {
		var storage *StorageError
		code, status := "", 0
		if errors.As(err, &storage) {
			code, status = storage.Code, storage.Status
		}
		t.Logf("storage_code=%s storage_status=%d", code, status)
		t.Fatalf("publish failed: type=%T invalid=%t integrity=%t exists=%t unavailable=%t s3_code=%s", err, errors.Is(err, ErrInvalid), errors.Is(err, ErrIntegrity), errors.Is(err, ErrExists), errors.Is(err, ErrUnavailable), minio.ToErrorResponse(err).Code)
	}
	replacement := []byte("replacement must never be downloaded")
	newer, err := s.client.PutObject(ctx, s.bucket, object.Key, bytes.NewReader(replacement), int64(len(replacement)), minio.PutObjectOptions{ContentType: "image/jpeg"})
	if err != nil || newer.VersionID == object.VersionID {
		t.Fatal("replacement version unavailable")
	}
	signed, err := s.SignDownload(ctx, object, "presentation.jpg", time.Minute)
	if err != nil {
		t.Fatal("signing failed")
	}
	parsed, err := url.Parse(signed.URL)
	if err != nil || parsed.Query().Get("versionId") != object.VersionID {
		t.Fatal("signed version missing")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	get := func() *http.Response {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, signed.URL, nil)
		if err != nil {
			t.Fatal("request creation failed")
		}
		response, err := client.Do(request)
		// Never include the signed URL or HTTP error containing it in diagnostics.
		if err != nil {
			t.Fatal("signed download transport failed")
		}
		return response
	}
	response := get()
	received, readErr := io.ReadAll(io.LimitReader(response.Body, 1024))
	response.Body.Close()
	if response.StatusCode != http.StatusOK || readErr != nil || !bytes.Equal(received, payload) || digest(received) != object.SHA256 {
		t.Fatal("signed download integrity failed")
	}
	if response.Header.Get("X-Amz-Version-Id") != object.VersionID || response.Header.Get("Content-Type") != object.ContentType {
		t.Fatal("download metadata mismatch")
	}
	if err := s.Remove(ctx, object); err != nil {
		t.Fatal("version removal failed")
	}
	response = get()
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatal("removed version unexpectedly downloadable", response.StatusCode)
	}
}
