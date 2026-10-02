package v2sessions

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type applyObjectStore map[string][]byte

func (s applyObjectStore) Open(_ context.Context, object objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s[object.Key])), nil
}

func TestIntegrationAnalysisApplyNew(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with V22 migrations required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	var actor, tenantID, projectID, collectionID, sessionID, authSession uuid.UUID
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
	if _, err = admin.Exec(ctx, `INSERT INTO photo.tenant_usage(tenant_id)
	 VALUES($1) ON CONFLICT DO NOTHING`, tenantID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.sessions
	 (user_id,refresh_hash,family_id,client_type,expires_at)
	 VALUES($1,$2,$3,'web',now()+interval '1 day') RETURNING id`,
		actor, auth.HashSecret(uuid.NewString()), uuid.New()).Scan(&authSession); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.projects(tenant_id,name)
	 VALUES($1,'New Analysis Apply') RETURNING id`, tenantID).Scan(&projectID); err != nil {
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
	store := applyObjectStore{}
	artifact := func(key, sha, media, role string, size int) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := admin.QueryRow(ctx, `INSERT INTO photo.artifacts
		 (tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,
		 verified_at,output_role)
		 VALUES($1,'test',$2,'v1',$3,$4,$5,now(),NULLIF($6,'')) RETURNING id`,
			tenantID, key, sha, size, media, role).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	const sourceSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sourceArtifact := artifact("source", sourceSHA, "image/jpeg", "", 1024)
	var assetID, versionID, itemID uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.assets
	 (tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance)
	 VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`, tenantID,
		projectID, sourceArtifact).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.asset_versions
	 (tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state)
	 VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`, tenantID,
		assetID, sourceArtifact, sourceSHA).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.collection_items
	 (tenant_id,collection_id,asset_id,asset_version_id,position)
	 VALUES($1,$2,$3,$4,0) RETURNING id`, tenantID, collectionID,
		assetID, versionID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	var sourceRevision int64
	if err = admin.QueryRow(ctx, `SELECT revision FROM photo.collections
	 WHERE tenant_id=$1 AND id=$2`, tenantID, collectionID).Scan(&sourceRevision); err != nil {
		t.Fatal(err)
	}
	parentID := uuid.New()
	snapshotID, err := v2input.AnalysisSnapshotID(parentID)
	if err != nil {
		t.Fatal(err)
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: versionID,
		Source:     v2input.Ref{ArtifactID: sourceArtifact, SHA256: sourceSHA},
		CanvasHash: strings.Repeat("b", 64)}
	item := v2input.SessionSnapshotItem{Ordinal: 0, Binding: binding,
		WorkingImage: binding.Source,
		Recipe:       v2input.Ref{ArtifactID: sourceArtifact, SHA256: sourceSHA}}
	input, inputBody, err := v2input.CompileSessionInputSnapshot(parentID,
		[]v2input.SessionSnapshotItem{item})
	if err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.snapshots
	 (id,tenant_id,kind,collection_id,source_revision,principal_type,
	 principal_id,digest,item_count,expires_at)
	 VALUES($1,$2,'v2',$3,$4,'user',$5,$6,1,
	 now()+interval '1 hour') RETURNING id`,
		snapshotID, tenantID, collectionID, sourceRevision, actor,
		input.SnapshotDigest).Scan(&snapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.snapshot_items
	 (tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,
	 sha256,payload) VALUES($1,$2,0,$3,$4,$5,$6,'{}')`, tenantID,
		snapshotID, itemID, versionID, sourceArtifact, sourceSHA); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `UPDATE photo.snapshots SET expires_at=NULL
	 WHERE tenant_id=$1 AND id=$2`, tenantID, snapshotID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.runs
	 (id,tenant_id,kind,status,input_snapshot_id,config_snapshot,
	 config_hash,created_by,failure_policy,graph_sealed)
	 VALUES($1,$2,'v2','waiting_review',$3,
	 jsonb_build_object('project_id',$4::text,'session_id',$5::text),
	 $6,$7,'fail_fast',true) RETURNING id`, parentID, tenantID,
		snapshotID, projectID, sessionID, strings.Repeat("c", 64), actor).
		Scan(&parentID); err != nil {
		t.Fatal(err)
	}
	const policySHA = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	var workflowID uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.v2_workflows
	 (tenant_id,project_id,run_id,input_snapshot_id,workflow_type,
	 renderer_version,policy_sha)
	 VALUES($1,$2,$3,$4,'session','v2.2',$5) RETURNING id`,
		tenantID, projectID, parentID, snapshotID, policySHA).
		Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	hashBytes := func(body []byte) string {
		sum := sha256.Sum256(body)
		return hex.EncodeToString(sum[:])
	}
	inputSHA := hashBytes(inputBody)
	store["input"] = inputBody
	inputArtifact := artifact("input", inputSHA, "application/json", "", len(inputBody))
	if _, err = admin.Exec(ctx, `INSERT INTO photo.backend_documents
	 (tenant_id,run_id,owner_id,purpose,artifact_id,token,sha256,bytes,
	 bucket,state,object_version,completed_at)
	 VALUES($1,$2,$2,'v2_analysis_input_snapshot',$3,gen_random_uuid(),
	 $4,$5,'test','ready','v1',now())`, tenantID, parentID,
		inputArtifact, inputSHA, len(inputBody)); err != nil {
		t.Fatal(err)
	}
	groupID := uuid.New()
	correctionBody, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "snapshot_digest": input.SnapshotDigest,
		"items": []any{map[string]any{"binding": binding,
			"ordinal": 0, "delta": v2draft.CorrectionVector{},
			"disposition": "no_change", "reason": nil}},
	})
	if err != nil {
		t.Fatal(err)
	}
	correctionSHA := hashBytes(correctionBody)
	store["correction"] = correctionBody
	correctionArtifact := artifact("correction", correctionSHA,
		"application/json", "correction_plan", len(correctionBody))
	planBody, err := canonical.Marshal(scenePlan{SchemaVersion: 1,
		SnapshotID: snapshotID, SnapshotDigest: input.SnapshotDigest,
		PolicySHA: policySHA, Groups: []sceneGroup{{ID: groupID,
			Ordinals: []int{0}, ReferenceOrdinal: 0, Confidence: 1,
			Corrections: ArtifactRef{ArtifactID: correctionArtifact,
				SHA256: correctionSHA}}}})
	if err != nil {
		t.Fatal(err)
	}
	planSHA := hashBytes(planBody)
	store["plan"] = planBody
	planArtifact := artifact("plan", planSHA, "application/json", "document", len(planBody))
	var analysisID uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.session_analyses
	 (tenant_id,project_id,session_id,workflow_id,snapshot_id,
	 plan_artifact_id,policy_sha,model_lock_sha,plan_body_digest)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		tenantID, projectID, sessionID, workflowID, snapshotID,
		planArtifact, policySHA, strings.Repeat("e", 64), planSHA).
		Scan(&analysisID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.scene_groups
	 (id,tenant_id,project_id,analysis_id,ordinal,reference_version_id,
	 confidence) VALUES($1,$2,$3,$4,0,$5,1)`, groupID, tenantID,
		projectID, analysisID, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.scene_members
	 (tenant_id,project_id,group_id,asset_version_id,snapshot_ordinal)
	 VALUES($1,$2,$3,$4,0)`, tenantID, projectID, groupID, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.v2_artifact_links
	 (tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal)
	 VALUES($1,$2,$3,$4,$5,'group-correction',0)`, tenantID,
		projectID, workflowID, correctionArtifact, correctionSHA); err != nil {
		t.Fatal(err)
	}
	styleBody, err := os.ReadFile("../run/identity_style.json")
	if err != nil {
		t.Fatal(err)
	}
	styleSHA, err := canonical.Hash(json.RawMessage(styleBody))
	if err != nil {
		t.Fatal(err)
	}
	styleArtifact := artifact("style", styleSHA, "application/json", "", len(styleBody))
	var styleID, styleVersionID uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.styles
	 (tenant_id,project_id,name,created_by) VALUES($1,$2,'Base',$3)
	 RETURNING id`, tenantID, projectID, actor).Scan(&styleID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.style_versions
	 (tenant_id,style_id,version_no,config_json,config_hash,schema_version,
	 renderer_version,approval_status,metrics,export_artifact_id)
	 VALUES($1,$2,1,$3,$4,'2.0.0','photo_renderer_v2','accepted','{}',$5)
	 RETURNING id`, tenantID, styleID, styleBody, styleSHA, styleArtifact).
		Scan(&styleVersionID); err != nil {
		t.Fatal(err)
	}
	service := Service{DB: api, Objects: store, Bucket: "test"}
	who := auth.Identity{UserID: actor, SessionID: authSession, Epoch: epoch}
	// Scene Board uses the same immutable completed plan under the actual API role.
	if _, err = admin.Exec(ctx, `INSERT INTO photo.v2_artifact_links
     (tenant_id,project_id,workflow_id,artifact_id,sha256,purpose,ordinal)
     VALUES($1,$2,$3,$4,$5,'scene-plan',0)`, tenantID, projectID, workflowID, planArtifact, planSHA); err != nil {
		t.Fatal(err)
	}
	analyses, listErr := service.ListAnalyses(ctx, who, tenantID, sessionID, 100, "")
	if listErr != nil || len(analyses.Items) != 1 || analyses.NextCursor != nil {
		t.Fatalf("analysis list: %+v %v", analyses, listErr)
	}
	var envelope struct {
		ID uuid.UUID `json:"id"`
	}
	if err = json.Unmarshal(analyses.Items[0], &envelope); err != nil || envelope.ID != analysisID {
		t.Fatalf("analysis list envelope: %+v %v", envelope, err)
	}
	invalidListWho := who
	invalidListWho.SessionID = uuid.New()
	if _, err = service.ListAnalyses(ctx, invalidListWho, tenantID, sessionID, 100, ""); err == nil {
		t.Fatal("invalid session listed analyses")
	}
	board, boardErr := service.GetSceneBoard(ctx, who, tenantID, analysisID)
	if boardErr != nil || board.AnalysisID != analysisID || board.SessionID != sessionID || board.SnapshotID != snapshotID || board.PlanSHA != planSHA || !bytes.Equal(board.Plan, planBody) || len(board.Frames) != 1 || board.Frames[0].VersionID != versionID || board.Frames[0].SourceSHA != sourceSHA {
		t.Fatalf("scene board: %+v %v", board, boardErr)
	}
	store["plan"] = append([]byte(nil), planBody...)
	store["plan"][len(planBody)-2] ^= 1
	if _, err = service.GetSceneBoard(ctx, who, tenantID, analysisID); err == nil {
		t.Fatal("corrupted plan exposed")
	}
	store["plan"] = planBody
	if _, err = service.GetSceneBoard(ctx, who, tenantID, uuid.New()); err == nil {
		t.Fatal("missing analysis exposed")
	}
	denied := who
	denied.SessionID = uuid.New()
	if _, err = service.GetSceneBoard(ctx, denied, tenantID, analysisID); err == nil {
		t.Fatal("unregistered authenticated session read plan")
	}
	request := ApplyDecision{PlanSHA: planSHA,
		ApprovedGroups:   []uuid.UUID{groupID},
		Recipe:           ArtifactRef{ArtifactID: styleArtifact, SHA256: styleSHA},
		ExpectedRevision: "1"}
	key := uuid.NewString()
	if _, _, err = service.analysisApplyReplay(ctx, who, tenantID,
		analysisID, key, strings.Repeat("0", 64)); err != nil {
		t.Fatalf("replay preflight: %v", err)
	}
	if _, err = service.analysisApplyContext(ctx, who, tenantID,
		analysisID, request.Recipe); err != nil {
		t.Fatalf("analysis context: %v", err)
	}
	stale := request
	stale.ExpectedRevision = "2"
	if _, _, err = service.ApplyAnalysis(ctx, who, tenantID,
		analysisID, uuid.NewString(), stale); err == nil {
		t.Fatal("stale parent revision admitted a child")
	}
	var premature int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.runs
	 WHERE tenant_id=$1 AND kind='apply'`, tenantID).Scan(&premature); err != nil ||
		premature != 0 {
		t.Fatalf("failed admission left child run: %d %v", premature, err)
	}
	ticket, replayed, err := service.ApplyAnalysis(ctx, who, tenantID,
		analysisID, key, request)
	if err != nil || replayed || ticket.RunID == uuid.Nil ||
		ticket.WorkflowID != workflowID {
		t.Fatalf("new analysis apply: %+v replay=%v err=%v", ticket,
			replayed, err)
	}
	var decisionChild uuid.UUID
	if err = admin.QueryRow(ctx, `SELECT child_run_id FROM
	 photo.v22_analysis_decisions WHERE tenant_id=$1 AND analysis_id=$2`,
		tenantID, analysisID).Scan(&decisionChild); err != nil ||
		decisionChild != ticket.RunID {
		t.Fatalf("decision child mismatch: %v %v", decisionChild, err)
	}
	var parentStatus, childStatus, boundSource, boundDecision string
	var selectedCount int
	if err = admin.QueryRow(ctx, `SELECT parent.status,child.status,
	 child.config_snapshot->'item_overrides'->0->'body'->>'input_sha256',
	 child.config_snapshot->>'analysis_decision_sha',
	 (SELECT count(*) FROM photo.snapshot_items i
	  WHERE i.tenant_id=child.tenant_id AND i.snapshot_id=child.input_snapshot_id)
	 FROM photo.runs parent JOIN photo.runs child
	 ON child.tenant_id=parent.tenant_id
	 WHERE parent.tenant_id=$1 AND parent.id=$2 AND child.id=$3`,
		tenantID, parentID, ticket.RunID).Scan(&parentStatus, &childStatus,
		&boundSource, &boundDecision, &selectedCount); err != nil ||
		parentStatus != "completed" || childStatus != "queued" ||
		boundSource != sourceSHA || len(boundDecision) != 64 || selectedCount != 1 {
		t.Fatalf("atomic parent/child state: parent=%s child=%s source=%s decision=%s count=%d err=%v",
			parentStatus, childStatus, boundSource, boundDecision, selectedCount, err)
	}
	store = nil
	service.Objects = nil
	second, replayed, err := service.ApplyAnalysis(ctx, who, tenantID,
		analysisID, key, request)
	if err != nil || !replayed || second != ticket {
		t.Fatalf("new apply replay: %+v replay=%v err=%v", second,
			replayed, err)
	}
}
