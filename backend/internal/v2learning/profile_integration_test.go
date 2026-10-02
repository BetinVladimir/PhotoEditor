package v2learning

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
	"photoeditor/backend/internal/pagination"
)

func TestIntegrationProfileCreateGetAndListVersions(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with V23 migrations required")
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
	var actor, tenantID, projectID, authSession uuid.UUID
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
	 VALUES($1,$2,$3,'web',now()+interval '1 hour') RETURNING id`,
		actor, auth.HashSecret(uuid.NewString()), uuid.New()).Scan(&authSession); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.projects(tenant_id,name)
	 VALUES($1,'Learning') RETURNING id`, tenantID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: actor, SessionID: authSession, Epoch: epoch}
	svc := ProfileService{DB: api, Cursors: pagination.Codec{
		Key: []byte(strings.Repeat("k", 32)), TTL: 15 * time.Minute}}
	created, err := svc.CreateProfile(ctx, who, tenantID, "profile-one",
		ProfileCreate{ProjectID: projectID, Name: "Personal", Mode: "photographer"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != 201 || created.ResourceID == nil {
		t.Fatalf("unexpected create response: %+v", created)
	}
	replay, err := svc.CreateProfile(ctx, who, tenantID, "profile-one",
		ProfileCreate{ProjectID: projectID, Name: "Personal", Mode: "photographer"})
	if err != nil || !replay.Replayed || string(replay.Body) != string(created.Body) {
		t.Fatalf("unexpected replay: %+v %v", replay, err)
	}
	profile, err := svc.GetProfile(ctx, who, tenantID, *created.ResourceID)
	if err != nil || profile.ID != *created.ResourceID || profile.Revision != "1" ||
		profile.ConsentState != "disabled" {
		t.Fatalf("unexpected profile: %+v %v", profile, err)
	}
	var consentID uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.consent_grants
	 (tenant_id,project_id,purpose,status,epoch,policy_version,actor_id)
	 VALUES($1,$2,'learning_photographer','granted',1,'v1',$3)
	 RETURNING id`, tenantID, projectID, actor).Scan(&consentID); err != nil {
		t.Fatal(err)
	}
	profile, err = svc.GetProfile(ctx, who, tenantID, *created.ResourceID)
	if err != nil || profile.ConsentState != "enabled" {
		t.Fatalf("grant not reflected: %+v %v", profile, err)
	}
	if _, err = admin.Exec(ctx, `UPDATE photo.consent_grants SET status='revoked',
	 epoch=2 WHERE id=$1`, consentID); err != nil {
		t.Fatal(err)
	}
	profile, err = svc.GetProfile(ctx, who, tenantID, *created.ResourceID)
	if err != nil || profile.ConsentState != "revoked" {
		t.Fatalf("revocation not reflected: %+v %v", profile, err)
	}
	var snapshotID, runID, workflowID, evaluationID, versionID uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.snapshots
	 (tenant_id,kind,principal_type,principal_id,digest,item_count)
	 VALUES($1,'v2','user',$2,$3,1) RETURNING id`, tenantID, actor,
		strings.Repeat("a", 64)).Scan(&snapshotID); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]string{"project_id": projectID.String()})
	if err = admin.QueryRow(ctx, `INSERT INTO photo.runs
	 (tenant_id,kind,status,input_snapshot_id,config_snapshot,
	 config_hash,created_by,failure_policy)
	 VALUES($1,'v2','draft',$2,$3,$4,$5,'fail_fast') RETURNING id`,
		tenantID, snapshotID, config, strings.Repeat("b", 64), actor).
		Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.v2_workflows
	 (tenant_id,project_id,run_id,input_snapshot_id,workflow_type,
	 renderer_version,policy_sha)
	 VALUES($1,$2,$3,$4,'learning','v2.2',$5) RETURNING id`,
		tenantID, projectID, runID, snapshotID, strings.Repeat("c", 64)).
		Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	artifact := func(digest string) uuid.UUID {
		var id uuid.UUID
		if e := admin.QueryRow(ctx, `INSERT INTO photo.artifacts
		 (tenant_id,bucket,object_key,object_version,sha256,bytes,
		 media_type,output_role,verified_at)
		 VALUES($1,'test',$2,'v1',$3,64,'application/json','document',now())
		 RETURNING id`, tenantID, uuid.NewString(), digest).Scan(&id); e != nil {
			t.Fatal(e)
		}
		return id
	}
	training, model := artifact(strings.Repeat("d", 64)), artifact(strings.Repeat("e", 64))
	evaluationID = artifact(strings.Repeat("f", 64))
	if err = admin.QueryRow(ctx, `INSERT INTO photo.learning_versions
	 (tenant_id,project_id,profile_id,version_no,workflow_id,
	 training_artifact_id,model_artifact_id,evaluation_artifact_id)
	 VALUES($1,$2,$3,1,$4,$5,$6,$7) RETURNING id`, tenantID,
		projectID, *created.ResourceID, workflowID, training, model, evaluationID).
		Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.version_eligibility
	 (tenant_id,project_id,version_id,status)
	 VALUES($1,$2,$3,'eligible')`, tenantID, projectID, versionID); err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListVersions(ctx, who, tenantID, *created.ResourceID, 1, "")
	if err != nil || len(page.Items) != 1 || page.NextCursor != nil ||
		page.Items[0].ID != versionID || page.Items[0].Revision != "1" ||
		page.Items[0].Eligibility != "eligible" ||
		page.Items[0].Evaluation.ArtifactID != evaluationID {
		t.Fatalf("unexpected version page: %+v %v", page, err)
	}
}
