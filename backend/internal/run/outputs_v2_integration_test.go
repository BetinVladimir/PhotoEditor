package run

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
	"time"
)

type unusedOutputSigner struct {
	calls    int
	objects  []objectstore.Object
	delegate *objectstore.Store
	onSign   func() error
}

func (s *unusedOutputSigner) SignDownload(ctx context.Context, object objectstore.Object, filename string, ttl time.Duration) (objectstore.ReadURL, error) {
	s.calls++
	s.objects = append(s.objects, object)
	if s.onSign != nil {
		if err := s.onSign(); err != nil {
			return objectstore.ReadURL{}, err
		}
	}
	if s.delegate != nil {
		return s.delegate.SignDownload(ctx, object, filename, ttl)
	}
	return objectstore.ReadURL{URL: "https://objects.example/version-pinned", ExpiresAt: time.Now().Add(time.Minute)}, nil
}
func TestIntegrationV2OutputsProjectWithoutCollection(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	var user, tid, snapshot, runID, projectID uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@test.example").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tid); err != nil {
		t.Fatal(err)
	}
	session := uuid.New()
	if _, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')", session, user, auth.HashSecret(uuid.NewString()), uuid.New()); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	if err = admin.QueryRow(ctx, "INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'V2 output project','restricted') RETURNING id", tid).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'v2','user',$2,$3,1) RETURNING id", tid, user, hash).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES($1,'v2','completed',$2,jsonb_build_object('project_id',$5::text),$3,$4,'fail_fast') RETURNING id", tid, snapshot, hash, user, projectID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES($1,$2,$3,$4,'preview','v2.2',$5)", tid, projectID, runID, snapshot, hash); err != nil {
		t.Fatal(err)
	}
	signer := &unusedOutputSigner{}
	bucket, version, outputHash, outputBytes := "test", "encoded-v1", hash, int64(42)
	var expected []byte
	if raw := os.Getenv("PHOTO_OUTPUT_S3_FIXTURE"); raw != "" {
		var fixture struct {
			Config                   objectstore.Config
			Version, SHA256, Payload string
			Bytes                    int64
		}
		if json.Unmarshal([]byte(raw), &fixture) != nil {
			t.Fatal("invalid S3 fixture")
		}
		signer.delegate, err = objectstore.New(fixture.Config)
		if err != nil {
			t.Fatal("S3 fixture configuration rejected")
		}
		expected, err = base64.StdEncoding.DecodeString(fixture.Payload)
		if err != nil || len(expected) == 0 {
			t.Fatal("invalid fixture payload")
		}
		bucket, version, outputHash, outputBytes = fixture.Config.Bucket, fixture.Version, fixture.SHA256, fixture.Bytes
	}
	service := Service{DB: api, Objects: signer, Bucket: bucket}
	who := auth.Identity{UserID: user, SessionID: session, Epoch: epoch}
	items, err := service.Outputs(ctx, who, tid, runID)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatal("collectionless V2 output scope", err)
	}

	status, e := service.Get(ctx, who, tid, runID)
	if e != nil || status.Presentation == nil || status.Presentation.Total != 0 {
		t.Fatal("scoped V2 presentation status missing", e)
	}
	// Publish controlled canonical metadata and verify the API query and signer.
	// The optional owned-S3 fixture downloads a real encoded JPEG; task execution
	// and canonical result adoption remain controlled in both modes.

	var collection uuid.UUID
	if err = admin.QueryRow(ctx, "INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Output fixture') RETURNING id", tid, projectID).Scan(&collection); err != nil {
		t.Fatal(err)
	}
	// Generic encoded previews live in preview runs. V2 runs admit only their
	// canonical document root; Render derivatives use a separate preview run.
	if err = admin.QueryRow(ctx, "INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,output_collection_id,config_snapshot,config_hash,created_by) VALUES($1,'preview','completed',$2,$3,'{}',$4,$5) RETURNING id", tid, snapshot, collection, hash, user).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	var params, task, output, result uuid.UUID
	if err = admin.QueryRow(ctx, "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES($1,'test',$2,$3,2,'application/json','params-v1',now()) RETURNING id", tid, uuid.NewString(), hash).Scan(&params); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES($1,$2,'media.preview:0','media.preview','1','media','ready','[]',$3,$4,1,1,'{\"renderer_version\":\"v2.2\"}') RETURNING id", tid, runID, params, hash).Scan(&task); err != nil {
		t.Fatal(err)
	}
	worker := uuid.New()
	if _, err = admin.Exec(ctx, "INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES($1,$2,1,1,$3,now(),now(),'cpu','{}')", tid, task, worker); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES($1,$4,'presentation.jpg',$5,$2,$6,'image/jpeg','derivative',now(),$3,1,1) RETURNING id", tid, outputHash, task, bucket, version, outputBytes).Scan(&output); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES($1,$2,1,1,jsonb_build_array(jsonb_build_object('artifact_id',$3::text,'sha256',$4::text)),'{}',$4) RETURNING id", tid, task, output, outputHash).Scan(&result); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.tasks SET state='succeeded',canonical_result_id=$1 WHERE id=$2", result, task); err != nil {
		t.Fatal(err)
	}
	items, err = service.Outputs(ctx, who, tid, runID)
	if err != nil || len(items) != 1 || items[0].ArtifactID != output || items[0].URL == "" || signer.calls != 1 {
		t.Fatal("current verified output missing", err)
	}
	candidates, err := service.readOutputCandidates(ctx, who, tid, runID)
	if err != nil || len(candidates) != 1 || candidates[0].Revocable {
		t.Fatal("ordinary media preview misclassified as reviewable", err)
	}
	signed := signer.objects[0]
	if signed.Key != "presentation.jpg" || signed.VersionID != version || signed.SHA256 != outputHash || signed.Bytes != outputBytes || signed.ContentType != "image/jpeg" {
		t.Fatal("immutable signer binding changed")
	}
	if signer.delegate != nil {
		request, e := http.NewRequestWithContext(ctx, http.MethodGet, items[0].URL, nil)
		if e != nil {
			t.Fatal("download request failed")
		}
		response, e := (&http.Client{Timeout: 10 * time.Second}).Do(request)
		if e != nil {
			t.Fatal("download transport failed")
		}
		received, e := io.ReadAll(io.LimitReader(response.Body, outputBytes+1))
		response.Body.Close()
		if e != nil || response.StatusCode != 200 || !bytes.Equal(received, expected) || response.Header.Get("X-Amz-Version-Id") != version {
			t.Fatal("API signed download differs from pinned version")
		}
	}
	// A state change during external URL signing must invalidate the response.
	signer.onSign = func() error {
		_, e := admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=$1", output)
		return e
	}
	items, err = service.Outputs(ctx, who, tid, runID)
	signer.onSign = nil
	if domain.AsError(err).Code != "STALE_CANDIDATE_REVIEW" || items != nil {
		t.Fatal("output changed during signing was returned", err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", output); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{
		"UPDATE photo.tasks SET fencing_token=2 WHERE id=$1",
		"UPDATE photo.tasks SET cancel_requested=true WHERE id=$1",
		"UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE created_by_task_id=$1",
		"UPDATE photo.artifacts SET verified_at=NULL WHERE created_by_task_id=$1",
	} {
		tx, e := admin.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, mutation, task); e != nil {
			tx.Rollback(ctx)
			t.Fatal(e)
		}
		// Service owns another connection: commit the mutation for its observation,
		// then restore only the explicitly changed mutable fixture metadata.
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		before := signer.calls
		items, e = service.Outputs(ctx, who, tid, runID)
		if e != nil || len(items) != 0 || signer.calls != before {
			t.Fatal("stale/unverified output signed", e)
		}
		if _, e = admin.Exec(ctx, "UPDATE photo.tasks SET fencing_token=1,cancel_requested=false WHERE id=$1", task); e != nil {
			t.Fatal(e)
		}
		if _, e = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL,verified_at=now() WHERE created_by_task_id=$1", task); e != nil {
			t.Fatal(e)
		}
	}
	// A separate active viewer must have a grant to this restricted project.
	var viewer uuid.UUID
	var viewerEpoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@test.example").Scan(&viewer, &viewerEpoch); err != nil {
		t.Fatal(err)
	}
	viewerSession := uuid.New()
	if _, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')", viewerSession, viewer, auth.HashSecret(uuid.NewString()), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'viewer','active')", tid, viewer); err != nil {
		t.Fatal(err)
	}
	viewerWho := auth.Identity{UserID: viewer, SessionID: viewerSession, Epoch: viewerEpoch}
	_, err = service.Outputs(ctx, viewerWho, tid, runID)
	if domain.AsError(err).Code != "NOT_FOUND" {
		t.Fatal("restricted project leaked", err)
	}
	if _, err = admin.Exec(ctx, "INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'viewer')", tid, projectID, viewer); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Outputs(ctx, viewerWho, tid, runID); err != nil {
		t.Fatal("granted viewer denied", err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.projects SET archived_at=now() WHERE tenant_id=$1 AND id=$2", tid, projectID); err != nil {
		t.Fatal(err)
	}
	_, err = service.Outputs(ctx, who, tid, runID)
	if domain.AsError(err).Code != "NOT_FOUND" {
		t.Fatal("archived project exposed", err)
	}
	if signer.calls != 2 {
		t.Fatal("scope denial unexpectedly signed objects")
	}
}
