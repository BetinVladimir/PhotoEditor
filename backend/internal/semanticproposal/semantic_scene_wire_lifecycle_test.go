package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	workflow "photoeditor/backend/internal/taskrecovery"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
	"time"
)

// Called by the reproducible native-scene-wire Python fixture after actual
// detector bootstrap. Imported qualifications and model engines are explicit
// test doubles; source/Params/transport/results use versioned object IO.
func probeSceneLifecycle(t *testing.T, ctx context.Context, who auth.Identity, parent detectorDraftPlan, reader SQLTransferSourceReader, scheduler *pgxpool.Pool, documents v2draft.Publisher, transport artifactjson.Publisher, catalog v2draft.ArtifactCatalog, color TransferColorProfile, pins [2]v2input.SemanticQualificationPin) {
	t.Helper()
	admin, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var parentCount int
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=$2", parent.request.TenantID, parent.request.RunID).Scan(&parentCount); err != nil || parentCount != 0 {
		t.Fatal("scene prerequisites must precede original parent admission", err)
	}
	producer := func(operation string, pin v2input.SemanticQualificationPin) TransferSceneProducer {
		return TransferSceneProducer{Report: pin.Report, Expected: v2qualification.Expected{Operation: operation, Version: "1", Backend: pin.Backend, ModelLockSHA: pin.ModelLockSHA, RuntimeLockSHA: pin.RuntimeLockSHA, DeviceProfileSHA: pin.DeviceProfileSHA, DatasetSHA: pin.DatasetSHA, IssuerID: pin.IssuerID, RequiredChecks: []string{"quality"}, TestFixtureSHAs: map[string]struct{}{strings.Repeat("e", 64): {}}, EvidenceSHAs: map[string]struct{}{strings.Repeat("f", 64): {}}, CalibrationSHAs: map[string]struct{}{pin.Calibration.SHA256: {}}, LicenseCleared: true}, Costs: v2input.SemanticQualifiedCosts{VRAMBytes: 128 << 20}}
	}
	p := TransferDraftPreparer{Reader: reader, Scheduler: scheduler, Documents: documents, TransportDocuments: transport, ReservedBytes: 128 << 20, Configuration: TransferPreparation{Color: color, RAM: 8 << 30, VRAM: 1 << 30, Scratch: 256 << 20, Output: 32 << 20, MetadataBytes: 16 << 20, Scene: &TransferScenePreparation{Scene: producer("scene.parse", pins[0]), Depth: producer("depth.predict", pins[1])}}}
	for _, operation := range []string{"scene.parse", "depth.predict"} {
		image, err := reader.Read(ctx, who, parent.request.TenantID, parent.request.Intent.Reference)
		if err != nil {
			t.Fatal(err)
		}
		anchor, err := compileSemanticSceneStageAnchor(ctx, who, reader.ProjectID, parent.request, parent.request.Intent.Reference, image, operation)
		if err != nil {
			t.Fatal(err)
		}
		interrupted := p
		failure := &nativePersonFailTransport{Run: anchor.run, Delegate: transport}
		interrupted.TransportDocuments = failure
		if _, err = interrupted.advanceSemanticSceneStage(ctx, who, parent, parent.request.Intent.Reference, catalog, operation); err == nil || failure.Calls != 1 {
			t.Fatal("expected publication interruption after committed draft", err)
		}
		saved, present, err := reader.readSemanticSceneStageSaved(ctx, who, parent.request, anchor)
		if err != nil || !present || saved.Sealed || saved.RunState != "draft" || saved.TaskState != "blocked" {
			t.Fatal("pending draft not recoverable", err)
		}
		waiting, err := p.advanceSemanticSceneStage(ctx, who, parent, parent.request.Intent.Reference, catalog, operation)
		if err != nil || waiting.phase != nativePreparationWaiting || waiting.plan.shell.Run != anchor.run {
			t.Fatal("recover exact pending child", err)
		}
		repeated, err := p.advanceSemanticSceneStage(ctx, who, parent, parent.request.Intent.Reference, catalog, operation)
		if err != nil || repeated.plan.seal != waiting.plan.seal || repeated.phase != nativePreparationWaiting {
			t.Fatal("sealed retry changed config/profile", err)
		}
		if os.Getenv("PHOTO_TEST_SCENE_PAIRED_PREPARE_ONLY") == "1" {
			var state string
			var sealed bool
			var results int
			if err := admin.QueryRow(ctx, `SELECT t.state,(r.kind='v2' AND r.graph_sealed),(SELECT count(*) FROM photo.task_results z WHERE z.task_id=t.id) FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.tenant_id=$1 AND t.id=$2`, parent.request.TenantID, anchor.task).Scan(&state, &sealed, &results); err != nil || state != "ready" || !sealed || results != 0 {
				t.Fatal("paired source must be real sealed ready child", state, sealed, results, err)
			}
			return
		}
		// Use this newly constructed child, rather than a preexisting external
		// success case, for current authority and clock-expiry refusals.
		probeSceneLiveFixtureRefusals(t, ctx, admin, parent.request.TenantID, anchor.run, anchor.task, who.UserID, waiting.plan.prepared.published.ref.ArtifactID)
		// Real reserve_dispatch -> claim -> mTLS gateway Begin/Put/Complete -> worker
		// commit_success -> typed result trigger; no invented attempt/fence/result.
		probeNativeWireTaskOutputs(t, ctx, anchor.task)
		// The first actual attempt uploads then fails retryably. Its ready orphans
		// are physically purged and refunded by the Python maintenance caller.
		recovery := workflow.Recovery{DB: scheduler}
		if n, err := recovery.Recover(ctx, 10); err != nil || n < 1 {
			t.Fatal("scene retry scheduling", n, err)
		}
		var next time.Time
		if err = admin.QueryRow(ctx, "SELECT next_attempt_at FROM photo.tasks WHERE id=$1 AND state='retry_wait' AND retry_count=1", anchor.task).Scan(&next); err != nil {
			t.Fatal(err)
		}
		delay := time.Until(next) + 20*time.Millisecond
		if delay > 7*time.Second {
			t.Fatal("unexpected scene retry delay", delay)
		}
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				t.Fatal(ctx.Err())
			}
		}
		if n, err := recovery.Recover(ctx, 10); err != nil || n < 1 {
			t.Fatal("scene retry activation", n, err)
		}
		probeNativeWireTaskOutputs(t, ctx, anchor.task)
		result, err := reader.readSemanticSceneStageResult(ctx, who, waiting.plan)
		if err != nil {
			t.Fatal("canonical scene/depth result accessor", err)
		}
		if result.value.Task != anchor.task || result.value.Root.ArtifactID == result.value.Pixels.ArtifactID {
			t.Fatal("typed outputs lost identity")
		}
		completed, err := p.advanceSemanticSceneStage(ctx, who, parent, parent.request.Intent.Reference, catalog, operation)
		if err != nil || completed.phase != nativePreparationSucceeded {
			t.Fatal("finish scene/depth", err)
		}
		again, err := p.advanceSemanticSceneStage(ctx, who, parent, parent.request.Intent.Reference, catalog, operation)
		if err != nil || again.phase != nativePreparationSucceeded || again.result.value.Result != completed.result.value.Result {
			t.Fatal("completion replay changed canonical result", err)
		}
		err = database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
			if _, err := reader.beginSemanticSceneStage(ctx, tx, who, parent, waiting.plan); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		// Observe committed settlement through the fixture administrator only.
		// The actual repeated producer transition above remains photo_scheduler.
		var valid bool
		err = admin.QueryRow(ctx, `SELECT r.status='completed' AND q.state='released' AND q.bytes=0
    AND (SELECT count(*) FROM photo.run_events WHERE tenant_id=r.tenant_id AND run_id=r.id AND type='v2.run.completed')=1
    AND (SELECT count(*) FROM photo.outbox WHERE tenant_id=r.tenant_id AND payload->>'run_id'=r.id::text AND routing_key='v2.run.completed')=1
    AND (SELECT count(*) FROM photo.task_results WHERE tenant_id=r.tenant_id AND task_id=$3)=1
    FROM photo.runs r JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id
    WHERE r.tenant_id=$1 AND r.id=$2`, parent.request.TenantID, anchor.run, anchor.task).Scan(&valid)
		if err != nil || !valid {
			t.Fatal("repeated finish changed settlement/events", err)
		}
	}
	t.Log("V27_SCENE_DEPTH_LIFECYCLE_OK")
}

func probeSceneLiveFixtureRefusals(t *testing.T, ctx context.Context, admin *pgxpool.Pool, tenant, run, task, actor, params uuid.UUID) {
	t.Helper()
	var transactions []pgx.Tx
	defer func() {
		for _, tx := range transactions {
			_ = tx.Rollback(ctx)
		}
	}()
	open := func(readonly bool) pgx.Tx {
		t.Helper()
		options := pgx.TxOptions{}
		if readonly {
			options.AccessMode = pgx.ReadOnly
		}
		tx, err := admin.BeginTx(ctx, options)
		if err != nil {
			t.Fatal(err)
		}
		transactions = append(transactions, tx)
		if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
			t.Fatal(err)
		}
		return tx
	}
	check := func(tx pgx.Tx, want bool) {
		t.Helper()
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
			t.Fatal(err)
		}
		var live bool
		if err := tx.QueryRow(ctx, "SELECT photo.execution_source_live($1)", task).Scan(&live); err != nil || live != want {
			t.Fatalf("source live want=%v got=%v err=%v", want, live, err)
		}
	}
	ro := open(true)
	check(ro, true)
	_ = ro.Rollback(ctx)
	for _, change := range []struct {
		sql   string
		value uuid.UUID
	}{
		{"UPDATE photo.users SET status='disabled' WHERE id=$1", actor},
		{"UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", params},
		{"UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE operation=(SELECT operation FROM photo.tasks WHERE id=$1) AND backend=(SELECT execution_profile->>'backend' FROM photo.tasks WHERE id=$1)", task},
	} {
		tx := open(false)
		tag, err := tx.Exec(ctx, change.sql, change.value)
		if err != nil || tag.RowsAffected() == 0 {
			t.Fatal("refusal fixture did not mutate", err)
		}
		check(tx, false)
		_ = tx.Rollback(ctx)
	}
	// Internal execution derives authority from the actual task, not an API GUC.
	// Public selectors and sealing still require the exact API tenant context.
	snapshot := `SELECT jsonb_build_object('task',to_jsonb(t),'run',to_jsonb(r),'usage',to_jsonb(u),
   'quota',(SELECT coalesce(jsonb_agg(to_jsonb(q) ORDER BY to_jsonb(q)::text),'[]') FROM photo.quota_reservations q WHERE q.tenant_id=t.tenant_id AND q.holder_id=t.run_id),
   'outbox',(SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY to_jsonb(o)::text),'[]') FROM photo.outbox o WHERE o.tenant_id=t.tenant_id))::text
   FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id JOIN photo.tenant_usage u ON u.tenant_id=t.tenant_id WHERE t.id=$1`
	publicCheck := func(tx pgx.Tx, want int) {
		t.Helper()
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
			t.Fatal(err)
		}
		var config []byte
		var creator uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT config_snapshot,created_by FROM photo.runs WHERE tenant_id=$1 AND id=$2", tenant, run).Scan(&config, &creator); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
			t.Fatal(err)
		}
		var reports, documents int
		err := tx.QueryRow(ctx, `SELECT
   (SELECT count(*) FROM photo.semantic_proposal_scene_qualification_object(r.tenant_id,(r.config_snapshot->>'project_id')::uuid,r.created_by,(c->>'auth_session_id')::uuid,c->'qualification_expected')),
   (SELECT count(*) FROM photo.semantic_defect_version_params_object(r.tenant_id,(r.config_snapshot->>'project_id')::uuid,r.created_by,(c->>'auth_session_id')::uuid,(c->'source'->>'asset_version_id')::uuid,(c->'source'->'source'->>'artifact_id')::uuid,c->'source'->'source'->>'sha256',(c->>'params_document')::uuid,(c->'params'->>'artifact_id')::uuid,c->'params'->>'sha256'))
   FROM (SELECT $1::uuid tenant_id,$2::jsonb config_snapshot,$3::uuid created_by) r CROSS JOIN LATERAL (SELECT r.config_snapshot->'v27_scene_prepare' c) v`, tenant, string(config), creator).Scan(&reports, &documents)
		if err != nil || reports != want || documents != want {
			t.Fatal("public scene selector tenant boundary", reports, documents, err)
		}
		if want == 0 {
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
				t.Fatal(err)
			}
			var sealed bool
			if err := tx.QueryRow(ctx, "SELECT photo.seal_v2_graph($1,$2)", tenant, run).Scan(&sealed); err != nil || sealed {
				t.Fatal("public scene seal accepted foreign context", err)
			}
		}
	}
	ro = open(true)
	publicCheck(ro, 1)
	_ = ro.Rollback(ctx)
	for _, guc := range []string{"", uuid.NewString()} {
		tx := open(false)
		if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", guc); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
			t.Fatal(err)
		}
		var before, after string
		if err := tx.QueryRow(ctx, snapshot, task).Scan(&before); err != nil {
			t.Fatal(err)
		}
		publicCheck(tx, 0)
		check(tx, true)
		if err := tx.QueryRow(ctx, snapshot, task).Scan(&after); err != nil || after != before {
			t.Fatal("scene context boundary changed state", err)
		}
		_ = tx.Rollback(ctx)
	}
	// Exact foreign tenant/task private scope refusals retain task authority.
	for _, wrongTenant := range []bool{true, false} {
		tx := open(false)
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
			t.Fatal(err)
		}
		var before, after string
		if err := tx.QueryRow(ctx, snapshot, task).Scan(&before); err != nil {
			t.Fatal(err)
		}
		tenantArg, taskArg := tenant, task
		if wrongTenant {
			tenantArg = uuid.New()
		} else {
			taskArg = uuid.New()
		}
		var live bool
		if err := tx.QueryRow(ctx, "SELECT photo.semantic_scene_scope_live($1,$2,$3)", tenantArg, run, taskArg).Scan(&live); err != nil || live {
			t.Fatal("private scene foreign binding accepted", wrongTenant, err)
		}
		if err := tx.QueryRow(ctx, snapshot, task).Scan(&after); err != nil || after != before {
			t.Fatal("private scene refusal wrote state", err)
		}
		_ = tx.Rollback(ctx)
	}
	ro = open(true)
	check(ro, true)
	_ = ro.Rollback(ctx)
}
