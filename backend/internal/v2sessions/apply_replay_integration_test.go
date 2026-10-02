package v2sessions

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

// A sealed Analysis Apply replay must work with only relational state. In
// particular it cannot require the original plan/correction object reads.
func TestIntegrationAnalysisApplyReplay(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with V22 migrations required")
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
	var actor, tenantID, projectID, collectionID, sessionID uuid.UUID
	var snapshotID, parentID, workflowID, analysisID, artifactID uuid.UUID
	var childID, authSession uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, `SELECT user_id,auth_epoch FROM
	 photo.auth_bootstrap_user($1,'en',true)`, uuid.NewString()+"@test.example").
		Scan(&actor, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `SELECT id FROM photo.tenants
	 WHERE owner_user_id=$1`, actor).Scan(&tenantID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.sessions
	 (user_id,refresh_hash,family_id,client_type,expires_at)
	 VALUES($1,$2,$3,'web',now()+interval '1 day') RETURNING id`,
		actor, auth.HashSecret(uuid.NewString()), uuid.New()).Scan(&authSession); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.projects(tenant_id,name)
	 VALUES($1,'Apply replay') RETURNING id`, tenantID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.collections
	 (tenant_id,project_id,name) VALUES($1,$2,'Source') RETURNING id`,
		tenantID, projectID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.shoot_sessions
	 (tenant_id,project_id,collection_id,name,state,policy,consent_epoch)
	 VALUES($1,$2,$3,'Shoot','open','{}',1) RETURNING id`,
		tenantID, projectID, collectionID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.snapshots
	 (tenant_id,kind,collection_id,source_revision,principal_type,principal_id,
	 digest,item_count) VALUES($1,'v2',$2,1,'user',$3,$4,1) RETURNING id`,
		tenantID, collectionID, actor, strings.Repeat("a", 64)).Scan(&snapshotID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.runs
	 (tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,
	 created_by,failure_policy,graph_sealed)
	 VALUES($1,'v2','completed',$2,
	 jsonb_build_object('project_id',$3::text,'session_id',$4::text),
	 $5,$6,'fail_fast',true)
	 RETURNING id`, tenantID, snapshotID, projectID, sessionID,
		strings.Repeat("b", 64), actor).
		Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.v2_workflows
	 (tenant_id,project_id,run_id,input_snapshot_id,workflow_type,
	 renderer_version,policy_sha)
	 VALUES($1,$2,$3,$4,'session','v2.2',$5) RETURNING id`,
		tenantID, projectID, parentID, snapshotID, strings.Repeat("c", 64)).
		Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.artifacts
	 (tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,
	 verified_at,output_role)
	 VALUES($1,'test','expired-plan',$2,64,'application/json','v1',now(),
	 'document') RETURNING id`, tenantID, strings.Repeat("d", 64)).
		Scan(&artifactID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.session_analyses
	 (tenant_id,project_id,session_id,workflow_id,snapshot_id,plan_artifact_id,
	 policy_sha,model_lock_sha,plan_body_digest)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, tenantID,
		projectID, sessionID, workflowID, snapshotID, artifactID,
		strings.Repeat("c", 64), strings.Repeat("e", 64), strings.Repeat("f", 64)).
		Scan(&analysisID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.runs
	 (tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,
	 created_by,failure_policy)
	 VALUES($1,'apply','queued',$2,'{}',$3,$4,'continue_items')
	 RETURNING id`, tenantID, snapshotID, strings.Repeat("1", 64), actor).
		Scan(&childID); err != nil {
		t.Fatal(err)
	}
	groupID := uuid.New()
	request := ApplyDecision{PlanSHA: strings.Repeat("d", 64),
		ApprovedGroups:   []uuid.UUID{groupID},
		Recipe:           ArtifactRef{ArtifactID: artifactID, SHA256: strings.Repeat("d", 64)},
		ExpectedRevision: "1"}
	decisionSHA, err := canonical.Hash(struct {
		AnalysisID uuid.UUID     `json:"analysis_id"`
		Decision   ApplyDecision `json:"decision"`
	}{analysisID, request})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.v22_analysis_decisions
	 (tenant_id,project_id,analysis_id,child_run_id,actor_id,plan_sha,
	 recipe_artifact_id,recipe_sha,decision_sha,approved_groups)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, tenantID, projectID,
		analysisID, childID, actor, strings.Repeat("d", 64), artifactID,
		strings.Repeat("d", 64), decisionSHA, []uuid.UUID{groupID}); err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString()
	if _, err = admin.Exec(ctx, `INSERT INTO photo.idempotency_records
	 (tenant_id,principal_type,principal_id,route,key,request_hash,
	 response_status,response_body,resource_id,expires_at)
	 VALUES($1,'user',$2,'SessionAnalysis.Apply',$3,$4,202,$5,$6,
	 now()+interval '1 day')`, tenantID, actor, key, strings.Repeat("3", 64),
		`{"run_id":"`+childID.String()+`"}`, childID); err != nil {
		t.Fatal(err)
	}
	service := Service{DB: api}
	who := auth.Identity{UserID: actor, SessionID: authSession, Epoch: epoch}
	ticket, found, err := service.analysisApplyReplay(ctx, who, tenantID,
		analysisID, key, decisionSHA)
	if err != nil || !found || ticket.RunID != childID ||
		ticket.WorkflowID != workflowID {
		t.Fatalf("sealed replay: %+v, %v, %v", ticket, found, err)
	}
	// The public service path must return before it checks object storage.
	ticket, found, err = service.ApplyAnalysis(ctx, who, tenantID,
		analysisID, key, request)
	if err != nil || !found || ticket.RunID != childID ||
		ticket.WorkflowID != workflowID {
		t.Fatalf("service replay: %+v, %v, %v", ticket, found, err)
	}
	changed := request
	changed.ApprovedGroups = []uuid.UUID{uuid.New()}
	if _, found, err = service.ApplyAnalysis(ctx, who, tenantID,
		analysisID, key, changed); err == nil || found {
		t.Fatalf("reused key with changed decision: %v, %v", found, err)
	}
	if _, found, err = service.analysisApplyReplay(ctx, who, tenantID,
		analysisID, key, strings.Repeat("4", 64)); err == nil || found {
		t.Fatalf("mismatched decision replayed: %v, %v", found, err)
	}
	if _, found, err = service.analysisApplyReplay(ctx, who, tenantID,
		analysisID, uuid.NewString(), decisionSHA); err != nil || found {
		t.Fatalf("unknown key: %v, %v", found, err)
	}
	if _, found, err = service.analysisApplyReplay(ctx,
		auth.Identity{UserID: uuid.New(), SessionID: authSession, Epoch: epoch},
		tenantID, analysisID, key, decisionSHA); err == nil || found {
		t.Fatalf("different actor accessed replay: %v, %v", found, err)
	}
}
