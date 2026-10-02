package httpapi

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"testing"
)

// Synthetic verified ingest metadata; no storage verification is claimed here.
func captureVerifiedReceiptFixture(t *testing.T, db *pgxpool.Pool, tenant, project, device uuid.UUID) (session, collection, upload uuid.UUID, sha string) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	scan := func(q string, dst any, args ...any) {
		t.Helper()
		if err := tx.QueryRow(ctx, q, args...).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	var actor, artifact, asset, version, snapshot, run, reservation uuid.UUID
	sha = strings.Repeat("c", 64)
	upload = uuid.New()
	scan(`SELECT owner_user_id FROM photo.tenants WHERE id=$1`, &actor, tenant)
	scan(`INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Agent receipt') RETURNING id`, &collection, tenant, project)
	scan(`INSERT INTO photo.shoot_sessions(tenant_id,project_id,collection_id,name,state,policy,consent_epoch,capture_policy) VALUES($1,$2,$3,'Agent receipt','open','{}',1,'{}') RETURNING id`, &session, tenant, project, collection)
	scan(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES($1,'test',$2,$3,3,'image/jpeg','v1',now()) RETURNING id`, &artifact, tenant, "agent-fixture/"+uuid.NewString(), sha)
	scan(`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`, &asset, tenant, project, artifact)
	scan(`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`, &version, tenant, asset, artifact, sha)
	scan(`INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'v2','user',$2,$3,0) RETURNING id`, &snapshot, tenant, actor, sha)
	scan(`INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES($1,'v2','completed',$2,jsonb_build_object('project_id',$3::text),$4,$5,'fail_fast',true) RETURNING id`, &run, tenant, snapshot, project, sha, actor)
	scan(`INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at) VALUES($1,'device',$2,'upload',$3,3,1,'consumed',now()+interval '1 hour') RETURNING id`, &reservation, tenant, device, upload)
	must(`INSERT INTO photo.upload_sessions(id,tenant_id,project_id,collection_id,principal_type,principal_id,expected_size,actual_size,filename,object_key,state,expires_at,verified_version_id,reservation_id,ingest_run_id,staging_candidate_id,ingest_snapshot_id,capture_session_id,capture_credential_epoch) VALUES($1,$2,$3,$4,'device',$5,3,3,'frame.jpg',$6,'verified',now()+interval '1 hour',$7,$8,$9,$11,$12,$10,1)`, upload, tenant, project, collection, device, "agent-upload/"+uuid.NewString(), version, reservation, run, session, artifact, snapshot)
	must(`INSERT INTO photo.ingest_upload_bindings(tenant_id,run_id,ordinal,upload_id,source_id,source_sha,project_id,collection_id,principal_type,principal_id,asset_id,version_id) VALUES($1,$2,0,$3,$4,$5,$6,$7,'device',$8,$9,$10)`, tenant, run, upload, artifact, sha, project, collection, device, asset, version)
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return
}
