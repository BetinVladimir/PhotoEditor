package upload

import (
	"bytes"
	"context"
	"encoding/json"
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
	"photoeditor/backend/internal/rawdevelopment"
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
	for _, testCase := range []struct {
		name, mode       string
		raw, projectOnly bool
		archiveProject   bool
	}{
		{"before", "before", false, false, false}, {"after", "after", false, false, false},
		{"raw_before", "before", true, false, false}, {"raw_after", "after", true, false, false},
		{"project_before", "before", false, true, false}, {"project_after", "after", false, true, false},
		{"raw_project_before", "before", true, true, false}, {"raw_project_after", "after", true, true, false},
		{"project_archived", "after", false, true, true},
	} {
		mode := testCase.mode
		t.Run(testCase.name, func(t *testing.T) {
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
			// This fixture qualifies completion SQL/S3, not native RAW quality.
			// Supply an explicit synthetic admission adapter for RAW cases.
			if testCase.raw {
				service.RAWAdmission = completionRAWAdmission{}
			}
			who := auth.Identity{UserID: user, SessionID: sessionID, Epoch: epoch}
			admission := BeginRequest{ProjectID: projectID, CollectionID: &collectionID, Filename: "photo.jpg", Bytes: int64(len(data)), Format: "jpeg"}
			if testCase.projectOnly {
				admission.CollectionID = nil
			}
			if testCase.raw {
				admission.Filename = "photo.nef"
				admission.Format = "nef"
				admission.RAWDevelopment = &rawdevelopment.Policy{SchemaVersion: 1, Demosaic: "ahd", WBSource: "camera", ExposureEV: 1.5, HighlightPolicy: "blend", DecoderVersion: "rawpy-0.27.1/libraw-0.21.4"}
			}
			capabilities, capabilityErr := service.RAWCapabilities(ctx, who, tenantID, projectID)
			if capabilityErr != nil || capabilities.ProjectID != projectID || capabilities.Available || capabilities.Development != nil {
				t.Fatal("synthetic completion admission advertised native qualification", capabilityErr)
			}
			if _, capabilityErr = service.RAWCapabilities(ctx, who, tenantID, uuid.New()); capabilityErr == nil {
				t.Fatal("foreign project capability scope accepted")
			}
			session, err := service.Begin(ctx, who, tenantID, "begin-"+mode, admission)
			if err != nil {
				t.Fatal(err)
			}
			if testCase.raw {
				replay, err := service.Begin(ctx, who, tenantID, "begin-"+mode, admission)
				if err != nil || replay.ID != session.ID {
					t.Fatal("RAW admission replay", err)
				}
				if _, err = admin.Exec(ctx, `UPDATE photo.upload_sessions SET raw_development=jsonb_set(raw_development,'{exposure_ev}','2') WHERE id=$1`, session.ID); err == nil {
					t.Fatal("RAW policy mutated")
				}
				var allowed bool
				if err = api.QueryRow(ctx, `SELECT has_function_privilege(current_user,'photo.claim_upload_completion_v2()','EXECUTE')`).Scan(&allowed); err != nil || allowed {
					t.Fatal("API can claim scheduler completion", err)
				}
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
			if testCase.raw {
				// Model restore fencing of a previously issued claim. Only
				// the lease deadline changes; immutable settings survive.
				if _, err = admin.Exec(ctx, `UPDATE photo.upload_sessions SET completion_token=$2,completion_lease_until=clock_timestamp()+interval '1 hour' WHERE id=$1`, session.ID, uuid.New()); err != nil {
					t.Fatal(err)
				}
				if _, err = admin.Exec(ctx, `UPDATE photo.upload_sessions SET completion_lease_until='-infinity'::timestamptz WHERE id=$1`, session.ID); err != nil {
					t.Fatal(err)
				}
				var persisted []byte
				if err = admin.QueryRow(ctx, `SELECT raw_development FROM photo.upload_sessions WHERE id=$1`, session.ID).Scan(&persisted); err != nil {
					t.Fatal(err)
				}
				var policy rawdevelopment.Policy
				if err = json.Unmarshal(persisted, &policy); err != nil || policy != *admission.RAWDevelopment {
					t.Fatal("restore fencing changed RAW policy", err)
				}
			}
			if testCase.archiveProject {
				if _, err = admin.Exec(ctx, `UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`, tenantID, projectID); err != nil {
					t.Fatal(err)
				}
			}
			uncertain := &uncertainCompletionStore{Store: store, mode: mode}
			processor := CompletionProcessor{Leases: SQLCompletionLeases{DB: scheduler, Bucket: bucket}, Objects: uncertain, HeartbeatInterval: 100 * time.Millisecond, RenewTimeout: time.Second, WorkTimeout: 20 * time.Second}
			changed, firstErr := processor.ProcessOne(ctx)
			if testCase.archiveProject {
				if changed || firstErr == nil {
					t.Fatal("archived project completion accepted", firstErr)
				}
				var unchanged bool
				if err = admin.QueryRow(ctx, `SELECT u.state='completing' AND u.actual_size IS NULL AND u.staging_artifact_id IS NULL AND NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.id=u.staging_candidate_id) AND NOT EXISTS(SELECT FROM photo.runs r WHERE r.id=u.ingest_run_id) AND NOT EXISTS(SELECT FROM photo.snapshots s WHERE s.id=u.ingest_snapshot_id) FROM photo.upload_sessions u WHERE id=$1`, session.ID).Scan(&unchanged); err != nil || !unchanged {
					t.Fatal("destination rejection partially adopted upload", err)
				}
				return
			}
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
			// Completion must preserve the chosen destination, including a genuine
			// project-only user upload; it must not invent a default collection.
			var bindingProject uuid.UUID
			var bindingCollection *uuid.UUID
			if err = admin.QueryRow(ctx, `SELECT b.project_id,b.collection_id FROM photo.upload_sessions u JOIN photo.ingest_upload_bindings b ON b.tenant_id=u.tenant_id AND b.run_id=u.ingest_run_id AND b.ordinal=0 WHERE u.id=$1`, session.ID).Scan(&bindingProject, &bindingCollection); err != nil {
				t.Fatal(err)
			}
			if bindingProject != projectID || (testCase.projectOnly && bindingCollection != nil) || (!testCase.projectOnly && (bindingCollection == nil || *bindingCollection != collectionID)) {
				t.Fatal("completion changed upload destination")
			}
			var payload []byte
			if err = admin.QueryRow(ctx, `SELECT i.payload FROM photo.upload_sessions u JOIN photo.snapshot_items i ON i.tenant_id=u.tenant_id AND i.snapshot_id=u.ingest_snapshot_id AND i.ordinal=0 WHERE u.id=$1`, session.ID).Scan(&payload); err != nil {
				t.Fatal(err)
			}
			var snapshot struct {
				Version int                    `json:"schema_version"`
				Policy  *rawdevelopment.Policy `json:"raw_development"`
			}
			if err = json.Unmarshal(payload, &snapshot); err != nil {
				t.Fatal(err)
			}
			if testCase.raw {
				if snapshot.Version != 2 || snapshot.Policy == nil || *snapshot.Policy != *admission.RAWDevelopment {
					t.Fatal("persisted RAW snapshot changed")
				}
			} else if snapshot.Version != 1 || snapshot.Policy != nil {
				t.Fatal("legacy snapshot changed")
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

// Only a completion fixture: production must install rawdevelopment.Source.
type completionRAWAdmission struct{}

func (completionRAWAdmission) Admit(ctx context.Context, tx rawdevelopment.ReportRows, tenantID, projectID, actor, session uuid.UUID, policy *rawdevelopment.Policy) (*rawdevelopment.Policy, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owned := *policy
	return &owned, nil
}
