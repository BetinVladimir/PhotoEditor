package v2workflow

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
)

func TestIntegrationGetAndCancelWorkflow(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with V21 migration required")
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

	var userID, tenantID, projectID, snapshotID, runID, workflowID uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@test.example").Scan(&userID, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", userID).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.New()
	if _, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')", sessionID, userID, auth.HashSecret(uuid.NewString()), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.projects(tenant_id,name) VALUES($1,'V2 integration') RETURNING id", tenantID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	if err = admin.QueryRow(ctx, "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'v2','user',$2,$3,0) RETURNING id", tenantID, userID, hash).Scan(&snapshotID); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]string{"project_id": projectID.String()})
	if err = admin.QueryRow(ctx, "INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES($1,'v2','running',$2,$3,$4,$5,'fail_fast',true) RETURNING id", tenantID, snapshotID, config, hash, userID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES($1,$2,$3,$4,'session','v2.2',$5) RETURNING id", tenantID, projectID, runID, snapshotID, hash).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	service := Service{DB: api}
	who := auth.Identity{UserID: userID, SessionID: sessionID, Epoch: epoch}
	before, err := service.Get(ctx, who, tenantID, workflowID)
	if err != nil || before.RunID != runID || before.State != "running" || before.Revision != "1" || len(before.Outputs) != 0 {
		t.Fatal("get workflow", before, err)
	}
	key := uuid.NewString()
	first, err := service.Cancel(ctx, who, tenantID, workflowID, key, CancelRequest{ExpectedRevision: "1"})
	if err != nil || first.Status != 200 {
		t.Fatal("first cancel", first, err)
	}
	var cancelled Status
	if err = json.Unmarshal(first.Body, &cancelled); err != nil || cancelled.State != "cancelling" || cancelled.Revision != "2" {
		t.Fatal("cancel response", cancelled, err)
	}
	replay, err := service.Cancel(ctx, who, tenantID, workflowID, key, CancelRequest{ExpectedRevision: "1"})
	if err != nil || string(replay.Body) != string(first.Body) {
		t.Fatal("idempotent replay", replay, err)
	}
	if _, err = service.Cancel(ctx, who, tenantID, workflowID, uuid.NewString(), CancelRequest{ExpectedRevision: "1"}); err == nil {
		t.Fatal("stale revision was accepted")
	}
	var audits int
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='workflow.cancel_requested'", tenantID, workflowID).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("audit cardinality", audits, err)
	}
	if _, err = service.Get(ctx, who, tenantID, uuid.New()); err == nil {
		t.Fatal("unknown workflow was visible")
	}
}
