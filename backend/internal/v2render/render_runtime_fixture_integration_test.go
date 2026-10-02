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
	batch := uuid.New()
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
	prepared, prepareErr := factory.PrepareCalibratedSelection(ctx, evidence.Tenant, evidence.Actor, batch, evidence.Project, evidence.Snapshot, policy, selection, catalog)
	if !errors.Is(prepareErr, io.ErrUnexpectedEOF) || stopped.puts != 1 || len(prepared) != len(parts) {
		t.Fatal("selection preparation did not retain staged plan before publication", prepareErr, stopped.puts, len(prepared))
	}
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
	beforeReplay := state()
	replayed, replayErr := factory.PrepareCalibratedSelection(ctx, evidence.Tenant, evidence.Actor, batch, evidence.Project, evidence.Snapshot, policy, selection, catalog)
	if !errors.Is(replayErr, io.ErrUnexpectedEOF) || stopped.puts != 2 || len(replayed) != len(prepared) || state() != beforeReplay {
		t.Fatal("interrupted preparation replay duplicated persistence/reservation", replayErr, stopped.puts)
	}
	for i := range prepared {
		if replayed[i].RunID != prepared[i].RunID || replayed[i].Plan.ConfigSHA != prepared[i].Plan.ConfigSHA {
			t.Fatal("interrupted preparation replay changed frozen identity")
		}
	}
	changedOrder := append([]RenderSelectionItem(nil), selection...)
	changedOrder[0], changedOrder[len(changedOrder)-1] = changedOrder[len(changedOrder)-1], changedOrder[0]
	conflicting, conflictErr := factory.PrepareCalibratedSelection(ctx, evidence.Tenant, evidence.Actor, batch, evidence.Project, evidence.Snapshot, policy, changedOrder, catalog)
	if !errors.Is(conflictErr, v2input.ErrConflict) || len(conflicting) == 0 || conflicting[0].Plan.ConfigSHA == prepared[0].Plan.ConfigSHA || stopped.puts != 2 || state() != beforeReplay {
		t.Fatal("changed selection replay replaced or published batch", conflictErr, stopped.puts)
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
