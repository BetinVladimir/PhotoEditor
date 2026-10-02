package upload

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
)

type uncertainCompletionStore struct {
	*objectstore.Store
	mode  string
	fired bool
}

func (s *uncertainCompletionStore) CompleteMultipart(ctx context.Context, key, uploadID, contentType string, expectedBytes int64, parts []objectstore.UploadedPart) (objectstore.PendingObject, error) {
	if !s.fired {
		s.fired = true
		if s.mode == "before" {
			return objectstore.PendingObject{}, objectstore.ErrUnavailable
		}
		if s.mode == "after" {
			if _, err := s.Store.CompleteMultipart(ctx, key, uploadID, contentType, expectedBytes, parts); err != nil {
				return objectstore.PendingObject{}, err
			}
			return objectstore.PendingObject{}, objectstore.ErrUnavailable
		}
	}
	return s.Store.CompleteMultipart(ctx, key, uploadID, contentType, expectedBytes, parts)
}

func newIntegrationStore(t *testing.T) (*objectstore.Store, string) {
	t.Helper()
	endpoint := os.Getenv("PHOTO_TEST_MINIO_URL")
	access, secret := os.Getenv("PHOTO_TEST_MINIO_ACCESS"), os.Getenv("PHOTO_TEST_MINIO_SECRET")
	if endpoint == "" || access == "" || secret == "" {
		t.Skip("isolated MinIO required")
	}
	bucket := "photo-upload-" + uuid.NewString()
	store, err := objectstore.New(objectstore.Config{Endpoint: endpoint, PublicEndpoint: endpoint, Bucket: bucket, AccessKey: access, SecretKey: secret, Development: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = store.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	client, err := minio.New(endpoint[len("http://"):], &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), Secure: false})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		for object := range client.ListObjects(cleanup, bucket, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
			if object.Err == nil {
				_ = client.RemoveObject(cleanup, bucket, object.Key, minio.RemoveObjectOptions{VersionID: object.VersionID})
			}
		}
		_ = client.RemoveBucket(cleanup, bucket)
	})
	return store, bucket
}

func TestIntegrationStorageCompletionUnknownOutcome(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	for _, mode := range []string{"before", "after"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			admin, err := pgxpool.New(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer admin.Close()
			apiConfig, _ := pgxpool.ParseConfig(dsn)
			apiConfig.ConnConfig.RuntimeParams["role"] = "photo_api"
			api, err := pgxpool.NewWithConfig(ctx, apiConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer api.Close()
			schedulerConfig, _ := pgxpool.ParseConfig(dsn)
			schedulerConfig.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
			scheduler, err := pgxpool.NewWithConfig(ctx, schedulerConfig)
			if err != nil {
				t.Fatal(err)
			}
			defer scheduler.Close()
			store, bucket := newIntegrationStore(t)

			var user, tenantID, projectID, collectionID uuid.UUID
			var epoch int64
			if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@test.example").Scan(&user, &epoch); err != nil {
				t.Fatal(err)
			}
			if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tenantID); err != nil {
				t.Fatal(err)
			}
			sessionID := uuid.New()
			if _, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')", sessionID, user, auth.HashSecret(uuid.NewString()), uuid.New()); err != nil {
				t.Fatal(err)
			}
			if err = admin.QueryRow(ctx, "INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Completion','workspace') RETURNING id", tenantID).Scan(&projectID); err != nil {
				t.Fatal(err)
			}
			if err = admin.QueryRow(ctx, "INSERT INTO photo.collections(tenant_id,project_id,name,state) VALUES($1,$2,'Destination','draft') RETURNING id", tenantID, projectID).Scan(&collectionID); err != nil {
				t.Fatal(err)
			}
			data := []byte("versioned multipart upload bytes")
			service := Service{DB: api, Objects: store}
			who := auth.Identity{UserID: user, SessionID: sessionID, Epoch: epoch}
			session, err := service.Begin(ctx, who, tenantID, "begin-"+mode, BeginRequest{ProjectID: projectID, CollectionID: &collectionID, Filename: "photo.jpg", Bytes: int64(len(data)), Format: "jpeg"})
			if err != nil {
				t.Fatal(err)
			}
			urls, err := store.SignUploadParts(ctx, session.objectKey, *session.multipartID, []int{1}, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			request, _ := http.NewRequestWithContext(ctx, http.MethodPut, urls[0].URL, bytes.NewReader(data))
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			etag := response.Header.Get("ETag")
			if response.StatusCode/100 != 2 || etag == "" {
				t.Fatalf("part upload status=%d etag=%q", response.StatusCode, etag)
			}
			if _, err = service.Complete(ctx, who, tenantID, session.ID, "complete-"+mode, CompleteRequest{ExpectedSize: int64(len(data)), Parts: []objectstore.UploadedPart{{PartNumber: 1, ETag: etag}}}); err != nil {
				t.Fatal(err)
			}
			uncertain := &uncertainCompletionStore{Store: store, mode: mode}
			processor := CompletionProcessor{Leases: SQLCompletionLeases{DB: scheduler, Bucket: bucket}, Objects: uncertain, HeartbeatInterval: 100 * time.Millisecond, RenewTimeout: time.Second, WorkTimeout: 20 * time.Second}
			changed, firstErr := processor.ProcessOne(ctx)
			if mode == "before" {
				if changed || (!errors.Is(firstErr, objectstore.ErrUnavailable) && !errors.Is(firstErr, objectstore.ErrMissing)) {
					t.Fatalf("pre-complete uncertainty changed=%v err=%v", changed, firstErr)
				}
				if _, err = admin.Exec(ctx, "UPDATE photo.upload_sessions SET next_completion_at=clock_timestamp() WHERE id=$1", session.ID); err != nil {
					t.Fatal(err)
				}
				changed, firstErr = processor.ProcessOne(ctx)
			}
			if firstErr != nil || !changed {
				t.Fatalf("completion recovery changed=%v err=%v", changed, firstErr)
			}
			var state string
			var actual int64
			var artifacts, snapshots, runs int
			if err = admin.QueryRow(ctx, `SELECT u.state,u.actual_size,
				(SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=u.tenant_id AND a.id=u.staging_artifact_id),
				(SELECT count(*) FROM photo.snapshots s WHERE s.tenant_id=u.tenant_id AND s.id=u.ingest_snapshot_id),
				(SELECT count(*) FROM photo.runs r WHERE r.tenant_id=u.tenant_id AND r.id=u.ingest_run_id)
				FROM photo.upload_sessions u WHERE u.id=$1`, session.ID).Scan(&state, &actual, &artifacts, &snapshots, &runs); err != nil {
				t.Fatal(err)
			}
			if state != "verifying" || actual != int64(len(data)) || artifacts != 1 || snapshots != 1 || runs != 1 {
				t.Fatalf("adoption state=%s bytes=%d rows=%d/%d/%d", state, actual, artifacts, snapshots, runs)
			}
			var reservedBytes, reservedFiles int64
			if err = admin.QueryRow(ctx, "SELECT reserved_bytes,reserved_files FROM photo.tenant_usage WHERE tenant_id=$1", tenantID).Scan(&reservedBytes, &reservedFiles); err != nil || reservedBytes != int64(len(data)) || reservedFiles != 1 {
				t.Fatalf("quota reservation bytes=%d files=%d err=%v", reservedBytes, reservedFiles, err)
			}
			if replayed, replayErr := processor.ProcessOne(ctx); replayErr != nil || replayed {
				t.Fatalf("terminal completion replay changed=%v err=%v", replayed, replayErr)
			}
		})
	}
}
