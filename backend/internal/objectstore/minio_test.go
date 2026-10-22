package objectstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"io"
	"net"
	"net/http"
	"net/url"
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
func digest(data []byte) string     { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func testStore(t *testing.T) *Store { return testStoreWithCORS(t, nil) }
func testStoreWithCORS(t *testing.T, origins []string) *Store {
	t.Helper()
	endpoint := os.Getenv("PHOTO_TEST_MINIO_URL")
	if endpoint == "" {
		t.Skip("isolated MinIO required")
	}
	bucket := "photo-test-" + uuid.NewString()
	t.Logf("owned_fixture_bucket_intent=%s", bucket)
	s, err := New(Config{Endpoint: endpoint, Bucket: bucket, AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true, CORSOrigins: origins})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// A fresh UUID bucket is exclusively owned by this fixture. Register its
	// inverse before Bootstrap, which may create it before a later setup error.
	exists, err := s.client.BucketExists(ctx, bucket)
	if err != nil || exists {
		t.Fatal("fresh fixture bucket preflight failed")
	}
	t.Cleanup(func() { cleanupOwnedTestBucket(t, s) })
	if err = s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}

	return s
}

// Cleanup uses explicit trusted administration when a public storage transport
// does not support bucket deletion. All product assertions still use s.client
// and public presigned URLs; no production policy or TLS validation is changed.
func cleanupOwnedTestBucket(t *testing.T, s *Store) {
	t.Helper()
	const prefix = "photo-test-"
	if len(s.bucket) <= len(prefix) || s.bucket[:len(prefix)] != prefix {
		t.Error("unowned fixture bucket cleanup refused")
		return
	}
	id, err := uuid.Parse(s.bucket[len(prefix):])
	if err != nil || id == uuid.Nil || prefix+id.String() != s.bucket {
		t.Error("noncanonical fixture bucket cleanup refused")
		return
	}
	admin := s.client
	if endpoint := os.Getenv("PHOTO_TEST_MINIO_ADMIN_URL"); endpoint != "" {
		u, e := url.Parse(endpoint)
		if e != nil || u.Scheme != "https" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Hostname() != "localhost" && (net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback())) {
			t.Error("trusted fixture cleanup endpoint invalid")
			return
		}
		pem, e := os.ReadFile(os.Getenv("PHOTO_TEST_MINIO_ADMIN_CA_FILE"))
		if e != nil || digest(pem) != os.Getenv("PHOTO_TEST_MINIO_ADMIN_CA_SHA256") {
			t.Error("trusted fixture cleanup CA pin invalid")
			return
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			t.Error("trusted fixture cleanup CA invalid")
			return
		}
		transport := &http.Transport{DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
		defer transport.CloseIdleConnections()
		admin, e = minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(os.Getenv("PHOTO_TEST_MINIO_ACCESS"), os.Getenv("PHOTO_TEST_MINIO_SECRET"), ""), Secure: true, Region: "us-east-1", Transport: transport})
		if e != nil {
			t.Error("trusted fixture cleanup client invalid")
			return
		}

	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exists, e := admin.BucketExists(ctx, s.bucket)
	if e != nil {
		response := minio.ToErrorResponse(e)
		t.Errorf("cleanup bucket existence failed code=%s status=%d error_type=%T detail=%v", response.Code, response.StatusCode, e, e)
		return
	}
	if !exists {
		return
	}
	count := 0
	incomplete := map[string]bool{}
	for object := range admin.ListIncompleteUploads(ctx, s.bucket, "", true) {
		if object.Err != nil {
			t.Error("cleanup multipart listing failed")
			return
		}
		count++
		if count > 1000 {
			t.Error("cleanup multipart bound exceeded")
			return
		}
		incomplete[object.Key] = true
	}
	for key := range incomplete {
		if e = admin.RemoveIncompleteUpload(ctx, s.bucket, key); e != nil {
			t.Error("cleanup multipart abort failed")
			return
		}
	}
	count = 0
	drained := false
	for pass := 0; pass < 3; pass++ {
		versions := []minio.ObjectInfo{}
		for object := range admin.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
			if object.Err != nil {
				t.Error("cleanup version listing failed")
				return
			}
			count++
			if count > 10000 {
				t.Error("cleanup total version bound exceeded")
				return
			}
			if object.VersionID == "" {
				t.Error("cleanup unpinned version refused")
				return
			}
			versions = append(versions, object)
		}
		t.Logf("cleanup snapshot pass=%d versions=%d", pass, len(versions))
		if len(versions) == 0 {
			drained = true
			break
		}
		for _, object := range versions {
			if e = admin.RemoveObject(ctx, s.bucket, object.Key, minio.RemoveObjectOptions{VersionID: object.VersionID}); e != nil {
				t.Error("cleanup version failed")
				return
			}
		}
	}
	if !drained {
		t.Error("cleanup version reconciliation bound exhausted")
		return
	}
	for object := range admin.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
		if object.Err != nil {
			t.Error("cleanup final absence listing failed")
		} else {
			t.Error("cleanup retained object version")
		}
		return
	}

	// Classify public transport separately; a successful trusted inverse is still
	// mandatory, and neither bucketwide policy nor product assertions are relaxed.
	if e = s.client.RemoveBucket(ctx, s.bucket); e != nil {
		response := minio.ToErrorResponse(e)
		t.Logf("public fixture DeleteBucket code=%s status=%d error_type=%T", response.Code, response.StatusCode, e)
		if e = admin.RemoveBucket(ctx, s.bucket); e != nil {
			response = minio.ToErrorResponse(e)
			t.Errorf("trusted cleanup bucket failed code=%s status=%d error_type=%T", response.Code, response.StatusCode, e)
			return
		}
	}
	exists, e = admin.BucketExists(ctx, s.bucket)
	if e != nil || exists {
		t.Error("cleanup bucket absence not proven")
		return
	}
	t.Logf("owned fixture bucket removed and absent: %s", s.bucket)
}

func TestIntegrationBootstrapArchiveCORSReadbackAndRotation(t *testing.T) {
	origins := []string{"http://localhost:8080", "http://127.0.0.2:3000"}
	s := testStoreWithCORS(t, origins)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	installed, err := s.client.GetBucketCors(ctx, s.bucket)
	if err != nil || !archiveCORSInstalled(installed, origins) {
		t.Fatal("archive CORS missing after bootstrap", err, installed)
	}
	s.corsOrigins = []string{origins[0]}
	if err = s.Bootstrap(ctx); err != nil {
		t.Fatal("CORS rotation failed", err)
	}
	installed, err = s.client.GetBucketCors(ctx, s.bucket)
	if err != nil || !archiveCORSInstalled(installed, s.corsOrigins) {
		t.Fatal("old origin retained after rotation", err, installed)
	}
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
	prefixes, _ := tenantNamespaces(tenant)
	for _, prefix := range prefixes {
		for i := 0; i < 3; i++ {
			key := prefix + uuid.NewString()
			object, err := s.PutImmutable(ctx, key, bytes.NewReader(payload), int64(len(payload)), digest(payload), "application/octet-stream")
			if err != nil {
				t.Fatal(err)
			}
			owned = append(owned, object)
		}
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
		if passes > 12 {
			t.Fatal("tenant cleanup did not converge")
		}
	}
	if passes < 9 {
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

func TestIntegrationPurgeTenantBatchCountsMultipartSessions(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tenant := uuid.New()
	prefix, _ := tenantPrefix(tenant)
	key := prefix + "two-sessions"
	core := minio.Core{Client: s.client}
	first, err := core.NewMultipartUpload(ctx, s.bucket, key, minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.client.RemoveIncompleteUpload(context.Background(), s.bucket, key)
	second, err := core.NewMultipartUpload(ctx, s.bucket, key, minio.PutObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("multipart sessions were not distinct")
	}
	more, err := s.PurgeTenantBatch(ctx, tenant, 1)
	if err != nil || !more {
		t.Fatal("first exact session cleanup", more, err)
	}
	scan := s.client.ListIncompleteUploads(ctx, s.bucket, key, true)
	remaining := map[string]bool{}
	for row := range scan {
		if row.Err != nil {
			t.Fatal(row.Err)
		}
		if row.Key == key {
			remaining[row.UploadID] = true
		}
	}
	if len(remaining) != 1 {
		t.Fatal("batch removed more than one multipart session", remaining)
	}
	more, err = s.PurgeTenantBatch(ctx, tenant, 1)
	if err != nil || !more {
		t.Fatal("second exact session cleanup", more, err)
	}
	more, err = s.PurgeTenantBatch(ctx, tenant, 1)
	if err != nil || more {
		t.Fatal("multipart cleanup did not converge", more, err)
	}
}
