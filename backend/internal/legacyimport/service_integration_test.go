package legacyimport

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/upload"
)

type multipartStore struct{}

func (multipartStore) BeginMultipart(context.Context, string, string) (string, error) {
	return "legacy-multipart", nil
}
func (multipartStore) AbortMultipart(context.Context, string, string) error { return nil }
func (multipartStore) SignUploadParts(context.Context, string, string, []int, time.Duration) ([]objectstore.PartURL, error) {
	return nil, nil
}

func TestIntegrationReconcileVerifiedUploadIsReplayable(t *testing.T) {
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
	var user, tenantID, projectID uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@test.example").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	// Keep the actual tenant owner intact. This caller is a removable Admin,
	// so demotion leaves valid tenant Editor authority but no restricted ACL.
	if err=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&user,&epoch);err!=nil{t.Fatal(err)}
	if _,err=admin.Exec(ctx,`INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'admin','active')`,tenantID,user);err!=nil{t.Fatal(err)}
	sessionID := uuid.New()
	if _, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')", sessionID, user, auth.HashSecret(uuid.NewString()), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Legacy','workspace') RETURNING id", tenantID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: user, SessionID: sessionID, Epoch: epoch}
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	uploaded, err := (upload.Service{DB: api, Objects: multipartStore{}}).Begin(ctx, who, tenantID, "legacy-upload", upload.BeginRequest{ProjectID: projectID, Filename: "old.jpg", Bytes: 10, Format: "jpeg", SHA256: &digest})
	if err != nil {
		t.Fatal(err)
	}
	artifactID, assetID, versionID := uuid.New(), uuid.New(), uuid.New()
	if _, err = admin.Exec(ctx, `INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type) VALUES($1,$2,'photo','legacy/master','v1',$3,10,'image/jpeg')`, artifactID, tenantID, digest); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.assets(id,tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,$4,'{}','verified','{}')`, assetID, tenantID, projectID, artifactID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,$5,'{}','accepted')`, versionID, tenantID, assetID, artifactID, digest); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `UPDATE photo.upload_sessions SET state='verified',verified_version_id=$1 WHERE tenant_id=$2 AND id=$3`, versionID, tenantID, uploaded.ID); err != nil {
		t.Fatal(err)
	}
	request := ReconcileRequest{PlanHash: digest, SourceRelative: "input/old.jpg", SourceSHA256: digest, UploadID: uploaded.ID, Provenance: json.RawMessage(`{"kind":"source"}`)}
	first, err := (Service{DB: api}).Reconcile(ctx, who, tenantID, request)
	if err != nil || first.AssetVersionID != versionID {
		t.Fatal(first, err)
	}
	second, err := (Service{DB: api}).Reconcile(ctx, who, tenantID, request)
	if err != nil || second.ID != first.ID || second.AssetVersionID != versionID {
		t.Fatal(second, err)
	}
	var count int
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.legacy_imports WHERE tenant_id=$1", tenantID).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	blocker, err := admin.Begin(ctx)
	if err != nil { t.Fatal(err) };defer blocker.Rollback(ctx)
	if _,err=admin.Exec(ctx,`UPDATE photo.projects SET visibility='restricted' WHERE tenant_id=$1 AND id=$2`,tenantID,projectID);err!=nil{t.Fatal(err)}
	var blockerPID int32
	if err=blocker.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&blockerPID);err!=nil{t.Fatal(err)}
	if _,err=blocker.Exec(ctx,`SELECT id FROM photo.upload_sessions WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,tenantID,uploaded.ID);err!=nil{t.Fatal(err)}
	pending:=request;pending.SourceRelative="input/after-wait.jpg"
	done:=make(chan error,1)
	go func(){_,e:=(Service{DB:api}).Reconcile(ctx,who,tenantID,pending);done<-e}()
	tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop()
	for {var waiting bool
		if err=admin.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_stat_activity a WHERE $1=ANY(pg_blocking_pids(a.pid)))`,blockerPID).Scan(&waiting);err!=nil{t.Fatal(err)}
		if waiting{break};select{case e:=<-done:t.Fatal("reconcile did not wait on upload",e);case <-ctx.Done():t.Fatal(ctx.Err());case <-tick.C:}
	}
	if _,err=admin.Exec(ctx,`UPDATE photo.memberships SET role='editor' WHERE tenant_id=$1 AND user_id=$2`,tenantID,user);err!=nil{t.Fatal(err)}
	if err=blocker.Commit(ctx);err!=nil{t.Fatal(err)}
	if err=<-done;err==nil||domain.AsError(err).HTTPStatus<400{t.Fatal("withdrawn authority reconciled upload",err)}
	if err=admin.QueryRow(ctx,`SELECT count(*) FROM photo.legacy_imports WHERE tenant_id=$1`,tenantID).Scan(&count);err!=nil||count!=1{t.Fatal("late refusal wrote a record",count,err)}
}
