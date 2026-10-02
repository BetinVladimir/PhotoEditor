package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

func TestRejectUnsafeKeysAndEndpoints(t *testing.T) {
	for _, key := range []string{"../asset", "/absolute", "a/../b", "a//b", "a/", "x\\y", "a\nsecret", ""} {
		if validKey(key) {
			t.Fatal("unsafe key accepted", key)
		}
	}
	key, err := AttemptKey(uuid.New(), uuid.New(), uuid.New(), 1, 3)
	if err != nil || !validKey(key) {
		t.Fatal("generated key rejected", err)
	}
	for _, endpoint := range []string{"http://example.com", "https://user:secret@example.com", "https://example.com/path", "https://example.com?x=1"} {
		if _, err = New(Config{Endpoint: endpoint, Bucket: "test", AccessKey: "test", SecretKey: "test", Development: true}); err == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
}
func digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func testStore(t *testing.T) *Store {
	t.Helper()
	endpoint := os.Getenv("PHOTO_TEST_MINIO_URL")
	if endpoint == "" {
		t.Skip("isolated MinIO required")
	}
	bucket := "photo-test-" + uuid.NewString()
	s, err := New(Config{Endpoint: endpoint, Bucket: bucket, AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for object := range s.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
			if object.Err != nil {
				t.Error("cleanup listing failed")
				return
			}
			if err := s.client.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{VersionID: object.VersionID}); err != nil {
				t.Error("cleanup version failed")
			}
		}
		if err := s.client.RemoveBucket(ctx, bucket); err != nil {
			t.Error("cleanup bucket failed")
		}
	})
	return s
}
func TestIntegrationVersionedStreamAndMultipart(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for _, size := range []int{0, 37, 10*1024*1024 + 19} {
		data := bytes.Repeat([]byte{0x9b}, size)
		key, _ := AttemptKey(uuid.New(), uuid.New(), uuid.New(), 1, 1)
		object, err := s.PutImmutable(ctx, key, bytes.NewReader(data), int64(size), digest(data), "application/octet-stream")
		if err != nil {
			t.Fatal("upload", size, err)
		}
		if object.VersionID == "" || object.VersionID == "null" {
			t.Fatal("unversioned upload")
		}
		reader, err := s.Open(ctx, object)
		if err != nil {
			t.Fatal(err)
		}
		received, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || !bytes.Equal(received, data) {
			t.Fatal("read integrity", size, err)
		}
		if size >= 37 {
			ranged, e := s.OpenRange(ctx, object, 7, 23)
			if e != nil {
				t.Fatal("range open", e)
			}
			part, e := io.ReadAll(ranged)
			ranged.Close()
			if e != nil || !bytes.Equal(part, data[7:24]) {
				t.Fatal("range contents", e)
			}
		}
		if _, err = s.PutImmutable(ctx, key, bytes.NewReader(data), int64(size), digest(data), "application/octet-stream"); !errors.Is(err, ErrExists) {
			t.Fatal("overwrite allowed", size, err)
		}
		if err = s.Remove(ctx, object); err != nil {
			t.Fatal(err)
		}
		if err = s.Check(ctx, object); !errors.Is(err, ErrMissing) {
			t.Fatal("deleted version readable", err)
		}
	}
}
func TestIntegrationCorruptUploadsNeverAccepted(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, body := range [][]byte{[]byte("bad"), []byte("good plus surplus")} {
		key, _ := AttemptKey(uuid.New(), uuid.New(), uuid.New(), 1, 1)
		if _, err := s.PutImmutable(ctx, key, bytes.NewReader(body), 3, digest([]byte("yes")), "application/octet-stream"); !errors.Is(err, ErrIntegrity) {
			t.Fatal("corruption accepted", err)
		}
		_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
		if minio.ToErrorResponse(err).Code != "NoSuchKey" {
			t.Fatal("rejected object retained", err)
		}
	}
}
func TestIntegrationConcurrentCreateOnly(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	key, _ := AttemptKey(uuid.New(), uuid.New(), uuid.New(), 1, 1)
	data := []byte("immutable bytes")
	var group sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := s.PutImmutable(ctx, key, bytes.NewReader(data), int64(len(data)), digest(data), "application/octet-stream")
			results <- err
		}()
	}
	group.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrExists) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("conditional upload race", success, conflict)
	}
}

func TestIntegrationPurgeAttemptExactKey(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tenant, task, artifact := uuid.New(), uuid.New(), uuid.New()
	key, _ := AttemptKey(tenant, task, artifact, 1, 1)
	payload := []byte("orphan")
	object, err := s.PutImmutable(ctx, key, bytes.NewReader(payload), int64(len(payload)), digest(payload), "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	neighbor, err := s.PutImmutable(ctx, key+"/neighbor", bytes.NewReader(payload), int64(len(payload)), digest(payload), "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PurgeAttempt(ctx, tenant, task, artifact, 1, 1); err != nil {
		t.Fatal(err)
	}
	if err = s.Check(ctx, object); !errors.Is(err, ErrMissing) {
		t.Fatal("orphan remains", err)
	}
	if err = s.Check(ctx, neighbor); err != nil {
		t.Fatal("prefix neighbor deleted", err)
	}
	if err = s.PurgeAttempt(ctx, tenant, task, artifact, 1, 1); err != nil {
		t.Fatal("repeat cleanup failed", err)
	}
}

func TestTenantPrefixIsUUIDScoped(t *testing.T) {
	id := uuid.New()
	prefix, err := tenantPrefix(id)
	if err != nil || prefix != "tenants/"+id.String()+"/" {
		t.Fatal("invalid tenant prefix", prefix, err)
	}
	if _, err = tenantPrefix(uuid.Nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("nil tenant accepted")
	}
}

func TestIntegrationPurgeTenantBatchIsBoundedAndScoped(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tenant, neighborTenant := uuid.New(), uuid.New()
	payload := []byte("tenant-delete")
	var owned []Object
	for i := 0; i < 3; i++ {
		key, _ := AttemptKey(tenant, uuid.New(), uuid.New(), 1, 1)
		object, err := s.PutImmutable(ctx, key, bytes.NewReader(payload), int64(len(payload)), digest(payload), "application/octet-stream")
		if err != nil {
			t.Fatal(err)
		}
		owned = append(owned, object)
	}
	key, _ := AttemptKey(neighborTenant, uuid.New(), uuid.New(), 1, 1)
	neighbor, err := s.PutImmutable(ctx, key, bytes.NewReader(payload), int64(len(payload)), digest(payload), "application/octet-stream")
	if err != nil {
		t.Fatal(err)
	}
	passes := 0
	for {
		more, err := s.PurgeTenantBatch(ctx, tenant, 1)
		if err != nil {
			t.Fatal(err)
		}
		passes++
		if !more {
			break
		}
		if passes > 5 {
			t.Fatal("tenant cleanup did not converge")
		}
	}
	if passes < 3 {
		t.Fatal("batch limit was not enforced", passes)
	}
	for _, object := range owned {
		if err = s.Check(ctx, object); !errors.Is(err, ErrMissing) {
			t.Fatal("tenant object remains", err)
		}
	}
	if err = s.Check(ctx, neighbor); err != nil {
		t.Fatal("neighbor tenant object deleted", err)
	}
}
