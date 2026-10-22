package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Reproducibly seeded by native-scene-tail in the Python publication fixture.
// Producer engines and imported reports are explicit fixture doubles; all
// ordinary admission, durable continuation, physical IO and transfer replay
// boundaries are the actual production implementations.
func TestIntegrationOrdinaryScenePipelineTail(t *testing.T) {
	raw := os.Getenv("PHOTO_SEMANTIC_INTENT_PUBLISH_CASE")
	if raw == "" {
		t.Skip("isolated migrated DB and versioned object fixture required")
	}
	var f struct {
		Tenant, Actor, Session             uuid.UUID
		Intent                             v2input.SemanticEditIntent
		Objects                            objectstore.Config
		Qualification, PersonQualification v2input.SemanticQualificationPin
		SceneQualifications                [2]v2input.SemanticQualificationPin
		ScenePolicy                        json.RawMessage
		LegacyOrdinary                     bool
	}
	if json.Unmarshal([]byte(raw), &f) != nil || (!f.LegacyOrdinary && len(f.ScenePolicy) == 0) {
		t.Fatal("ordinary scene tail fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 19*time.Minute)
	defer cancel()
	owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Tracer = newNativeTimingTracer(t)
	config.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	config = config.Copy()
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	objects, err := objectstore.New(f.Objects)
	if err != nil {
		t.Fatal(err)
	}
	color, err := LoadTransferColorProfile("../../../resources/icc")
	if err != nil {
		t.Fatal(err)
	}
	var project uuid.UUID
	if err = owner.QueryRow(ctx, `SELECT a.project_id FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id WHERE v.tenant_id=$1 AND v.id=$2`, f.Tenant, f.Intent.Reference.AssetVersionID).Scan(&project); err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	expected := func(operation string, pin v2input.SemanticQualificationPin) v2qualification.Expected {
		return v2qualification.Expected{Operation: operation, Version: "1", Backend: pin.Backend, ModelLockSHA: pin.ModelLockSHA, RuntimeLockSHA: pin.RuntimeLockSHA, DeviceProfileSHA: pin.DeviceProfileSHA, DatasetSHA: pin.DatasetSHA, IssuerID: pin.IssuerID, RequiredChecks: []string{"quality"}, TestFixtureSHAs: map[string]struct{}{strings.Repeat("e", 64): {}}, EvidenceSHAs: map[string]struct{}{strings.Repeat("f", 64): {}}, CalibrationSHAs: map[string]struct{}{pin.Calibration.SHA256: {}}, LicenseCleared: true}
	}
	transferExpected := expected("semantic.transfer", f.Qualification)
	if !f.LegacyOrdinary {
		_, policySHA, err := v2input.DecodeSemanticScenePolicy(f.ScenePolicy)
		if err != nil {
			t.Fatal(err)
		}
		transferExpected.RequiredChecks = []string{"quality", "semantic.scene.farneback_depth_plane_v1"}
		transferExpected.EvidenceSHAs = map[string]struct{}{policySHA: {}}
	}
	producer := func(operation string, pin v2input.SemanticQualificationPin) TransferSceneProducer {
		return TransferSceneProducer{Report: pin.Report, Expected: expected(operation, pin), Costs: v2input.SemanticQualifiedCosts{VRAMBytes: 128 << 20}}
	}
	reader := SQLTransferSourceReader{DB: api, Objects: objects, Bucket: f.Objects.Bucket, ProjectID: project}
	p := TransferDraftPreparer{Reader: reader, Scheduler: scheduler, Documents: v2draft.Publisher{DB: api, Objects: objects, Bucket: f.Objects.Bucket}, TransportDocuments: artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: scheduler, Bucket: f.Objects.Bucket}, Objects: objects}, ReservedBytes: 128 << 20, Configuration: TransferPreparation{Qualification: f.Qualification, Expected: transferExpected, Color: color, Costs: v2input.SemanticQualifiedCosts{VRAMBytes: 128 << 20}, RAM: 8 << 30, VRAM: 1 << 30, Scratch: 1 << 30, Output: 64 << 20, MetadataBytes: 16 << 20, PersonProtection: &TransferPersonProtection{Report: f.PersonQualification.Report, Expected: expected("mask.person", f.PersonQualification)}, Scene: &TransferScenePreparation{Policy: append(json.RawMessage(nil), f.ScenePolicy...), Scene: producer("scene.parse", f.SceneQualifications[0]), Depth: producer("depth.predict", f.SceneQualifications[1])}}}
	if f.LegacyOrdinary {
		p.Configuration.Scene = nil
		probeLegacyOrdinaryUnsealedRecovery(t, ctx, p, who, f.Tenant, f.Intent)
		return
	}
	// Actual ordinary operator deployment constructor, then fresh public create.
	deployment := createDeploymentForProbe(t, p)
	service, err := deployment.Build(ctx, api, scheduler, objects, f.Objects.Bucket)
	if err != nil {
		t.Fatal("ordinary deployment", err)
	}
	key := "ordinary-scene-full-tail"
	body, err := canonical.Marshal(f.Intent)
	if err != nil {
		t.Fatal(err)
	}
	request, err := CompileRequest(f.Tenant, f.Actor, key, body)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Propose(ctx, who, f.Tenant, key, f.Intent)
	if !errors.Is(err, v2input.ErrConflict) || first.Status != 0 {
		t.Fatal("fresh command must wait privately for real preparation", first, err)
	}
	var count int
	if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=$2", f.Tenant, request.RunID).Scan(&count); err != nil || count != 0 {
		t.Fatal("private child became public parent", count, err)
	}
	job := SQLNativeInitialPipelineRecovery{Preparer: p, CatalogForReference: (SQLNativeParentArtifactCatalogFactory{API: api, Bucket: f.Objects.Bucket}).ForReference, Timeout: 2 * time.Minute}
	t.Setenv("PHOTO_TEST_V27_SCENE_TAIL", "1")
	accepted := false
	for step := 0; step < 256; step++ {
		proposeStarted := time.Now()
		reply, replayErr := service.Propose(ctx, who, f.Tenant, key, f.Intent)
		t.Logf("NATIVE_SCENE_PHASE phase=propose step=%d elapsed=%s context_done=%t", step, time.Since(proposeStarted), ctx.Err() != nil)
		if replayErr == nil {
			var ticket v2input.JobTicket
			if reply.Status != 202 || json.Unmarshal(reply.Body, &ticket) != nil || ticket.RunID != request.RunID || ticket.WorkflowID != request.WorkflowID || ticket.State != "queued" {
				t.Fatal("wrong original final receipt", reply)
			}
			accepted = true
			break
		}
		if !errors.Is(replayErr, v2input.ErrConflict) {
			t.Fatal("public preparation retry", replayErr)
		}
		recoverStarted := time.Now()
		_, locked, recoverErr := job.Recover(ctx, who, f.Tenant, request.RunID)
		t.Logf("NATIVE_SCENE_PHASE phase=recover step=%d elapsed=%s context_done=%t", step, time.Since(recoverStarted), ctx.Err() != nil)
		if recoverErr != nil || !locked {
			t.Fatal("ordinary durable recovery", locked, recoverErr)
		}
		rows, err := owner.Query(ctx, `SELECT t.id FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
   WHERE r.tenant_id=$1 AND t.run_id<>$2 AND t.state='ready' AND jsonb_path_exists(r.config_snapshot,'$.*.parent_run_id ? (@ == $parent)',jsonb_build_object('parent',$2::text)) ORDER BY t.id`, f.Tenant, request.RunID)
		if err != nil {
			t.Fatal(err)
		}
		var tasks []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err = rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			tasks = append(tasks, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) == 0 {
			// The last recovery pass can seal the parent without emitting more private
			// tasks. Re-evaluate the public receipt before declaring a stalled chain.
			if reply, e := service.Propose(ctx, who, f.Tenant, key, f.Intent); e == nil && reply.Status == 202 {
				continue
			}
			t.Fatal("ordinary preparation stalled without a ready child")
		}
		for _, id := range tasks {
			probeNativeWireTaskOutputs(t, ctx, id)
		}
	}
	if !accepted {
		t.Fatal("ordinary preparation did not produce original sealed receipt")
	}
	// New Scene commands must recover via their frozen detector locator, not
	// depend on an ordinary_parent tag absent from initial person preparation.
	located, locatedKey, found, err := reader.readOrdinaryParentReceiptRequest(ctx, who, f.Tenant, request.RunID)
	if err != nil || !found || locatedKey != key || located.RequestSHA != request.RequestSHA {
		t.Fatal("scene detector accepted locator", found, err)
	}
	assertOrdinaryReceiptCanonicalBoundary(t,ctx,reader,who,f.Tenant,request.RunID,!f.LegacyOrdinary)
	t.Log("V27_ORDINARY_SCENE_FINAL_PARENT_AND_RECEIPT_OK")
	rows, err := owner.Query(ctx, "SELECT id FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 AND operation='semantic.transfer' AND state='ready' ORDER BY node_key", f.Tenant, request.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var transfers []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		transfers = append(transfers, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(transfers) == 0 {
		t.Fatal("no actual final transfer tasks", err)
	}
	for _, id := range transfers {
		probeNativeWireTaskOutputs(t, ctx, id)
	}
	// Each transfer's nested Python probe invokes the result-bound independent
	// scene verifier and actual adoption coordinator after canonical commit.
	var adopted int
	if err = owner.QueryRow(ctx, `SELECT count(*) FROM photo.tasks t JOIN photo.inbox a ON a.tenant_id=t.tenant_id AND a.consumer='v2-adopt' AND a.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id) WHERE t.tenant_id=$1 AND t.run_id=$2 AND t.operation='semantic.transfer'`, f.Tenant, request.RunID).Scan(&adopted); err != nil || adopted != len(transfers) {
		t.Fatal("final candidates not independently adopted", adopted, err)
	}
	retired, err := json.Marshal(map[string]any{"Accepted": map[string]any{"Tenant": f.Tenant, "Actor": f.Actor, "Session": f.Session, "Key": key, "Intent": f.Intent, "Scene": true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PHOTO_TEST_SEMANTIC_CREATE_RECEIPT", string(retired))
	TestIntegrationOrdinaryReceiptLocatorAfterSourceRetirement(t)
	t.Log("V27_ORDINARY_SCENE_TRANSFER_TAIL_OK")
}
