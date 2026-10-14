package run

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/pagination"
)

func TestIntegrationCancelRunIsAuthorizedAuditedAndIdempotent(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with migration74 required")
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

	var user, tenantID, snapshot, runID uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@test.example").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	session := uuid.New()
	if _, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')", session, user, auth.HashSecret(uuid.NewString()), uuid.New()); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	if err = admin.QueryRow(ctx, "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,1) RETURNING id", tenantID, user, hash).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES($1,'apply','running',$2,'{}',$3,$4) RETURNING id", tenantID, snapshot, hash, user).Scan(&runID); err != nil {
		t.Fatal(err)
	}

	service := Service{DB: api, Cursors: pagination.Codec{Key: []byte(strings.Repeat("k", 32))}}
	who := auth.Identity{UserID: user, SessionID: session, Epoch: epoch}
	first, err := service.Cancel(ctx, who, tenantID, runID)
	if err != nil || first.State != "cancelling" || first.Revision != "2" {
		t.Fatal("first cancel", first, err)
	}
	second, err := service.Cancel(ctx, who, tenantID, runID)
	if err != nil || second.Revision != "2" {
		t.Fatal("idempotent cancel", second, err)
	}
	var audits int
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='run.cancel_requested'", tenantID, runID).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("audit cardinality", audits, err)
	}
	page, err := service.List(ctx, who, tenantID, 1, "", "cancelling")
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != runID || page.Items[0].State != "cancelling" {
		t.Fatal("filtered run list", page, err)
	}
	var project uuid.UUID
	if err := admin.QueryRow(ctx, "INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Batch cancellation fixture') RETURNING id", tenantID).Scan(&project); err != nil {
		t.Fatal(err)
	}
	batch := uuid.New()
	child := uuid.NewSHA1(batch, []byte("render.batch:0"))
	manifest, _ := json.Marshal(map[string]any{"schema_version": 1, "project_id": project, "input_snapshot_id": snapshot, "auth_session_id": session, "policy_sha": hash, "parts": []any{map[string]any{"run_id": child, "first_ordinal": 0, "frame_count": 1, "config_sha": hash, "output_bytes": 1}}})
	childConfig, _ := json.Marshal(map[string]any{"project_id": project, "v26_render": map[string]any{"auth_session_id": session}})
	if _, err := admin.Exec(ctx, "INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES($1,$2,'bulk','draft',$3,$4,$5,$6)", batch, tenantID, snapshot, string(manifest), hash, user); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES($1,$2,'v2','draft',$3,$4,$5,$6,'fail_fast')", child, tenantID, snapshot, string(childConfig), hash, user); err != nil {
		t.Fatal(err)
	}
	cancelledBatch, err := service.Cancel(ctx, who, tenantID, batch)
	if err != nil || cancelledBatch.State != "cancelling" || cancelledBatch.Revision != "2" || cancelledBatch.Counts.Total != 1 || cancelledBatch.Progress != 0 {
		t.Fatal("API batch cancel failed", cancelledBatch, err)
	}
	againBatch, err := service.Cancel(ctx, who, tenantID, batch)
	if err != nil || againBatch.Revision != "2" {
		t.Fatal("API batch replay changed revision", againBatch, err)
	}
	batchStatus, err := service.Get(ctx, who, tenantID, batch)
	if err != nil || batchStatus.Counts.Total != 1 || batchStatus.Progress != 0 || batchStatus.Stage != "cancelling" {
		t.Fatal("API batch get lost planned frame count", batchStatus, err)
	}
	listed, err := service.List(ctx, who, tenantID, 100, "", "cancelling")
	if err != nil {
		t.Fatal("API batch list failed", err)
	}
	foundBatch := false
	for _, item := range listed.Items {
		if item.ID == batch {
			foundBatch = true
			if item.Counts.Total != 1 || item.Progress != 0 {
				t.Fatal("API batch list lost counts", item)
			}
		}
	}
	if !foundBatch {
		t.Fatal("API batch missing from list")
	}
	var childStatus string
	var childCancel bool
	if err := admin.QueryRow(ctx, "SELECT status,cancel_requested FROM photo.runs WHERE id=$1", child).Scan(&childStatus, &childCancel); err != nil || childStatus != "cancelling" || !childCancel {
		t.Fatal("API batch did not propagate cancellation", childStatus, childCancel, err)
	}
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='run.cancel_requested'", tenantID, batch).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("batch audit cardinality", audits, err)
	}

	// A finalized batch rejects retry atomically, including the task reset performed first.
	if _, err := admin.Exec(ctx, "UPDATE photo.runs SET status='failed' WHERE id=ANY($1::uuid[])", []uuid.UUID{batch, child}); err != nil {
		t.Fatal(err)
	}
	var params uuid.UUID
	if err := admin.QueryRow(ctx, "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES($1,'test',$2,'v1',$3,64,'application/json','document',now()) RETURNING id", tenantID, uuid.NewString(), hash).Scan(&params); err != nil {
		t.Fatal(err)
	}
	task := uuid.New()
	if _, err := admin.Exec(ctx, "INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,terminal_reason) VALUES($1,$2,$3,'render.compose:0','render.compose','1','gpu','failed','[]',$4,$5,'{}',1,1,'fixture_failure')", task, tenantID, child, params, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile,error) VALUES($1,$2,1,1,$3,now(),now(),'cuda','{}','{\"retryable\":true}')", tenantID, task, uuid.New()); err != nil {
		t.Fatal(err)
	}
	before, err := service.Get(ctx, who, tenantID, child)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Retry(ctx, who, tenantID, child, uuid.NewString(), RetryRequest{ExpectedRevision: "2", TaskIDs: []uuid.UUID{task}})
	if d := domain.AsError(err); d.Code != "RUN_NOT_RETRYABLE" {
		t.Fatal("terminal batch retry conflict", err)
	}
	after, err := service.Get(ctx, who, tenantID, child)
	if err != nil || after.State != before.State || after.Revision != before.Revision {
		t.Fatal("rejected retry changed run", after, err)
	}
	var state string
	if err := admin.QueryRow(ctx, "SELECT state FROM photo.tasks WHERE id=$1", task).Scan(&state); err != nil || state != "failed" {
		t.Fatal("rejected retry changed task", state, err)
	}
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.audit_log WHERE resource_id=$1 AND action='run.retry'", child).Scan(&audits); err != nil || audits != 0 {
		t.Fatal("rejected retry audit", audits, err)
	}

	var allowed, excessive bool
	if err := admin.QueryRow(ctx, "SELECT has_column_privilege('photo_api','photo.task_attempts','error','SELECT'),has_column_privilege('photo_api','photo.task_attempts','profile','SELECT')").Scan(&allowed, &excessive); err != nil || !allowed || excessive {
		t.Fatal("retry attempt column grants", allowed, excessive, err)
	}

	scoped, err := api.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer scoped.Rollback(ctx)
	if _, err := scoped.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := scoped.QueryRow(ctx, "SELECT count(task_id) FROM photo.task_attempts WHERE task_id=$1", task).Scan(&visible); err != nil || visible != 0 {
		t.Fatal("retry attempt tenant isolation", visible, err)
	}
	if err := scoped.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// Reopening only the fixture parent permits ordinary retry while the batch is active.
	if _, err := admin.Exec(ctx, "UPDATE photo.runs SET status='draft',cancel_requested=false WHERE id=$1", batch); err != nil {
		t.Fatal(err)
	}
	retryKey := uuid.NewString()
	retried, err := service.Retry(ctx, who, tenantID, child, retryKey, RetryRequest{ExpectedRevision: "2", TaskIDs: []uuid.UUID{task}})
	if err != nil || retried.State != "running" || retried.Revision != "3" {
		t.Fatal("active batch child retry", retried, err)
	}
	replayed, err := service.Retry(ctx, who, tenantID, child, retryKey, RetryRequest{ExpectedRevision: "2", TaskIDs: []uuid.UUID{task}})
	if err != nil || replayed.ID != retried.ID || replayed.Revision != retried.Revision || replayed.State != retried.State {
		t.Fatal("retry replay", replayed, err)
	}
	if _, err := service.Retry(ctx, who, tenantID, child, retryKey, RetryRequest{ExpectedRevision: "3", TaskIDs: []uuid.UUID{task}}); domain.AsError(err).Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatal("retry key reused with different request", err)
	}
	if err := admin.QueryRow(ctx, "SELECT state FROM photo.tasks WHERE id=$1", task).Scan(&state); err != nil || state != "ready" {
		t.Fatal("retry task readiness", state, err)
	}
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.audit_log WHERE resource_id=$1 AND action='run.retry'", child).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("successful retry audit", audits, err)
	}

	if _, err := admin.Exec(ctx, "UPDATE photo.runs SET graph_sealed=true WHERE id=$1", child); err != nil {
		t.Fatal(err)
	}
	schedulerTx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer schedulerTx.Rollback(ctx)
	if _, err := schedulerTx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	var changed bool
	if err := schedulerTx.QueryRow(ctx, "SELECT photo.sync_v26_render_batch_state()").Scan(&changed); err != nil || !changed {
		t.Fatal("batch durable phase", changed, err)
	}
	if err := schedulerTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	running, err := service.List(ctx, who, tenantID, 100, "", "running")
	if err != nil {
		t.Fatal(err)
	}
	foundRunningBatch := false
	for _, item := range running.Items {
		if item.ID == batch {
			foundRunningBatch = true
			if item.State != "running" || item.Stage != "running" || item.Revision != "3" {
				t.Fatal("durable running batch projection", item)
			}
		}
	}
	if !foundRunningBatch {
		t.Fatal("status-filtered API list omitted active batch")
	}

}
