package v2draft

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
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type approvalReadStore struct{ bodies map[string][]byte }

func (s approvalReadStore) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.bodies[obj.Key])), nil
}
func (s approvalReadStore) PutImmutable(_ context.Context, key string, reader io.Reader,
	length int64, digest, mediaType string) (objectstore.Object, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return objectstore.Object{}, err
	}
	sum := sha256.Sum256(body)
	if int64(len(body)) != length || hex.EncodeToString(sum[:]) != digest {
		return objectstore.Object{}, objectstore.ErrIntegrity
	}
	s.bodies[key] = body
	return objectstore.Object{Key: key, Bytes: length, SHA256: digest,
		VersionID: "v1", ContentType: mediaType}, nil
}
func (s approvalReadStore) RecoverImmutable(_ context.Context, key string,
	length int64, digest, mediaType string) (objectstore.Object, error) {
	body, ok := s.bodies[key]
	if !ok {
		return objectstore.Object{}, objectstore.ErrMissing
	}
	sum := sha256.Sum256(body)
	if int64(len(body)) != length || hex.EncodeToString(sum[:]) != digest {
		return objectstore.Object{}, objectstore.ErrIntegrity
	}
	return objectstore.Object{Key: key, Bytes: length, SHA256: digest,
		VersionID: "v1", ContentType: mediaType}, nil
}

func TestIntegrationReadApprovalDraftPinsCurrentDocument(t *testing.T) {
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
	var userID, tenantID, projectID, sourceID, assetID, versionID, streamID uuid.UUID
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
	if err = admin.QueryRow(ctx, "INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Approval') RETURNING id", tenantID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err = json.Unmarshal(fixture, &images); err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	imageSHA := sha256.Sum256(images[0])
	sourceSHA := hex.EncodeToString(imageSHA[:])
	if err = admin.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,
	 sha256,bytes,media_type,object_version,verified_at,output_role)
	 VALUES($1,'test','image',$2,$3,'application/json','v1',now(),'working_image') RETURNING id`,
		tenantID, sourceSHA, len(images[0])).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.assets(tenant_id,project_id,
	 original_artifact_id,metadata,upload_state,provenance)
	 VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,
		tenantID, projectID, sourceID).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.asset_versions(tenant_id,asset_id,
	 master_artifact_id,content_hash,metadata,review_state)
	 VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`,
		tenantID, assetID, sourceID, sourceSHA).Scan(&versionID); err != nil {
		t.Fatal(err)
	}
	canvasHash, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: versionID,
		Source: v2input.Ref{ArtifactID: sourceID, SHA256: sourceSHA}, CanvasHash: canvasHash}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	recipe, err := canonical.Marshal(map[string]any{
		"schema_version": 3, "source": binding, "base_style": style,
		"session_corrections": nil, "learning_version_id": nil,
		"overrides": []any{}, "layer_stack": nil, "renderer_version": "v2.2",
		"parameter_delta": CorrectionVector{},
	})
	if err != nil {
		t.Fatal(err)
	}
	recipeSHA := sha256.Sum256(recipe)
	digest := hex.EncodeToString(recipeSHA[:])
	var recipeArtifactID, documentID uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,
	 sha256,bytes,media_type,object_version,verified_at,output_role)
	 VALUES($1,'test','recipe',$2,$3,'application/json','v1',now(),'document') RETURNING id`,
		tenantID, digest, len(recipe)).Scan(&recipeArtifactID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.v2_document_streams(tenant_id,project_id,
	 asset_version_id,stream_type) VALUES($1,$2,$3,'draft') RETURNING id`,
		tenantID, projectID, versionID).Scan(&streamID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.v2_documents(tenant_id,project_id,
	 stream_id,version_no,document_type,artifact_id,sha256)
	 VALUES($1,$2,$3,1,'EditingRecipeV3',$4,$5) RETURNING id`,
		tenantID, projectID, streamID, recipeArtifactID, digest).Scan(&documentID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `UPDATE photo.v2_document_streams SET current_document_id=$1
	 WHERE tenant_id=$2 AND id=$3`, documentID, tenantID, streamID); err != nil {
		t.Fatal(err)
	}
	store := approvalReadStore{bodies: map[string][]byte{"image": images[0], "recipe": recipe}}
	service := Service{DB: api, Objects: store, Bucket: "test"}
	who := auth.Identity{UserID: userID, SessionID: sessionID, Epoch: epoch}
	// LayerStack reads use an artifact ref, not the draft document identity.
	layerBody, e := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": []any{}})
	if e != nil {
		t.Fatal(e)
	}
	sumLayer := sha256.Sum256(layerBody)
	layerSHA := hex.EncodeToString(sumLayer[:])
	var layerArtifact uuid.UUID
	if e = admin.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role)
      VALUES($1,'test','layers',$2,$3,'application/json','v1',now(),'document') RETURNING id`, tenantID, layerSHA, len(layerBody)).Scan(&layerArtifact); e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Exec(ctx, `INSERT INTO photo.v2_documents(tenant_id,project_id,stream_id,version_no,document_type,artifact_id,sha256)
      VALUES($1,$2,$3,2,'LayerStack',$4,$5)`, tenantID, projectID, streamID, layerArtifact, layerSHA); e != nil {
		t.Fatal(e)
	}
	store.bodies["layers"] = layerBody
	layerRef := v2input.Ref{ArtifactID: layerArtifact, SHA256: layerSHA}
	layerRead, e := service.GetLayerStack(ctx, who, tenantID, assetID, layerRef)
	if e != nil || !bytes.Equal(layerRead, layerBody) {
		t.Fatalf("pinned LayerStack read: %s %v", layerRead, e)
	}
	if _, e = service.GetLayerStack(ctx, who, tenantID, assetID, v2input.Ref{ArtifactID: layerArtifact, SHA256: strings.Repeat("f", 64)}); e == nil {
		t.Fatal("changed layer SHA accepted")
	}
	store.bodies["layers"] = append([]byte(nil), layerBody...)
	store.bodies["layers"][len(layerBody)-2] ^= 1
	if _, e = service.GetLayerStack(ctx, who, tenantID, assetID, layerRef); e == nil {
		t.Fatal("corrupt layer document accepted")
	}
	store.bodies["layers"] = layerBody
	badWho := who
	badWho.SessionID = uuid.New()
	if _, e = service.GetLayerStack(ctx, badWho, tenantID, assetID, layerRef); e == nil {
		t.Fatal("invalid session read layers")
	}
	out, err := service.ReadApprovalDraft(ctx, who, tenantID, assetID, documentID, 2)
	if err != nil || out.ProjectID != projectID || out.AssetVersionID != versionID ||
		out.StreamID != streamID || !bytes.Equal(out.Recipe, recipe) || out.Revision != 2 {
		t.Fatal("current approval recipe", out, err)
	}
	if _, err = service.ReadApprovalDraft(ctx, who, tenantID, assetID, documentID, 1); err == nil {
		t.Fatal("accepted stale stream revision")
	}
	if _, err = service.ReadApprovalDraft(ctx, who, tenantID, assetID, uuid.New(), 2); err == nil {
		t.Fatal("accepted non-current document")
	}
	const modelLock = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	const snapshotDigest = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	var collectionID, snapshotID, runID, workflowID, analysisID, groupID, describeTaskID uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.collections(tenant_id,project_id,name)
	 VALUES($1,$2,'Descriptor') RETURNING id`, tenantID, projectID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	var shootID uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.shoot_sessions(tenant_id,project_id,
	 collection_id,name,state,policy,consent_epoch)
	 VALUES($1,$2,$3,'Descriptor','open','{}',1) RETURNING id`,
		tenantID, projectID, collectionID).Scan(&shootID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.snapshots(tenant_id,kind,
	 collection_id,source_revision,principal_type,principal_id,digest,item_count,
	 expires_at)
	 VALUES($1,'v2',$2,1,'user',$3,$4,1,now()+interval '1 hour') RETURNING id`,
		tenantID, collectionID, userID, snapshotDigest).Scan(&snapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,
	 ordinal,asset_version_id,artifact_id,sha256,payload)
	 VALUES($1,$2,0,$3,$4,$5,'{}')`, tenantID, snapshotID, versionID,
		sourceID, sourceSHA); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `UPDATE photo.snapshots SET expires_at=NULL
	 WHERE tenant_id=$1 AND id=$2`, tenantID, snapshotID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.runs(tenant_id,kind,status,
	 input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed)
	 VALUES($1,'v2','waiting_review',$2,$3,$4,$5,'fail_fast',true) RETURNING id`,
		tenantID, snapshotID, json.RawMessage(`{"project_id":"`+projectID.String()+`","session_id":"`+shootID.String()+`","v22_session_plan":{"snapshot_digest":"`+snapshotDigest+`"}}`),
		strings.Repeat("c", 64), userID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.v2_workflows(tenant_id,project_id,
	 run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha)
	 VALUES($1,$2,$3,$4,'session','v2.2',$5) RETURNING id`, tenantID,
		projectID, runID, snapshotID, strings.Repeat("d", 64)).Scan(&workflowID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.session_analyses(tenant_id,project_id,
	 session_id,workflow_id,snapshot_id,plan_artifact_id,policy_sha,model_lock_sha)
	 VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, tenantID, projectID,
		shootID, workflowID, snapshotID, recipeArtifactID,
		strings.Repeat("d", 64), modelLock).Scan(&analysisID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.scene_groups(tenant_id,project_id,
	 analysis_id,ordinal,reference_version_id,confidence)
	 VALUES($1,$2,$3,0,$4,1) RETURNING id`, tenantID, projectID,
		analysisID, versionID).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.scene_members(tenant_id,project_id,
	 group_id,asset_version_id,snapshot_ordinal) VALUES($1,$2,$3,$4,0)`,
		tenantID, projectID, groupID, versionID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.tasks(tenant_id,run_id,node_key,
	 operation,operation_version,resource_class,state,inputs,params_artifact_id,
	 input_digest,attempt,fencing_token,execution_profile)
	 VALUES($1,$2,'session.describe:0','session.describe','1','gpu',
	 'blocked','[]',$3,$4,1,1,'{"renderer_version":"v2.2"}') RETURNING id`, tenantID, runID,
		recipeArtifactID, strings.Repeat("a", 64)).Scan(&describeTaskID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.task_attempts(tenant_id,task_id,
	 attempt,fencing_token,worker_id,started_at,backend,profile)
	 VALUES($1,$2,1,1,$3,now(),'cuda','{}')`, tenantID,
		describeTaskID, uuid.New()); err != nil {
		t.Fatal(err)
	}
	shard, err := canonical.Marshal(map[string]any{"schema_version": 1,
		"snapshot_digest": snapshotDigest, "frames": []any{map[string]any{
			"schema_version": 1, "binding": binding, "ordinal": 0,
			"captured_at": nil, "camera_hash": nil,
			"scene_embedding": make([]float64, 384),
			"luma_quantiles":  []float64{.1, .2, .5, .7, .9},
			"neutral_rgb":     nil, "sharpness": 1, "clipping_fraction": 0,
			"person_fraction": 0,
			"faces":           v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("9", 64)},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	shardSHA := sha256.Sum256(shard)
	descriptorKey := "descriptor/" + uuid.NewString()
	var shardID, resultID uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,
	 sha256,bytes,media_type,object_version,verified_at,output_role,
	 created_by_task_id,produced_attempt,produced_fence)
	 VALUES($1,'test',$5,$2,$3,'application/json','v1',now(),
	 'document',$4,1,1) RETURNING id`, tenantID,
		hex.EncodeToString(shardSHA[:]), len(shard), describeTaskID, descriptorKey).Scan(&shardID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.output_uploads(tenant_id,task_id,
	 attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,
	 role,bucket,object_key,sha256,bytes,media_type,state,object_version)
	 VALUES($1,$2,1,1,$3,$4,$5,$6,'document','test',$9,$7,$8,
	 'application/json','ready','v1')`, tenantID, describeTaskID, uuid.New(),
		strings.Repeat("5", 64), uuid.NewString(), shardID,
		hex.EncodeToString(shardSHA[:]), len(shard), descriptorKey); err != nil {
		t.Fatal(err)
	}
	outputs, err := json.Marshal([]v2input.Ref{{ArtifactID: shardID,
		SHA256: hex.EncodeToString(shardSHA[:])}})
	if err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.task_results(tenant_id,task_id,
	 attempt,fencing_token,outputs,metrics,review_required,result_digest)
	 VALUES($1,$2,1,1,$3,'{}',false,$4) RETURNING id`, tenantID,
		describeTaskID, outputs, strings.Repeat("8", 64)).Scan(&resultID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `UPDATE photo.tasks SET canonical_result_id=$1,
	 state='succeeded' WHERE id=$2`, resultID, describeTaskID); err != nil {
		t.Fatal(err)
	}
	store.bodies[descriptorKey] = shard
	frame, err := service.ReadFrameDescriptor(ctx, who, tenantID, projectID,
		binding, modelLock, nil)
	if err != nil || frame.ShardRef.ArtifactID != shardID ||
		frame.Features.Binding != binding || frame.Ordinal != 0 {
		t.Fatal("read pinned frame descriptor", frame, err)
	}
	if _, err = service.ReadFrameDescriptor(ctx, who, tenantID, projectID,
		binding, strings.Repeat("7", 64), nil); err == nil {
		t.Fatal("accepted descriptor from another model lock")
	}
	key := uuid.NewString()
	request := ApprovalRequest{ExpectedRevision: "2", DocumentID: documentID}
	if _, err = service.Approve(ctx, who, tenantID, assetID, uuid.NewString(), request); err == nil {
		t.Fatal("approval compiled a recipe without a pinned model lock")
	}
	var premature int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.v2_document_approvals
	 WHERE tenant_id=$1 AND document_id=$2`, tenantID, documentID).Scan(&premature); err != nil || premature != 0 {
		t.Fatal("approval persisted without a resolved model lock", premature, err)
	}
	inputs := ResolvedRecipeInputs{Binding: binding, BaseStyle: style,
		Style:      StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1},
		ICCLockSHA: image.Color.ICCSHA256, ModelLockSHA: strings.Repeat("e", 64)}
	committed, err := service.commitResolvedApproval(ctx, who, tenantID,
		assetID, key, request, out, inputs)
	if err != nil || committed.Status != 200 {
		t.Fatal("commit approval", committed, err)
	}
	var result struct {
		Revision   string    `json:"revision"`
		ApprovalID uuid.UUID `json:"approval_id"`
	}
	if err = json.Unmarshal(committed.Body, &result); err != nil ||
		result.Revision != "3" || result.ApprovalID == uuid.Nil {
		t.Fatal("approval result", result, err)
	}
	replayDraft, err := service.ReadApprovalDraft(ctx, who, tenantID, assetID,
		documentID, 2)
	if err != nil {
		t.Fatal("read approved replay", err)
	}
	replay, err := service.commitResolvedApproval(ctx, who, tenantID, assetID,
		key, request, replayDraft, inputs)
	if err != nil || !replay.Replayed || !bytes.Equal(replay.Body, committed.Body) {
		t.Fatal("approval replay", replay, err)
	}
	var approvals, audits int
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.v2_document_approvals WHERE tenant_id=$1 AND document_id=$2", tenantID, documentID).Scan(&approvals); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='v2_draft.approved'", tenantID, documentID).Scan(&audits); err != nil || approvals != 1 || audits != 1 {
		t.Fatal("approval/audit cardinality", approvals, audits, err)
	}
	store.bodies["recipe"] = []byte(`{}`)
	if _, err = service.ReadApprovalDraft(ctx, who, tenantID, assetID, documentID, 2); err == nil {
		t.Fatal("accepted modified object bytes")
	}
}
