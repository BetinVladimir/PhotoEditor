package v2render

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
	"time"
)

// Stage a fresh eligible batch after the direct recovery tests, without
// publishing its inputs. The workflow runtime test must discover this batch.
func TestIntegrationStageRenderRuntimeBatch(t *testing.T) {
	path := os.Getenv("PHOTO_RENDER_TYPED_FIXTURE")
	raw := os.Getenv("PHOTO_RENDER_OBJECT_CONFIG")
	if path == "" || raw == "" {
		t.Skip("live typed recovery fixture required")
	}
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	data, err := os.ReadFile(path + ".recovery")
	if err != nil {
		t.Fatal(err)
	}
	var evidence renderBatchRestartEvidence
	if json.Unmarshal(data, &evidence) != nil {
		t.Fatal("invalid checkpoint")
	}
	var config objectstore.Config
	if json.Unmarshal([]byte(raw), &config) != nil {
		t.Fatal("invalid object configuration")
	}
	objects, err := objectstore.New(config)
	if err != nil {
		t.Fatal("object fixture unavailable")
	}
	admin, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("database fixture unavailable")
	}
	t.Cleanup(admin.Close)
	if err := admin.QueryRow(ctx, "SELECT auth_epoch FROM photo.users WHERE id=$1", evidence.Actor.UserID).Scan(&evidence.Actor.Epoch); err != nil {
		t.Fatal(err)
	}
	poolFor := func(role string) *pgxpool.Pool {
		cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
		if err != nil {
			t.Fatal("invalid database fixture")
		}
		cfg.ConnConfig.RuntimeParams["role"] = role
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal("role fixture unavailable")
		}
		t.Cleanup(pool.Close)
		return pool
	}
	scheduler, api := poolFor("photo_scheduler"), poolFor("photo_api")
	if _, err := (SQLRenderQualificationResolver{API: api}).Resolve(ctx, evidence.Actor, evidence.Tenant, evidence.Project); err == nil || domain.AsError(err).Code != "RENDER_UNAVAILABLE" {
		t.Fatal("unqualified render runtime became available", err)
	}
	factory := SQLRenderBatchFactory{API: api, Scheduler: scheduler, Sources: objects, Documents: objects, SourceBucket: config.Bucket, DocumentBucket: config.Bucket}
	coordinator, err := factory.ForProject(ctx, evidence.Project)
	if err != nil {
		t.Fatal(err)
	}
	var requests []RenderDraftRequest
	var inventories [][]v2input.Entry
	for _, part := range evidence.Parts {
		var stored json.RawMessage
		if err := admin.QueryRow(ctx, "SELECT config_snapshot FROM photo.runs WHERE id=$1 AND tenant_id=$2", part.RunID, evidence.Tenant).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		stored, err = canonical.Bytes(stored)
		if err != nil {
			t.Fatal(err)
		}
		workflow, err := v2input.WorkflowID(part.RunID)
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := DecodeRecoveredRenderPlan(RecoveredRenderDraft{RunID: part.RunID, WorkflowID: workflow, ProjectID: evidence.Project, SnapshotID: evidence.Snapshot, Config: stored, ConfigSHA: part.ConfigSHA, PolicySHA: "", ReservedBytes: part.OutputBytes})
		if err != nil {
			t.Fatal(err)
		}
		for _, recovered := range nodes {
			var profile struct {
				Backend    string `json:"backend"`
				RuntimeSHA string `json:"render_runtime_sha"`
				TileSize   int    `json:"tile_size"`
				AlphaMode  string `json:"alpha_mode"`
			}
			if json.Unmarshal(recovered.Node.Profile, &profile) != nil {
				t.Fatal("invalid frozen profile")
			}
			binding := recovered.Evidence.Recipe.Scalars.Source
			loader := RenderRequestLoader{Catalog: v2draft.SQLArtifactCatalog{DB: api, Bucket: config.Bucket, ProjectID: evidence.Project, AssetVersionID: binding.AssetVersionID}, Objects: objects}
			request, err := loader.Load(ctx, evidence.Tenant, evidence.Actor, RenderSelectionItem{Binding: binding, Recipe: recovered.Node.Params}, RenderExecutionChoice{Backend: profile.Backend, RuntimeSHA: profile.RuntimeSHA, TileSize: profile.TileSize, AlphaMode: profile.AlphaMode})
			if err != nil || string(request.RecipeRaw) != recovered.Node.ParamsJCS || string(request.SourceRaw) != recovered.Node.SourceJCS {
				t.Fatal("pinned object request differs from saved evidence", err)
			}
			entries, err := ResolveRenderInputs(ctx, evidence.Actor, evidence.Tenant, request.Binding, request.RecipeRef, request.RecipeRaw, request.SourceRaw, coordinator.Inputs.Resolver)
			if err != nil {
				t.Fatal(err)
			}
			requests = append(requests, request)
			inventories = append(inventories, entries)
		}
	}
	batch, err := v2input.RunID(evidence.Tenant, evidence.Actor.UserID, "fixture-render-calibrated")
	if err != nil {
		t.Fatal(err)
	}
	parts, err := CompileRenderBatchPlans(batch, evidence.Project, evidence.Snapshot, requests, inventories)
	if err != nil {
		t.Fatal(err)
	}
	var policy string
	if err := admin.QueryRow(ctx, "SELECT config_snapshot->>'policy_sha' FROM photo.runs WHERE id=$1", evidence.Batch).Scan(&policy); err != nil {
		t.Fatal(err)
	}
	selection := make([]RenderSelectionItem, len(requests))
	for i, request := range requests {
		selection[i] = RenderSelectionItem{Binding: request.Binding, Recipe: request.RecipeRef}
	}
	expectedBatch, submissionSHA, err := CompileRenderSubmissionIdentity(evidence.Tenant, evidence.Actor.UserID, evidence.Project, "fixture-render-calibrated", RenderBatchSubmissionRequest{SnapshotID: evidence.Snapshot, PolicySHA: policy, Selection: selection})
	if err != nil || expectedBatch != batch {
		t.Fatal("submission identity", err)
	}
	factory.SubmissionSHA = submissionSHA
	factory.SubmissionKey = "fixture-render-calibrated"
	submissionStore := SQLRenderDraftStore{DB: scheduler}
	if state, err := submissionStore.ProbeSubmission(ctx, evidence.Tenant, batch, evidence.Actor, submissionSHA); err != nil || state.Found {
		t.Fatal("missing submission probe", err, state)
	}
	if state, err := submissionStore.ProbeSubmissionKey(ctx, evidence.Tenant, evidence.Project, batch, evidence.Actor, "fixture-render-calibrated", submissionSHA); err != nil || state.Found {
		t.Fatal("missing global key probe", err, state)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,'V2.Admit','fixture-render-calibrated',repeat('0',64),clock_timestamp()+interval '24 hours')`, evidence.Tenant, evidence.Actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := submissionStore.ProbeSubmissionKey(ctx, evidence.Tenant, evidence.Project, batch, evidence.Actor, "fixture-render-calibrated", submissionSHA); !errors.Is(err, v2input.ErrConflict) {
		t.Fatal("cross-workflow shared key conflict", err)
	}
	if _, err := admin.Exec(ctx, "DELETE FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='V2.Admit' AND key='fixture-render-calibrated' AND request_hash=repeat('0',64)", evidence.Tenant, evidence.Actor.UserID); err != nil {
		t.Fatal(err)
	}
	first := requests[0]
	choice := RenderExecutionChoice{Backend: first.Backend, RuntimeSHA: first.RuntimeSHA, TileSize: first.TileSize, AlphaMode: first.AlphaMode, Provider: first.Provider}
	counted := &countedRenderSource{SessionAnalysisObjectReader: objects}
	factory.Sources = counted
	if plans, err := factory.PrepareQualifiedSelection(ctx, evidence.Tenant, evidence.Actor, batch, evidence.Project, evidence.Snapshot, policy, selection, choice); err == nil || domain.AsError(err).Code != "RENDER_UNAVAILABLE" || len(plans) != 0 || counted.opens != 0 {
		t.Fatal("unqualified selection crossed source IO boundary", err, counted.opens)
	}
	for _, duplicate := range []bool{false, true} {
		invalid := append([]RenderSelectionItem(nil), selection...)
		if duplicate {
			invalid[len(invalid)-1] = invalid[0]
		} else {
			invalid[len(invalid)-1].Recipe.SHA256 = "invalid"
		}
		if plans, err := factory.PrepareSelection(ctx, evidence.Tenant, evidence.Actor, batch, evidence.Project, evidence.Snapshot, policy, invalid, choice); !errors.Is(err, v2input.ErrInvalid) || len(plans) != 0 || counted.opens != 0 {
			t.Fatal("invalid late selection accessed source objects", err, counted.opens)
		}
	}
	frozen, err := v2draft.DecodeFrozenRenderRecipe(first.RecipeRaw)
	if err != nil {
		t.Fatal(err)
	}
	restoreWrongModel, _ := seedRenderQualificationMetadata(t, ctx, admin, evidence, "0000000000000000000000000000000000000000000000000000000000000000", choice)
	if plans, err := factory.PrepareQualifiedSelection(ctx, evidence.Tenant, evidence.Actor, batch, evidence.Project, evidence.Snapshot, policy, selection, choice); err == nil || domain.AsError(err).Code != "RENDER_UNAVAILABLE" || len(plans) != 0 || counted.opens == 0 {
		t.Fatal("recipe model mismatch not checked after document loading", err, counted.opens)
	}
	var modelMismatchStaged int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE id=$1", batch).Scan(&modelMismatchStaged); err != nil || modelMismatchStaged != 0 {
		t.Fatal("wrong model staged batch", err, modelMismatchStaged)
	}
	restoreWrongModel()
	counted.opens = 0
	restoreQualification, qualificationPolicy := seedRenderQualificationMetadata(t, ctx, admin, evidence, frozen.Scalars.ModelLockSHA, choice)
	wrongRuntime := choice
	wrongRuntime.RuntimeSHA = "0000000000000000000000000000000000000000000000000000000000000000"
	if plans, err := factory.PrepareQualifiedSelection(ctx, evidence.Tenant, evidence.Actor, batch, evidence.Project, evidence.Snapshot, policy, selection, wrongRuntime); err == nil || domain.AsError(err).Code != "RENDER_UNAVAILABLE" || len(plans) != 0 || counted.opens != 0 {
		t.Fatal("wrong runtime crossed object IO boundary", err, counted.opens)
	}
	revoking := &revokingRenderSource{SessionAnalysisObjectReader: objects, DB: admin, Policy: qualificationPolicy}
	factory.Sources = revoking
	if plans, err := factory.PrepareQualifiedSelection(ctx, evidence.Tenant, evidence.Actor, batch, evidence.Project, evidence.Snapshot, policy, selection, choice); err == nil || domain.AsError(err).Code != "RENDER_UNAVAILABLE" || len(plans) != 0 || !revoking.revoked {
		t.Fatal("post IO qualification revocation ignored", err)
	}
	var staged int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE id=$1", batch).Scan(&staged); err != nil || staged != 0 {
		t.Fatal("revoked qualification staged batch", err, staged)
	}
	if _, err := admin.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=true WHERE id=$1", qualificationPolicy); err != nil {
		t.Fatal(err)
	}
	factory.Sources = counted
	qualification, err := (SQLRenderQualificationResolver{API: api}).Resolve(ctx, evidence.Actor, evidence.Tenant, evidence.Project)
	if err != nil {
		t.Fatal(err)
	}
 assertRenderQualificationWithdrawnDuringLookup(t,ctx,admin,api,evidence)
	calibration := RenderCalibration{Qualification: qualification, EvidenceSHA: strings.Repeat("e", 64), ICCSHA: frozen.Scalars.ICCLockSHA, TileSize: choice.TileSize, AlphaMode: choice.AlphaMode}
	dimensions := map[[2]int64]bool{}
	for _, request := range requests {
		compiled, err := CompileRenderEvidence(request.Binding, request.RecipeRef, request.RecipeRaw, request.SourceRaw)
		if err != nil {
			t.Fatal(err)
		}
		size := [2]int64{int64(compiled.Source.Canvas.Width), int64(compiled.Source.Canvas.Height)}
		if !dimensions[size] {
			calibration.Samples = append(calibration.Samples, RenderCalibrationSample{Width: size[0], Height: size[1]})
			dimensions[size] = true
		}
	}
	// Synthetic zero-overhead profile exercises plumbing, not GPU calibration.
	invalidCalibration := calibration
	invalidCalibration.Qualification.DeviceSHA = strings.Repeat("0", 64)
	counted.opens = 0
	invalidCatalog := &fixtureRenderCalibrationCatalog{Calibration: invalidCalibration}
	if plans, err := factory.PrepareCalibratedSelection(ctx, evidence.Tenant, evidence.Actor, batch, evidence.Project, evidence.Snapshot, policy, selection, invalidCatalog); err == nil || domain.AsError(err).Code != "RENDER_UNAVAILABLE" || len(plans) != 0 || counted.opens != 0 {
		t.Fatal("unmatched calibration crossed IO", err, counted.opens)
	}
	changingCatalog := &fixtureRenderCalibrationCatalog{Calibration: calibration, ChangeOnRecheck: true}
	if plans, err := factory.PrepareCalibratedSelection(ctx, evidence.Tenant, evidence.Actor, batch, evidence.Project, evidence.Snapshot, policy, selection, changingCatalog); err == nil || domain.AsError(err).Code != "RENDER_UNAVAILABLE" || len(plans) != 0 || counted.opens == 0 || changingCatalog.Calls != 2 {
		t.Fatal("changed calibration staged batch", err, counted.opens, changingCatalog.Calls)
	}
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE id=$1", batch).Scan(&staged); err != nil || staged != 0 {
		t.Fatal("changed calibration persisted parent", err, staged)
	}
	catalogRaw, catalogSHA := calibrationManifest(t, []RenderCalibration{calibration})
	catalog, err := NewPinnedRenderCalibrationCatalog(catalogRaw, catalogSHA)
	if err != nil {
		t.Fatal(err)
	}
	factory.Sources = objects
	stopped := &stoppedRenderDocuments{Objects: objects}
	factory.Documents = stopped
	service := SQLRenderSubmissionService{Factory: factory, Calibrations: catalog, Timeout: 2 * time.Minute}
	intent := RenderBatchSubmissionRequest{SnapshotID: evidence.Snapshot, PolicySHA: policy, Selection: selection}
	ticket, prepareErr := service.Queue(ctx, evidence.Actor, evidence.Tenant, evidence.Project, "fixture-render-calibrated", intent)
	if !errors.Is(prepareErr, io.ErrUnexpectedEOF) || stopped.puts != 1 || ticket.RunID != uuid.Nil {
		t.Fatal("submission did not retain staged plan before publication", prepareErr, stopped.puts, ticket)
	}
	prepared := parts
	state := func() [4]int64 {
		var out [4]int64
		if err := admin.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM photo.runs WHERE tenant_id=$1),
 (SELECT count(*) FROM photo.tasks WHERE tenant_id=$1),
 (SELECT count(*) FROM photo.backend_documents WHERE tenant_id=$1),
 (SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1)`, evidence.Tenant).Scan(&out[0], &out[1], &out[2], &out[3]); err != nil {
			t.Fatal(err)
		}
		return out
	}
	probed, err := submissionStore.ProbeSubmission(ctx, evidence.Tenant, batch, evidence.Actor, submissionSHA)
	if err != nil || !probed.Found || probed.State != "draft" || probed.CancelRequested {
		t.Fatal("persisted submission probe", err, probed)
	}
	if _, err := submissionStore.ProbeSubmission(ctx, evidence.Tenant, batch, evidence.Actor, strings.Repeat("0", 64)); !errors.Is(err, v2input.ErrConflict) {
		t.Fatal("changed submission probe", err)
	}
	foreign := evidence.Actor
	foreign.UserID = uuid.New()
	if result, err := submissionStore.ProbeSubmission(ctx, evidence.Tenant, batch, foreign, submissionSHA); err != nil || result.Found {
		t.Fatal("foreign actor probe disclosed submission", err, result)
	}
	if _, err := admin.Exec(ctx, "UPDATE photo.runs SET v26_submission_request_sha=NULL WHERE tenant_id=$1 AND id=$2", evidence.Tenant, batch); err == nil {
		t.Fatal("submission identity cleared")
	}
	if keyed, err := submissionStore.ProbeSubmissionKey(ctx, evidence.Tenant, evidence.Project, batch, evidence.Actor, "fixture-render-calibrated", submissionSHA); err != nil || keyed != probed {
		t.Fatal("shared key replay did not match parent", err, keyed, probed)
	}
	var savedRequestSHA string
	var savedResource uuid.UUID
	var savedResponse *int
	if err := admin.QueryRow(ctx, "SELECT request_hash,resource_id,response_status FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='V2.Admit' AND key='fixture-render-calibrated'", evidence.Tenant, evidence.Actor.UserID).Scan(&savedRequestSHA, &savedResource, &savedResponse); err != nil || savedRequestSHA != submissionSHA || savedResource != batch || savedResponse != nil {
		t.Fatal("pending admission key not atomic with shell", err)
	}
	profileSHA, err := canonical.Hash(calibration)
	if err != nil {
		t.Fatal(err)
	}
	pin := RenderQualificationPin{RenderQualification: qualification, CalibrationSHA: profileSHA}
	pinRaw, err := json.Marshal(pin)
	if err != nil {
		t.Fatal(err)
	}
	pinnedIDs := []uuid.UUID{batch}
	for _, part := range prepared {
		pinnedIDs = append(pinnedIDs, part.RunID)
	}
	var pinCount int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=ANY($2::uuid[]) AND v26_qualification_pin=$3::jsonb", evidence.Tenant, pinnedIDs, string(pinRaw)).Scan(&pinCount); err != nil || pinCount != len(pinnedIDs) {
		t.Fatal("calibration qualification not atomic across parent/children", err, pinCount)
	}
	for _, id := range pinnedIDs {
		_, err := admin.Exec(ctx, "UPDATE photo.runs SET v26_qualification_pin=NULL WHERE tenant_id=$1 AND id=$2", evidence.Tenant, id)
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || denied.Code != "42501" {
			t.Fatal("frozen qualification pin cleared", err)
		}
	}
	verifyFrozenRenderQualificationPolicy(t, ctx, admin, evidence, prepared[0].RunID, qualificationPolicy, qualification)
	beforeReplay := state()
	changedPin := pin
	changedPin.CalibrationSHA = strings.Repeat("0", 64)
	pinStore := SQLRenderDraftStore{DB: scheduler, SubmissionKey: "fixture-render-calibrated", SubmissionSHA: submissionSHA, QualificationPin: &changedPin}
	if err := pinStore.BeginBatch(ctx, evidence.Tenant, batch, evidence.Project, evidence.Snapshot, evidence.Actor, policy, prepared); !errors.Is(err, v2input.ErrConflict) || state() != beforeReplay || stopped.puts != 1 {
		t.Fatal("changed measured profile replaced frozen pin", err)
	}
	owned, lockErr := withRenderBatchLock(ctx, scheduler, evidence.Tenant, batch, func(ctx context.Context) error {
		contenderTicket, contenderErr := service.Queue(ctx, evidence.Actor, evidence.Tenant, evidence.Project, "fixture-render-calibrated", intent)
		if contenderErr == nil || domain.AsError(contenderErr).Code != "DEPENDENCY_UNAVAILABLE" || contenderTicket.RunID != uuid.Nil || stopped.puts != 1 || state() != beforeReplay {
			t.Error("submission entered a batch owned by input recovery", contenderErr, contenderTicket)
		}
		return nil
	})
	if lockErr != nil || !owned {
		t.Fatal("submission contention fixture", lockErr, owned)
	}
	changedStore := SQLRenderDraftStore{DB: scheduler, SubmissionSHA: strings.Repeat("0", 64), SubmissionKey: "fixture-render-calibrated"}
	if err := changedStore.BeginBatch(ctx, evidence.Tenant, batch, evidence.Project, evidence.Snapshot, evidence.Actor, policy, prepared); !errors.Is(err, v2input.ErrConflict) || state() != beforeReplay {
		t.Fatal("changed submission binding mutated batch", err)
	}
	replayed, replayErr := factory.PrepareCalibratedSelection(ctx, evidence.Tenant, evidence.Actor, batch, evidence.Project, evidence.Snapshot, policy, selection, catalog)
	if !errors.Is(replayErr, io.ErrUnexpectedEOF) || stopped.puts != 2 || len(replayed) != len(prepared) || state() != beforeReplay {
		t.Fatal("interrupted preparation replay duplicated persistence/reservation", replayErr, stopped.puts)
	}
	for i := range prepared {
		if replayed[i].RunID != prepared[i].RunID || replayed[i].Plan.ConfigSHA != prepared[i].Plan.ConfigSHA {
			t.Fatal("interrupted preparation replay changed frozen identity")
		}
	}
	service.Calibrations = nil
	counted.opens = 0
	service.Factory.Sources = counted
	ticket, replayErr = service.Queue(ctx, evidence.Actor, evidence.Tenant, evidence.Project, "fixture-render-calibrated", intent)
	if !errors.Is(replayErr, io.ErrUnexpectedEOF) || stopped.puts != 3 || state() != beforeReplay || ticket.RunID != uuid.Nil || counted.opens == 0 {
		t.Fatal("pending submission did not recover frozen inputs without calibration", replayErr, stopped.puts, counted.opens)
	}
	changedOrder := append([]RenderSelectionItem(nil), selection...)
	changedOrder[0], changedOrder[len(changedOrder)-1] = changedOrder[len(changedOrder)-1], changedOrder[0]
	conflicting, conflictErr := factory.PrepareCalibratedSelection(ctx, evidence.Tenant, evidence.Actor, batch, evidence.Project, evidence.Snapshot, policy, changedOrder, catalog)
	if !errors.Is(conflictErr, v2input.ErrConflict) || len(conflicting) == 0 || conflicting[0].Plan.ConfigSHA == prepared[0].Plan.ConfigSHA || stopped.puts != 3 || state() != beforeReplay {
		t.Fatal("changed selection replay replaced or published batch", conflictErr, stopped.puts)
	}
	intent.Selection = changedOrder
	beforeConflictIO := counted.opens
	if _, err := service.Queue(ctx, evidence.Actor, evidence.Tenant, evidence.Project, "fixture-render-calibrated", intent); !errors.Is(err, v2input.ErrConflict) || stopped.puts != 3 || state() != beforeReplay || counted.opens != beforeConflictIO {
		t.Fatal("changed submission intent crossed IO or mutated frozen batch", err)
	}
	for _, part := range prepared {
		var savedSHA string
		if err := admin.QueryRow(ctx, "SELECT config_hash FROM photo.runs WHERE tenant_id=$1 AND id=$2", evidence.Tenant, part.RunID).Scan(&savedSHA); err != nil || savedSHA != part.Plan.ConfigSHA {
			t.Fatal("conflicting replay changed persisted child", err)
		}
	}
	for i := range parts {
		if prepared[i].RunID != parts[i].RunID || prepared[i].Plan.ConfigSHA != parts[i].Plan.ConfigSHA {
			t.Fatal("selection loader changed compiled plan")
		}
		var qualified bool
		if err := admin.QueryRow(ctx, "SELECT photo.v26_render_qualification_ready($1,$2)", evidence.Tenant, prepared[i].RunID).Scan(&qualified); err != nil || !qualified {
			t.Fatal("staged frozen plan qualification gate", err, qualified)
		}
	}
	verifyRenderQualificationLock(t, ctx, admin, evidence.Tenant, prepared[0].RunID, qualificationPolicy)
	restoreQualification()
	var qualifiedAfterRestore bool
	if err := admin.QueryRow(ctx, "SELECT photo.v26_render_qualification_ready($1,$2)", evidence.Tenant, prepared[0].RunID).Scan(&qualifiedAfterRestore); err != nil || qualifiedAfterRestore {
		t.Fatal("disabled qualification still ready at activation", err, qualifiedAfterRestore)
	}
	checkpoint := struct {
		Tenant, Batch uuid.UUID
		Tasks         []uuid.UUID
	}{Tenant: evidence.Tenant, Batch: batch}
	for _, part := range parts {
		for _, task := range part.Plan.Tasks {
			checkpoint.Tasks = append(checkpoint.Tasks, task.ID)
		}
	}
	data, err = json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".runtime", data, 0600); err != nil {
		t.Fatal(err)
	}
}

func verifyRenderQualificationLock(t *testing.T, ctx context.Context, db *pgxpool.Pool, tenant, run, policy uuid.UUID) {
	t.Helper()
	owner, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Rollback(ctx)
	var ready bool
	if err := owner.QueryRow(ctx, "SELECT photo.v26_render_qualification_ready($1,$2)", tenant, run).Scan(&ready); err != nil || !ready {
		t.Fatal("qualification lock owner", err, ready)
	}
	mutations := []struct{ name, sql string }{
		{"policy revocation", "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1"},
		{"report expiry", `UPDATE photo.artifacts report SET expires_at=clock_timestamp()-interval '1 second'
 FROM photo.model_qualifications q,photo.v2_qualification_policies p
 WHERE p.id=$1 AND q.tenant_id=$2 AND q.operation=p.operation AND q.operation_version=p.operation_version
 AND q.model_lock_sha=p.model_lock_sha AND q.runtime_lock_sha=p.runtime_lock_sha
 AND report.tenant_id=q.tenant_id AND report.id=q.report_artifact_id`},
		{"qualification deletion", `DELETE FROM photo.model_qualifications q USING photo.v2_qualification_policies p
 WHERE p.id=$1 AND q.tenant_id=$2 AND q.operation=p.operation AND q.operation_version=p.operation_version
 AND q.model_lock_sha=p.model_lock_sha AND q.runtime_lock_sha=p.runtime_lock_sha`},
	}
	for _, mutation := range mutations {
		peer, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := peer.Exec(ctx, "SET LOCAL lock_timeout='100ms'"); err != nil {
			_ = peer.Rollback(ctx)
			t.Fatal(err)
		}
		args := []any{policy}
		if mutation.name != "policy revocation" {
			args = append(args, tenant)
		}
		_, mutationErr := peer.Exec(ctx, mutation.sql, args...)
		rollbackErr := peer.Rollback(ctx)
		if rollbackErr != nil {
			t.Fatal("peer rollback", rollbackErr)
		}
		var pg *pgconn.PgError
		if !errors.As(mutationErr, &pg) || pg.Code != "55P03" {
			t.Fatal("parallel mutation bypassed activation lock", mutation.name, mutationErr)
		}
	}
	if err := owner.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

// Interrupt publication before storage IO, after real atomic SQL staging and
// document reservation. The independent workflow process must finish the batch.
type stoppedRenderDocuments struct {
	artifactjson.Objects
	puts int
}

type countedRenderSource struct {
	v2input.SessionAnalysisObjectReader
	opens int
}

type revokingRenderSource struct {
	v2input.SessionAnalysisObjectReader
	DB      *pgxpool.Pool
	Policy  uuid.UUID
	revoked bool
}

func (s *revokingRenderSource) Open(ctx context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	if !s.revoked {
		if _, err := s.DB.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", s.Policy); err != nil {
			return nil, err
		}
		s.revoked = true
	}
	return s.SessionAnalysisObjectReader.Open(ctx, obj)
}

func (s *countedRenderSource) Open(ctx context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	s.opens++
	return s.SessionAnalysisObjectReader.Open(ctx, obj)
}

func (s *stoppedRenderDocuments) PutImmutable(context.Context, string, io.Reader, int64, string, string) (objectstore.Object, error) {
	s.puts++
	return objectstore.Object{}, io.ErrUnexpectedEOF
}

// This catalog contains controlled fixture metadata, not measured GPU evidence.
type fixtureRenderCalibrationCatalog struct {
	Calibration     RenderCalibration
	Calls           int
	ChangeOnRecheck bool
}

func (c *fixtureRenderCalibrationCatalog) Resolve(ctx context.Context, q RenderQualification) (RenderCalibration, error) {
	if err := ctx.Err(); err != nil {
		return RenderCalibration{}, err
	}
	c.Calls++
	result := c.Calibration
	result.Samples = append([]RenderCalibrationSample(nil), result.Samples...)
	if c.ChangeOnRecheck && c.Calls > 1 {
		result.EvidenceSHA = strings.Repeat("0", 64)
	}
	return result, nil
}

// Report bytes remain controlled qualification fixtures. Import the second exact
// tuple through the real maintenance helper inside a transaction and roll it all
// back; no synthetic runnable work or policy switch persists.
func verifyFrozenRenderQualificationPolicy(t *testing.T, ctx context.Context, db *pgxpool.Pool, evidence renderBatchRestartEvidence, run, policy uuid.UUID, pin RenderQualification) {
	t.Helper()
	for _, field := range []string{"device_profile_sha", "dataset_sha"} {
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer tx.Rollback(context.Background())
			device, dataset := pin.DeviceSHA, pin.DatasetSHA
			if field == "device_profile_sha" {
				device = strings.Repeat("0", 64)
			} else {
				dataset = strings.Repeat("0", 64)
			}
			if _, err := tx.Exec(ctx, "UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=$1", policy); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO photo.v2_qualification_policies(operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,evidence_shas,license_cleared,enabled,admission_enabled)
SELECT operation,operation_version,model_lock_sha,runtime_lock_sha,backend,$2,$3,issuer_id,required_checks,test_fixture_shas,evidence_shas,true,true,true FROM photo.v2_qualification_policies WHERE id=$1`, policy, device, dataset); err != nil {
				t.Fatal(err)
			}
			body, err := canonical.Marshal(map[string]any{"schema_version": 1, "operation": "render.compose", "version": "1", "model_lock_sha": pin.ModelSHA, "runtime_lock_sha": pin.RuntimeSHA, "device_profile_sha": device, "dataset_sha": dataset, "input_fixture_shas": []string{strings.Repeat("e", 64)}, "checks": []any{map[string]any{"id": "quality", "passed": true, "evidence_sha": strings.Repeat("f", 64)}}, "metrics": map[string]any{"p50_ms": 10, "p95_ms": 20, "peak_vram_bytes": 0, "peak_rss_bytes": 0, "scratch_bytes": 0}, "status": "passed", "issuer_id": evidence.Actor.UserID, "created_at": "2026-09-01T00:00:00Z"})
			if err != nil {
				t.Fatal(err)
			}
			reportSHA, err := canonical.Hash(json.RawMessage(body))
			if err != nil {
				t.Fatal(err)
			}
			var report uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at) VALUES($1,'qualification-metadata-fixture',$2,'fixture-v1',$3,$4,'application/json','ready',clock_timestamp()) RETURNING id`, evidence.Tenant, uuid.NewString(), reportSHA, len(body)).Scan(&report); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_maintenance"); err != nil {
				t.Fatal(err)
			}
			var imported uuid.UUID
			if err := tx.QueryRow(ctx, "SELECT photo.import_v2_qualification($1,$2,$3,$4,$5,$6,$7)", evidence.Tenant, evidence.Project, report, reportSHA, evidence.Actor.UserID, pin.Backend, string(body)).Scan(&imported); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, "RESET ROLE"); err != nil {
				t.Fatal(err)
			}
			var oldReady, newReady bool
			if err := tx.QueryRow(ctx, "SELECT photo.v26_render_qualification_ready_before_pin($1,$2)", evidence.Tenant, run).Scan(&oldReady); err != nil || !oldReady {
				t.Fatal("policy switch did not isolate missing tuple pin", field, err, oldReady)
			}
			if err := tx.QueryRow(ctx, "SELECT photo.v26_render_qualification_ready($1,$2)", evidence.Tenant, run).Scan(&newReady); err != nil || newReady {
				t.Fatal("changed qualified device/dataset accepted frozen calibration", field, err, newReady)
			}
		}()
	}
}
