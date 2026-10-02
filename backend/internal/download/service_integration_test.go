package download

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

func TestIntegrationOwnerSnapshotAndCurrentPolicyFence(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	var user, tenantID, projectID, sourceArtifact, assetID, versionID, collectionID, itemID, snapshotID, presetID uuid.UUID
	var epoch int64
	digest := "a" + "000000000000000000000000000000000000000000000000000000000000000"
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
	if err = admin.QueryRow(ctx, "INSERT INTO photo.projects(tenant_id,name,visibility,download_allowed) VALUES($1,'Download','workspace',true) RETURNING id", tenantID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role)
		VALUES($1,'photo',$2,'source-v1',$3,8,'image/jpeg','working_image') RETURNING id`, tenantID, "sources/"+uuid.NewString(), digest).Scan(&sourceArtifact); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id", tenantID, projectID, sourceArtifact).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id", tenantID, assetID, sourceArtifact, digest).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.collections(tenant_id,project_id,name,state) VALUES($1,$2,'Source','draft') RETURNING id", tenantID, projectID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position) VALUES($1,$2,$3,$4,0) RETURNING id", tenantID, collectionID, assetID, versionID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count,expires_at)
		VALUES($1,'selection',$2,1,'user',$3,$4,1,clock_timestamp()+interval '1 hour') RETURNING id`, tenantID, collectionID, user, digest).Scan(&snapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload)
		VALUES($1,$2,0,$3,$4,$5,$6,'{}')`, tenantID, snapshotID, itemID, versionID, sourceArtifact, digest); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.export_presets WHERE tenant_id=$1 ORDER BY name LIMIT 1", tenantID).Scan(&presetID); err != nil {
		t.Fatal(err)
	}

	pepper := []byte("download-integration-pepper-32bytes")
	service := Service{DB: api, Pepper: pepper}
	who := auth.Identity{UserID: user, SessionID: sessionID, Epoch: epoch}
	accepted, err := service.Start(ctx, who, tenantID, "start-download", Request{SelectionSnapshotID: snapshotID, PresetID: presetID})
	if err != nil || accepted.Status != "queued" {
		t.Fatal(accepted, err)
	}
	var runID uuid.UUID
	var savedSnapshot uuid.UUID
	var savedDigest string
	if err = admin.QueryRow(ctx, `SELECT j.run_id,j.snapshot_id,r.config_snapshot->>'input_snapshot_digest'
		FROM photo.download_jobs j JOIN photo.runs r ON r.id=j.run_id WHERE j.id=$1`, accepted.DownloadJobID).Scan(&runID, &savedSnapshot, &savedDigest); err != nil {
		t.Fatal(err)
	}
	if savedSnapshot != snapshotID || savedDigest != digest {
		t.Fatal("download did not freeze exact snapshot")
	}

	// Later collection additions never enter the frozen download input.
	var secondAsset, secondVersion uuid.UUID
	if err = admin.QueryRow(ctx, "INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id", tenantID, projectID, sourceArtifact).Scan(&secondAsset); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id", tenantID, secondAsset, sourceArtifact, digest).Scan(&secondVersion); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position) VALUES($1,$2,$3,$4,1)", tenantID, collectionID, secondAsset, secondVersion); err != nil {
		t.Fatal(err)
	}
	var frozenCount int
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.snapshot_items WHERE tenant_id=$1 AND snapshot_id=$2", tenantID, snapshotID).Scan(&frozenCount); err != nil || frozenCount != 1 {
		t.Fatalf("snapshot changed after collection addition: %d %v", frozenCount, err)
	}

	var archiveID uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,expires_at)
		VALUES($1,'photo',$2,'archive-v1',$3,16,'application/zip','archive',clock_timestamp()+interval '1 hour') RETURNING id`, tenantID, "downloads/"+uuid.NewString(), digest).Scan(&archiveID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.download_job_items(tenant_id,download_job_id,ordinal,asset_version_id,artifact_id,sha256,entry_name,volume_no,bytes,delivery_kind)
		VALUES($1,$2,0,$3,$4,$5,'source.jpg',1,16,'zip')`, tenantID, accepted.DownloadJobID, versionID, archiveID, digest); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.download_volumes(tenant_id,download_job_id,volume_no,artifact_id,status,files_done,bytes_written)
		VALUES($1,$2,1,$3,'ready',1,16)`, tenantID, accepted.DownloadJobID, archiveID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.download_jobs SET status='ready',expires_at=clock_timestamp()+interval '1 hour' WHERE id=$1", accepted.DownloadJobID); err != nil {
		t.Fatal(err)
	}
	intent, err := service.CreateIntent(ctx, who, tenantID, accepted.DownloadJobID, "intent-before-removal", IntentRequest{Volume: 1})
	if err != nil || intent.Token == "" {
		t.Fatal(intent, err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.download_jobs SET status='expired',expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", accepted.DownloadJobID); err != nil {
		t.Fatal(err)
	}
	replacement, err := service.Start(ctx, who, tenantID, "start-after-expiry", Request{SelectionSnapshotID: snapshotID, PresetID: presetID})
	if err != nil || replacement.DownloadJobID == accepted.DownloadJobID || replacement.Status != "queued" {
		t.Fatalf("expired job was not replaceable: %+v %v", replacement, err)
	}
	if _, err = admin.Exec(ctx, "UPDATE photo.download_jobs SET status='ready',expires_at=clock_timestamp()+interval '1 hour' WHERE id=$1", accepted.DownloadJobID); err != nil {
		t.Fatal(err)
	}

	// Removal does not rewrite the snapshot or archive, but blocks new intent.
	if _, err = admin.Exec(ctx, "DELETE FROM photo.collection_items WHERE id=$1", itemID); err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateIntent(ctx, who, tenantID, accepted.DownloadJobID, "intent-after-removal", IntentRequest{Volume: 1})
	if domain.AsError(err).Code != "DOWNLOAD_REVOKED" {
		t.Fatalf("removed item remained downloadable: %v", err)
	}
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.snapshot_items WHERE snapshot_id=$1", snapshotID).Scan(&frozenCount); err != nil || frozenCount != 1 {
		t.Fatalf("removal rewrote snapshot: %d %v", frozenCount, err)
	}

	// An already issued bearer is also fenced by current project policy.
	if _, err = admin.Exec(ctx, "UPDATE photo.projects SET download_allowed=false WHERE id=$1", projectID); err != nil {
		t.Fatal(err)
	}
	var outcome string
	if err = admin.QueryRow(ctx, "SELECT outcome FROM photo.redeem_download_intent($1,'photo')", auth.MAC(pepper, "portal-download-intent", intent.Token)).Scan(&outcome); err != nil || outcome != "revoked" {
		t.Fatalf("current policy did not revoke bearer: %s %v", outcome, err)
	}
}
