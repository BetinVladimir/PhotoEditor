package provenance

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// Authored SQL lifecycle fixture; requires isolated DB migrated through01405.
// Rollback only. Does not prove physical object storage or native signing.
func TestIntegrationSigningPolicyPublicationLifecycle(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	exec := func(t *testing.T, query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	scan := func(t *testing.T, query string, dst any, args ...any) {
		t.Helper()
		if err := tx.QueryRow(ctx, query, args...).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	var user, tid, session, project, snapshot, run, params, task, source, result, asset, version, export uuid.UUID
	var epoch int64
	if err := tx.QueryRow(ctx, `SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`, uuid.NewString()+"@lineage.test").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	scan(t, `SELECT id FROM photo.tenants WHERE owner_user_id=$1`, &tid, user)
	session = uuid.New()
	exec(t, `INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`, session, user, auth.HashSecret(uuid.NewString()), uuid.New())
	exec(t, `UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','1000000'::jsonb) WHERE id=$1`, tid)
	scan(t, `INSERT INTO photo.projects(tenant_id,name,visibility,download_allowed) VALUES($1,'Lineage SQL fixture','restricted',true) RETURNING id`, &project, tid)
	sha := strings.Repeat("b", 64)
	scan(t, `INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,1) RETURNING id`, &snapshot, tid, user, sha)
	scan(t, `INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES($1,'preview','completed',$2,'{}',$3,$4) RETURNING id`, &run, tid, snapshot, sha, user)
	scan(t, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,state) VALUES($1,'test-bucket',$2,$3,2,'application/json','params-v1',now(),'ready') RETURNING id`, &params, tid, uuid.NewString(), sha)
	scan(t, `INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES($1,$2,'media.preview:0','media.preview','1','media','ready','[]',$3,$4,1,1,'{"renderer_version":"v2.2"}') RETURNING id`, &task, tid, run, params, sha)
	exec(t, `INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES($1,$2,1,1,$3,now(),now(),'cpu','{}')`, tid, task, uuid.New())
	sourceKey := "lineage-source/" + uuid.NewString()
	scan(t, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence,state) VALUES($1,'test-bucket',$2,'source-v1',$3,42,'image/jpeg','derivative',now(),$4,1,1,'ready') RETURNING id`, &source, tid, sourceKey, sha, task)
	scan(t, `INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES($1,$2,1,1,jsonb_build_array(jsonb_build_object('artifact_id',$3::text,'sha256',$4::text)),'{}',$4) RETURNING id`, &result, tid, task, source, sha)
	exec(t, `UPDATE photo.tasks SET state='succeeded',canonical_result_id=$1 WHERE id=$2`, result, task)
	scan(t, `INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`, &asset, tid, project, source)
	scan(t, `INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state,created_by_run_id) VALUES($1,$2,$3,$4,'{}','accepted',$5) RETURNING id`, &version, tid, asset, source, sha, run)
	scan(t, `INSERT INTO photo.exports(tenant_id,asset_version_id,preset_hash,artifact_id,status,task_id,metadata_policy) VALUES($1,$2,$3,$4,'ready',$5,'{}') RETURNING id`, &export, tid, version, sha, source, task)
	exec(t, `SET CONSTRAINTS ALL IMMEDIATE`)

	config, paramsPolicy, _ := signingRegistryFixture(t)
	registry, err := NewSigningPolicyRegistry(config, &signingAuthorityStub{})
	if err != nil {
		t.Fatal(err)
	}
	registry, err = registry.ForTenant(tid)
	if err != nil {
		t.Fatal(err)
	}
	installed := registry.policies[paramsPolicy.PolicyID]
	body, expected := installed.body, installed.ref
	who := auth.Identity{UserID: user, SessionID: session, Epoch: epoch}
	ready := ReadySigningExport{Tenant: tid, Project: project, ExportID: export, Version: version, Task: task, Run: run, Ref: v2input.Ref{ArtifactID: source, SHA256: sha}, Object: objectstore.Object{Key: sourceKey, VersionID: "source-v1", SHA256: sha, Bytes: 42, ContentType: "image/jpeg"}}
	_, pins, err := (SQLSigningPolicyStore{Bucket: "test-bucket"}).pins(who, ready, expected, body)
	if err != nil {
		t.Fatal(err)
	}
	reserve := func() (string, uuid.UUID, uuid.UUID) {
		t.Helper()
		var outcome string
		var id, token, owner *uuid.UUID
		var replay bool
		if err := tx.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready,storage_run FROM photo.reserve_v31_signing_policy($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,'test-bucket')`, tid, user, session, export, string(pins), paramsPolicy.PolicyID, expected.ArtifactID, expected.SHA256, int64(len(body))).Scan(&outcome, &id, &token, &replay, &owner); err != nil {
			t.Fatal(err)
		}
		if id == nil || *id != expected.ArtifactID || token == nil || owner == nil || *owner != run || replay != (outcome == "replayed") {
			t.Fatal("invalid reservation receipt", outcome)
		}
		return outcome, *token, *owner
	}
	complete := func(token, owner uuid.UUID, version string) bool {
		t.Helper()
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.complete_v31_signing_policy($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,'test-bucket',$10,$11,$12)`, tid, user, session, export, string(pins), paramsPolicy.PolicyID, expected.ArtifactID, expected.SHA256, int64(len(body)), token, owner, version).Scan(&accepted); err != nil {
			t.Fatal(err)
		}
		return accepted
	}
	var baseStored, baseReserved int64
	if err := tx.QueryRow(ctx, `SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1`, tid).Scan(&baseStored, &baseReserved); err != nil {
		t.Fatal(err)
	}
	outcome, first, owner := reserve()
	if outcome != "created" {
		t.Fatal("first reservation", outcome)
	}
	outcome, second, _ := reserve()
	if outcome != "resumed" || first == second {
		t.Fatal("fence not renewed", outcome)
	}
	if complete(first, owner, "policy-v1") || complete(second, uuid.New(), "policy-v1") {
		t.Fatal("stale fence or foreign owner accepted")
	}
	if !complete(second, owner, "policy-v1") {
		t.Fatal("valid completion refused")
	}
	outcome, replayed, _ := reserve()
	if outcome != "replayed" || replayed != second {
		t.Fatal("ready replay changed receipt")
	}
	if !complete(second, owner, "policy-v1") || complete(second, owner, "policy-v2") {
		t.Fatal("completion replay not exact")
	}
	var accepted, retained bool
	if err := tx.QueryRow(ctx, `SELECT photo.v31_signing_policy_ready($1,$2,$3,$4),photo.v2_artifact_has_references($1,$3)`, tid, paramsPolicy.PolicyID, expected.ArtifactID, expected.SHA256).Scan(&accepted, &retained); err != nil {
		t.Fatal(err)
	}
	if !accepted || !retained {
		t.Fatal("published policy unavailable or unretained")
	}

	// A different canonical export/run must reuse the first policy storage owner.
	var nextRun, nextTask, nextSource, nextResult, nextVersion, nextExport uuid.UUID
	scan(t, `INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES($1,'preview','completed',$2,'{}',$3,$4) RETURNING id`, &nextRun, tid, snapshot, sha, user)
	scan(t, `INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES($1,$2,'media.preview:0','media.preview','1','media','ready','[]',$3,$4,1,1,'{"renderer_version":"v2.2"}') RETURNING id`, &nextTask, tid, nextRun, params, sha)
	exec(t, `INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES($1,$2,1,1,$3,now(),now(),'cpu','{}')`, tid, nextTask, uuid.New())
	nextKey := "policy-next-source/" + uuid.NewString()
	scan(t, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence,state) VALUES($1,'test-bucket',$2,'next-source-v1',$3,42,'image/jpeg','derivative',now(),$4,1,1,'ready') RETURNING id`, &nextSource, tid, nextKey, sha, nextTask)
	scan(t, `INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES($1,$2,1,1,jsonb_build_array(jsonb_build_object('artifact_id',$3::text,'sha256',$4::text)),'{}',$4) RETURNING id`, &nextResult, tid, nextTask, nextSource, sha)
	exec(t, `UPDATE photo.tasks SET state='succeeded',canonical_result_id=$1 WHERE id=$2`, nextResult, nextTask)
	scan(t, `INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state,created_by_run_id) VALUES($1,$2,$3,$4,'{}','accepted',$5) RETURNING id`, &nextVersion, tid, asset, nextSource, sha, nextRun)
	scan(t, `INSERT INTO photo.exports(tenant_id,asset_version_id,preset_hash,artifact_id,status,task_id,metadata_policy) VALUES($1,$2,$3,$4,'ready',$5,'{}') RETURNING id`, &nextExport, tid, nextVersion, sha, nextSource, nextTask)
	exec(t, `SET CONSTRAINTS ALL IMMEDIATE`)
	ready.Run, ready.Task, ready.Version, ready.ExportID = nextRun, nextTask, nextVersion, nextExport
	ready.Ref.ArtifactID = nextSource
	ready.Object.Key, ready.Object.VersionID = nextKey, "next-source-v1"
	_, pins, err = (SQLSigningPolicyStore{Bucket: "test-bucket"}).pins(who, ready, expected, body)
	if err != nil {
		t.Fatal(err)
	}
	export = nextExport
	outcome, replayed, owner = reserve()
	if outcome != "replayed" || replayed != second || owner != run || owner == nextRun {
		t.Fatal("cross-export policy changed storage identity")
	}
	if !complete(second, owner, "policy-v1") || complete(second, nextRun, "policy-v1") {
		t.Fatal("cross-export completion changed storage owner")
	}
	var stored, reserved int64
	if err := tx.QueryRow(ctx, `SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1`, tid).Scan(&stored, &reserved); err != nil {
		t.Fatal(err)
	}
	if stored != baseStored+int64(len(body)) || reserved != baseReserved {
		t.Fatal("quota charged more than once", stored, reserved)
	}
	exec(t, `UPDATE photo.artifacts SET output_role='document' WHERE tenant_id=$1 AND id=$2`, tid, expected.ArtifactID)
	scan(t, `SELECT photo.v31_signing_policy_ready($1,$2,$3,$4)`, &accepted, tid, paramsPolicy.PolicyID, expected.ArtifactID, expected.SHA256)
	if accepted {
		t.Fatal("wrong policy role accepted")
	}
}
