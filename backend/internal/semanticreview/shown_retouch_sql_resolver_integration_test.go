package semanticreview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/colorprofile"
	"photoeditor/backend/internal/objectstore"
)

// Requires an adopted, physically rendered preview in a disposable database
// and versioned object bucket. This test publishes real immutable documents
// and seals real receipts; it must never point at deployed tenant data.
func TestIntegrationShownRetouchSQLResolverAndBatchPublication(t *testing.T) {
	fixturePath := os.Getenv("PHOTO_TEST_V27_RETOUCH_APPLY_FIXTURE")
	if fixturePath == "" || os.Getenv("PHOTO_TEST_DISPOSABLE") != "1" {
		t.Skip("disposable adopted retouch preview fixture required")
	}
	for _, name := range []string{"PHOTO_TEST_DATABASE_URL", "PHOTO_TEST_MINIO_BUCKET", "PHOTO_TEST_MINIO_URL", "PHOTO_TEST_MINIO_ACCESS", "PHOTO_TEST_MINIO_SECRET"} {
		if os.Getenv(name) == "" { t.Fatal("explicit disposable database and object store configuration required") }
	}
	var fixture struct {
		Tenant uuid.UUID `json:"tenant_id"`
		Intent uuid.UUID `json:"intent_id"`
		Actor uuid.UUID `json:"actor_id"`
		Session uuid.UUID `json:"session_id"`
		SRGBSHA string `json:"srgb_sha"`
		Catalog json.RawMessage `json:"catalog"`
		Request json.RawMessage `json:"request"`
		Target uuid.UUID `json:"target_id"`
		PreviewFence *PreviewRequest `json:"preview_fence"`
	}
	raw, err := os.ReadFile(fixturePath)
	if err != nil { t.Fatal("fixture file unavailable") }
	if decodeStrict(raw, &fixture) != nil { t.Fatal("invalid fixture document") }
	catalog, err := DecodeExposurePreparationCatalog(fixture.Catalog)
	if err != nil || len(catalog.Tenants) != 1 { t.Fatal("single own tenant catalog required", err) }
	entry := catalog.Tenants[0]
	if entry.TenantID != fixture.Tenant || entry.ShownRetouchExecution == nil || fixture.Intent == uuid.Nil || fixture.Actor == uuid.Nil || fixture.Session == uuid.Nil {
		t.Fatal("incomplete own retouch fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := func(role string) *pgxpool.Pool {
		t.Helper()
		config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
		if err != nil { t.Fatal("invalid disposable database configuration") }
		config.ConnConfig.RuntimeParams["role"] = role
		db, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil { t.Fatal("disposable role connection unavailable") }
		t.Cleanup(db.Close)
		return db
	}
	api, admission := pool("photo_api"), pool("photo_semantic_admission")
	bucket := os.Getenv("PHOTO_TEST_MINIO_BUCKET")
	objects, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: bucket,
		AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil { t.Fatal("disposable object store unavailable") }
	read, err := NewPlanObjectReader(objects, bucket)
	if err != nil { t.Fatal(err) }
	lookup, err := SQLRetouchPreviewResolver(api, objects, bucket)
	if err != nil { t.Fatal(err) }
	lockSHA, err := colorprofile.LoadLockSHA("../../../resources/icc")
	if err != nil { t.Fatal(err) }
	provider, err := NewRetouchPreviewProvider(api, objects, bucket, lookup, func(context.Context, objectstore.Object, time.Time) (string, error) {
		t.Fatal("Apply resolver must not sign presentation URLs")
		return "", ErrInvalid
	}, lockSHA, fixture.SRGBSHA)
	if err != nil { t.Fatal(err) }
	icc, err := os.ReadFile("../../../resources/icc/work_rec2020_gamma24_v1.icc")
	if err != nil { t.Fatal(err) }
	color, err := NewExposureColorProfile(icc)
	if err != nil { t.Fatal(err) }
	installation := ExposureApplyInstallation{API: api, Admission: admission, Objects: objects, Bucket: bucket,
		Color: color, Execution: entry.Execution, ReviewModelSHA: entry.Review.ModelSHA, ReviewDatasetSHA: entry.Review.DatasetSHA,
		DocumentBudgetBytes: entry.Review.DocumentBudgetBytes, ShownRetouchExecution: entry.ShownRetouchExecution, ShownRetouchPreview: provider}
	for _, wrongAPI := range []bool{true, false} {
		invalid := installation
		if wrongAPI { invalid.API = admission } else { invalid.Admission = api }
		if installed, e := InstallExposureApplyService(ctx, invalid); !errors.Is(e, ErrUnavailable) || installed != nil {
			t.Fatal("installation accepted swapped privilege contour", e)
		}
	}
	service, err := InstallExposureApplyService(ctx, installation)
	if err != nil || service == nil || service.Prepare == nil { t.Fatal("live retouch service installation", err) }
	selection, err := NewShownBudgetSelection(*entry.ShownRetouchExecution)
	if err != nil { t.Fatal(err) }
	resolve, err := NewSQLShownRetouchApplyResolver(api, provider, read, entry.Review.DocumentBudgetBytes, entry.Review.ModelSHA, entry.Review.DatasetSHA, selection)
	if err != nil { t.Fatal(err) }
	who := auth.Identity{UserID: fixture.Actor, SessionID: fixture.Session}
	var request ApplyDecision
	if len(fixture.Request) != 0 {
		if fixture.Target != uuid.Nil || fixture.PreviewFence != nil { t.Fatal("ambiguous manual and generated selection") }
		request, err = DecodeApplyDecision(fixture.Request)
		if err != nil { t.Fatal(err) }
	} else {
		if fixture.Target == uuid.Nil || fixture.PreviewFence == nil || fixture.PreviewFence.Validate() != nil {
			t.Fatal("current target and preview fence required for generated selection")
		}
		// Resolve real current review and physical outputs; never synthesize the
		// selected WorkingImage/Presentation IDs or the preview producer receipt.
		scoped, e := PrepareScopedPreviewReview(ctx, api, who, fixture.Tenant, fixture.Intent, fixture.Target, *fixture.PreviewFence,
			read, entry.Review.DocumentBudgetBytes, entry.Review.ModelSHA, entry.Review.DatasetSHA)
		if e != nil || scoped.Review.Intent.Kind != "retouch_region" { t.Fatal("own retouch review required", e) }
		_, selected, e := provider.ReadPhysicalPreview(ctx, who, fixture.Tenant, fixture.Intent, fixture.Target, *fixture.PreviewFence)
		if e != nil { t.Fatal("real preview receipt required", e) }
		request = ApplyDecision{PlanSHA: scoped.Review.PlanRef.SHA256, ExpectedRevision: fixture.PreviewFence.ExpectedRevision,
			TargetDecisions: []TargetDecision{{TargetID: fixture.Target, Decision: "approve", Preview: &PreviewSelection{
				Render: TargetDocument{ArtifactID: selected.Receipt.WorkingImage.ArtifactID, SHA256: selected.Receipt.WorkingImage.SHA256},
				Evidence: TargetDocument{ArtifactID: selected.Receipt.Presentation.ArtifactID, SHA256: selected.Receipt.Presentation.SHA256},
			}}}}
	}
	child := uuid.New()
	resolved, err := resolve(ctx, who, fixture.Tenant, fixture.Intent, child, request)
	if err != nil || len(resolved.RetouchInputs) == 0 || resolved.Recheck == nil { t.Fatal("real SQL/physical resolver", err) }
	prepared, err := PrepareShownRetouchApplyBatch(ctx, api, objects, bucket, who, fixture.Tenant, fixture.Intent, child,
		resolved.Review, resolved.Targets, request, resolved.RetouchInputs, resolved.Recheck)
	if err != nil || prepared.Child != child || prepared.Decision.BatchID == uuid.Nil || len(prepared.ShownTasks) != len(resolved.RetouchInputs) {
		t.Fatal("real full batch publication", err)
	}
	if _, err := VerifyShownRetouchBatch(fixture.Tenant, fixture.Intent, child, resolved.Review, resolved.Targets, request, prepared.ShownTasks); err != nil {
		t.Fatal("published batch lost review binding", err)
	}
	if err := resolved.Recheck(ctx); err != nil { t.Fatal("publication changed current authority", err) }
	// Exercise the public service orchestration as well as explicit preparation.
	// Only the disposable fixture may create this real admitted child and quota.
	key := uuid.NewString()
	response, err := service.Apply(ctx, who, fixture.Tenant, fixture.Intent, key, request)
	if err != nil || response.Status != 200 || response.Replayed || response.ResourceID == nil {
		t.Fatal("service failed atomic retouch Apply", err)
	}
	batch, err := DecodeBatchResult(response.Body)
	if err != nil || batch.Child == nil || batch.BatchID != *response.ResourceID {
		t.Fatal("service returned incomplete admitted batch", err)
	}
	// Owner reads are confined to this explicitly disposable test database.
	// Snapshot full rows, so replay cannot hide extra children, events or quota
	// changes behind a successful response with the same batch identifier.
	auditConfig, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil { t.Fatal("invalid disposable audit configuration") }
	delete(auditConfig.ConnConfig.RuntimeParams, "role")
	audit, err := pgxpool.NewWithConfig(ctx, auditConfig)
	if err != nil { t.Fatal("disposable audit connection unavailable") }
	t.Cleanup(audit.Close)
	snapshot := func() map[string][]byte {
		t.Helper()
		rows := make(map[string][]byte)
		for _, table := range []string{"runs", "tasks", "task_results", "task_attempts", "semantic_targets", "v2_review_batches", "quota_reservations", "tenant_usage", "idempotency_records", "run_events", "outbox", "v2_task_inputs", "artifacts", "output_uploads", "backend_documents", "audit_log", "tenant_event_cursors", "semantic_intents", "v2_workflows"} {
			var raw []byte
			query := "SELECT coalesce(jsonb_agg(payload ORDER BY payload::text),'[]'::jsonb) FROM (SELECT to_jsonb(t) AS payload FROM photo." + table + " t WHERE tenant_id=$1) inventory"
			if err := audit.QueryRow(ctx, query, fixture.Tenant).Scan(&raw); err != nil { t.Fatal("disposable snapshot", table, err) }
			rows[table] = append([]byte(nil), raw...)
		}
		return rows
	}
	beforeReplay := snapshot()
	service.Prepare = func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, ApplyDecision) (PreparedApply, error) {
		t.Fatal("saved replay entered physical preparation")
		return PreparedApply{}, ErrInvalid
	}
	service.Admission = nil
	replay, err := service.Apply(ctx, who, fixture.Tenant, fixture.Intent, key, request)
	if err != nil || !replay.Replayed || replay.Status != response.Status || replay.ResourceID == nil || *replay.ResourceID != *response.ResourceID || !bytes.Equal(replay.Body, response.Body) {
		t.Fatal("saved service receipt did not replay independently of admission", err)
	}
	canceled, stopCanceled := context.WithCancel(ctx)
	stopCanceled()
	if canceledResponse, err := service.Apply(canceled, who, fixture.Tenant, fixture.Intent, key, request); !errors.Is(err, context.Canceled) || canceledResponse.Status != 0 || len(canceledResponse.Body) != 0 {
		t.Fatal("canceled replay returned a successful receipt", err)
	}
	conflicting := request
	conflicting.PlanSHA = strings.Repeat("0", 64)
	if conflicting.PlanSHA == request.PlanSHA { conflicting.PlanSHA = strings.Repeat("1", 64) }
	if _, err := service.Apply(ctx, who, fixture.Tenant, fixture.Intent, key, conflicting); !errors.Is(err, ErrConflict) {
		t.Fatal("changed request reused saved Apply key", err)
	}
	afterReplay := snapshot()
	for table, before := range beforeReplay {
		if !bytes.Equal(before, afterReplay[table]) { t.Fatal("replay/conflict changed persisted state", table) }
	}
	t.Log("V27_RETOUCH_PHYSICAL_APPLY_OK")
}
