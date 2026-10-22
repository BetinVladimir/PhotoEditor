package objectstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

func TestIntegrationMinIOServerCORSBootstrapAndSignedMethods(t *testing.T) {
	endpoint, raw := os.Getenv("PHOTO_TEST_MINIO_URL"), os.Getenv("PHOTO_TEST_MINIO_SERVER_CORS_ORIGINS")
	if endpoint == "" || raw == "" {
		t.Skip("explicit isolated MinIO server-CORS fixture required")
	}
	origins := strings.Split(raw, ",")
	store, err := New(Config{Endpoint: endpoint, PublicEndpoint: endpoint, Bucket: "photo-cors-" + uuid.NewString(), AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true, CORSOrigins: origins, CORSStrategy: "minio-server"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for object := range store.client.ListObjects(ctx, store.bucket, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
			if object.Err != nil {
				t.Error("cleanup listing failed")
				return
			}
			if err := store.client.RemoveObject(ctx, store.bucket, object.Key, minio.RemoveObjectOptions{VersionID: object.VersionID}); err != nil {
				t.Error("cleanup object failed")
			}
		}
		if err := store.client.RemoveBucket(ctx, store.bucket); err != nil {
			t.Error("cleanup bucket failed")
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err = store.Bootstrap(ctx); err != nil {
		t.Fatal("server-CORS bootstrap", err)
	}
	data := []byte("version-pinned browser download")
	object, err := store.PutImmutable(ctx, "cors/download", bytes.NewReader(data), int64(len(data)), digest(data), "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		query := make(url.Values)
		query.Set("versionId", object.VersionID)
		signed, err := store.publicClient.Presign(ctx, method, store.bucket, object.Key, time.Minute, query)
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequestWithContext(ctx, method, signed.String(), nil)
		req.Header.Set("Origin", origins[0])
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		if readErr != nil || resp.StatusCode != 200 || resp.Header.Get("Access-Control-Allow-Origin") != origins[0] || resp.Header.Get("ETag") == "" || !corsHeaderContains(resp.Header.Get("Access-Control-Expose-Headers"), "ETag") {
			t.Fatal("browser cannot read pinned receipt", method, resp.StatusCode)
		}
		if method == http.MethodGet && !bytes.Equal(body, data) {
			t.Fatal("download bytes changed")
		}
		if method == http.MethodGet {
			req, _ = http.NewRequestWithContext(ctx, http.MethodDelete, signed.String(), nil)
			resp, err = client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatal("GET signature accepted for DELETE", resp.StatusCode)
			}
		}
	}
	signed, err := store.publicClient.Presign(ctx, http.MethodPut, store.bucket, "cors/browser-upload", time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, signed.String(), strings.NewReader("browser upload"))
	req.Header.Set("Origin", origins[0])
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Access-Control-Allow-Origin") != origins[0] || resp.Header.Get("ETag") == "" || !corsHeaderContains(resp.Header.Get("Access-Control-Expose-Headers"), "ETag") {
		t.Fatal("browser upload receipt unavailable", resp.StatusCode)
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodDelete, signed.String(), nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatal("PUT signature accepted for DELETE", resp.StatusCode)
	}
	if err = store.Check(ctx, object); err != nil {
		t.Fatal("signed mismatch damaged pinned download", err)
	}
	// Configuration readback must reject an incomplete expected origin set.
	store.corsOrigins = []string{"http://localhost:19999"}
	if err = store.Bootstrap(ctx); err != ErrIntegrity {
		t.Fatal("mismatched server policy accepted", err)
	}
	// Native MinIO does not implement bucket CORS. The default strategy must
	// still fail rather than silently treating server policy as equivalent.
	store.corsStrategy = "bucket"
	store.corsOrigins = origins
	err = store.Bootstrap(ctx)
	var storageError *StorageError
	if !errors.As(err, &storageError) || storageError.Status != http.StatusNotImplemented {
		t.Fatal("bucket strategy silently fell back", err)
	}

}
