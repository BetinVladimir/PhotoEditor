package workflow

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net"
	"net/http"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/collection"
	"photoeditor/backend/internal/colorprofile"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/upload"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2sessions"
	"sync/atomic"
	"time"
)

// RunScheduler dispatches sealed ready graphs, recovers attempts and advances
// persisted ingest/review work in independent bounded loops. Worker messages do
// not directly manufacture downstream tasks.
func RunScheduler(ctx context.Context, dsn, listen string, storageConfig objectstore.Config, iccDirectory string) error {
	workSHA, err := colorprofile.LoadWorkSHA(iccDirectory)
	if err != nil {
		return err
	}
	profileSHAs, err := colorprofile.LoadSHAs(iccDirectory)
	if err != nil {
		return err
	}
	storage, err := objectstore.New(storageConfig)
	if err != nil {
		return err
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("scheduler database configuration invalid")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "3000"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return errors.New("scheduler database unavailable")
	}
	defer pool.Close()
	startup, finishStartup := context.WithTimeout(ctx, 10*time.Second)
	var installed bool
	err = pool.QueryRow(startup, `SELECT to_regprocedure('photo.adopt_ingest_image(uuid,uuid,bigint,uuid,text,jsonb,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.reject_ingest_upload(uuid,uuid,bigint,uuid,text,text,text)') IS NOT NULL
 AND to_regprocedure('photo.finish_ingest_run(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.cancel_idle_task()') IS NOT NULL
 AND to_regprocedure('photo.cancel_ingest_upload()') IS NOT NULL
 AND to_regprocedure('photo.finalize_run_cancellation()') IS NOT NULL
 AND to_regprocedure('photo.claim_upload_completion()') IS NOT NULL
 AND to_regprocedure('photo.renew_upload_completion(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.release_upload_completion(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.finish_upload_completion(uuid,uuid,text,text,text,jsonb,text,jsonb,text)') IS NOT NULL
 AND to_regprocedure('photo.fail_upload_completion(uuid,uuid,text,text,text,jsonb,text,jsonb,text)') IS NOT NULL
 AND to_regprocedure('photo.ingest_decode_outcomes(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.reserve_ingest_original(uuid,uuid,bigint,uuid,text,text)') IS NOT NULL
 AND to_regprocedure('photo.complete_ingest_original(uuid,uuid,bigint,uuid,uuid,text)') IS NOT NULL
 AND to_regprocedure('photo.ingest_original_source(uuid,uuid,bigint,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.claim_ingest_build()') IS NOT NULL
 AND to_regprocedure('photo.renew_ingest_build(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.release_ingest_build(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.ingest_run_context(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.ingest_published_params(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.ingest_inspection_outcomes(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.seal_ingest_decode(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.seal_ingest_inspection(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.begin_ingest_decode(uuid,uuid,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.materialize_ingest_task(uuid,uuid,jsonb,uuid)') IS NOT NULL
 AND to_regprocedure('photo.register_ingest_node(uuid,uuid,uuid,bigint,text,uuid,text,text,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.skip_failed_ingest_inspection()') IS NOT NULL
 AND to_regprocedure('photo.pending_ingest_activations(integer)') IS NOT NULL
 AND to_regprocedure('photo.activate_bound_task(uuid,text,uuid)') IS NOT NULL
 AND to_regprocedure('photo.claim_defect_expansion()') IS NOT NULL
 AND to_regprocedure('photo.renew_defect_expansion(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.release_defect_expansion(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.materialize_defect_branch(uuid,uuid,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.defect_expansion_context(uuid,uuid,text)') IS NOT NULL
 AND to_regprocedure('photo.expansion_artifact(uuid,uuid,uuid,text)') IS NOT NULL
 AND to_regprocedure('photo.reserve_backend_document(uuid,uuid,uuid,text,text,bigint,text)') IS NOT NULL
 AND to_regprocedure('photo.complete_backend_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)') IS NOT NULL
 AND to_regprocedure('photo.claim_download_build()') IS NOT NULL
 AND to_regprocedure('photo.renew_download_build(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.release_download_build(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.download_export_context(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.download_export_inputs(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.register_download_export_plan(uuid,uuid,uuid,bigint,text,jsonb,text,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.materialize_download_export_task(uuid,uuid,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.seal_download_exports(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.download_build_phase(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.download_encode_outcomes(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.begin_download_packaging(uuid,uuid,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.download_archive_context(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.register_download_archive_plan(uuid,uuid,uuid,integer,text,jsonb,jsonb,text,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.materialize_download_archive_task(uuid,uuid,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.seal_download_archives(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.finalize_download_archives(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.claim_style_candidate()') IS NOT NULL
 AND to_regprocedure('photo.finish_style_candidate(uuid,uuid,jsonb,text,jsonb,boolean,text)') IS NOT NULL
 AND to_regprocedure('photo.claim_style_import_build()') IS NOT NULL
 AND to_regprocedure('photo.reserve_style_import_document(uuid,uuid,uuid,text,bigint,text)') IS NOT NULL
 AND to_regprocedure('photo.complete_style_import_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text)') IS NOT NULL
 AND to_regprocedure('photo.materialize_style_import_task(uuid,uuid,uuid,uuid,text,text,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.claim_asset_embedding()') IS NOT NULL
 AND to_regprocedure('photo.finish_asset_embedding(uuid,uuid,text,double precision[],text)') IS NOT NULL
 AND to_regprocedure('photo.finalize_export_batch()') IS NOT NULL
 AND to_regprocedure('photo.finalize_v26_render_batch()') IS NOT NULL
 AND to_regprocedure('photo.sync_v26_render_batch_state()') IS NOT NULL
 AND to_regprocedure('photo.next_v26_render_recovery(uuid)') IS NOT NULL
 AND to_regprocedure('photo.record_v26_render_recovery(uuid,uuid,boolean)') IS NOT NULL
 AND to_regprocedure('photo.claim_interactive_task()') IS NOT NULL
 AND to_regprocedure('photo.plan_interactive_params(uuid,uuid,jsonb,text)') IS NOT NULL
 AND to_regprocedure('photo.reserve_interactive_document(uuid,uuid,uuid,text,bigint,text)') IS NOT NULL
 AND to_regprocedure('photo.complete_interactive_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text)') IS NOT NULL
 AND to_regprocedure('photo.materialize_interactive_task(uuid,uuid,uuid,uuid,uuid,text,jsonb,text,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.finalize_interactive_run()') IS NOT NULL
 AND to_regprocedure('photo.claim_generation_preview()') IS NOT NULL
 AND to_regprocedure('photo.register_generation_preview(uuid,uuid,uuid,jsonb,text,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.claim_style_analysis_build()') IS NOT NULL
 AND to_regprocedure('photo.renew_style_analysis_build(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.advance_style_analysis_build(uuid,uuid,text,text)') IS NOT NULL
 AND to_regprocedure('photo.fail_style_analysis_build(uuid,uuid,text)') IS NOT NULL
 AND to_regprocedure('photo.register_style_analysis_node(uuid,uuid,uuid,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.reserve_style_analysis_document(uuid,uuid,uuid,text,bigint,text)') IS NOT NULL
 AND to_regprocedure('photo.complete_style_analysis_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text)') IS NOT NULL
 AND to_regprocedure('photo.materialize_style_analysis_task(uuid,uuid,uuid,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.style_analysis_pair_inputs(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.style_analysis_holdout_inputs(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.style_analysis_holdout_outputs(uuid,uuid,uuid,text,text)') IS NOT NULL
 AND to_regprocedure('photo.style_analysis_single_output(uuid,uuid,uuid,text,text,text)') IS NOT NULL
 AND to_regprocedure('photo.style_analysis_stage_outputs(uuid,uuid,uuid,text,text,text)') IS NOT NULL
 AND to_regprocedure('photo.claim_apply_build_versioned()') IS NOT NULL
 AND to_regprocedure('photo.renew_apply_build(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.fail_apply_build(uuid,uuid,text)') IS NOT NULL
 AND to_regprocedure('photo.register_apply_v4_plan(uuid,uuid,uuid,text,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.register_apply_v4_node(uuid,uuid,uuid,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.apply_v4_inputs(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.apply_v4_stage_outputs(uuid,uuid,uuid,text,text)') IS NOT NULL
 AND to_regprocedure('photo.advance_apply_v4_build(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.open_apply_v4_defect_reviews(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.create_dust_review(uuid,uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.open_apply_v4_dust_reviews(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.process_apply_v4_dust_reviews(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.apply_v4_dust_decisions(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.process_apply_v4_defect_reviews(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.apply_v4_defect_outputs(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.adopt_apply_results(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.claim_media_retry_job()') IS NOT NULL
 AND to_regprocedure('photo.begin_media_retry_plan(uuid,uuid,uuid,text,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.register_media_retry_node(uuid,uuid,uuid,jsonb,uuid)') IS NOT NULL
 AND to_regprocedure('photo.reserve_media_retry_document(uuid,uuid,uuid,text,bigint,text)') IS NOT NULL
 AND to_regprocedure('photo.complete_media_retry_document(uuid,uuid,uuid,uuid,uuid,text,bigint,text,text)') IS NOT NULL
 AND to_regprocedure('photo.materialize_media_retry_task(uuid,uuid,uuid,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.seal_media_retry_plan(uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.finalize_media_retry_job()') IS NOT NULL
 AND to_regprocedure('photo.enqueue_bulk_chunk()') IS NOT NULL`).Scan(&installed)
	if err == nil && installed {
		err = storage.Ready(startup)
	}
	finishStartup()
	if err != nil || !installed {
		return errors.New("scheduler expansion dependencies unavailable")
	}
	var sessionPlanInstalled bool
	err = pool.QueryRow(ctx, `SELECT
  to_regprocedure('photo.claim_v22_session_plan()') IS NOT NULL
  AND to_regprocedure('photo.renew_v22_session_plan(uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.release_v22_session_plan(uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.v22_session_plan_frozen(uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.v22_session_plan_inputs(uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.reserve_v22_session_plan_document(uuid,uuid,uuid,text,text,bigint,text)') IS NOT NULL
  AND to_regprocedure('photo.complete_v22_session_plan_document(uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)') IS NOT NULL
  AND to_regprocedure('photo.stage_v22_session_plan(uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb,jsonb)') IS NOT NULL
  AND to_regprocedure('photo.close_v22_session_plan(uuid,uuid,uuid,uuid,uuid)') IS NOT NULL`).Scan(&sessionPlanInstalled)
	if err != nil {
		return errors.New("scheduler V22 capability check unavailable")
	}
	var sceneMaterializationInstalled bool
	err = pool.QueryRow(ctx, `SELECT
  to_regprocedure('photo.v22_pending_scene_plan_materializations()') IS NOT NULL
  AND to_regprocedure('photo.v22_scene_plan_materialization_source(uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.v22_scene_input_snapshot_source(uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.v22_scene_snapshot_members(uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.v22_scene_correction_sources(uuid,uuid)') IS NOT NULL
  AND to_regprocedure('photo.materialize_v22_scene_plan(uuid,uuid,uuid,text,jsonb,text)') IS NOT NULL`).Scan(&sceneMaterializationInstalled)
	if err != nil {
		return errors.New("scheduler scene materialization capability check unavailable")
	}
	captureConfig, captureEnabled, err := CaptureConfigFromEnvironment(os.Getenv)
	if err != nil {
		return err
	}
	var captureProcessor v2input.CaptureAssessmentOrchestrator
	if captureEnabled {
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		installed, e := captureSQLReady(checkCtx, pool, captureConfig.Backend)
		cancel()
		if e != nil || !installed {
			return errors.New("scheduler V24 dependencies or qualified operations unavailable")
		}
		captureProcessor, err = NewCaptureProcessor(pool, storage, storageConfig.Bucket, captureConfig)
		if err != nil {
			return err
		}
	}
	captureState := new(ExpansionLoopState)
	var ready atomic.Bool
	sessionPlanState := new(ExpansionLoopState)
	sceneMaterializationState := new(ExpansionLoopState)
	expansionState := new(ExpansionLoopState)
	ingestState := new(ExpansionLoopState)
	uploadState := new(ExpansionLoopState)
	downloadState := new(ExpansionLoopState)
	bulkState := new(ExpansionLoopState)
	styleState := new(ExpansionLoopState)
	styleAnalysisState := new(ExpansionLoopState)
	styleImportState := new(ExpansionLoopState)
	embeddingState := new(ExpansionLoopState)
	exportBatchState := new(ExpansionLoopState)
	renderBatchState := new(ExpansionLoopState)
	renderRecoveryState := new(ExpansionLoopState)
	interactiveState := new(ExpansionLoopState)
	interactiveFinalizeState := new(ExpansionLoopState)
	applyState := new(ExpansionLoopState)
	mediaRetryState := new(ExpansionLoopState)
	mediaFinalizeState := new(ExpansionLoopState)
	plans := IngestPlanRepository{DB: pool}
	publisher := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: pool, Bucket: storageConfig.Bucket}, Objects: storage}
	originals := OriginalRepository{DB: pool, Bucket: storageConfig.Bucket}
	ingestRunner := IngestRunner{
		Catalog:    IngestCatalogCoordinator{Source: IngestCatalogLoader{DB: pool, Objects: storage, Bucket: storageConfig.Bucket, WorkICCSHA: workSHA}, Originals: OriginalCoordinator{Reservations: originals, Sources: originals, Objects: storage}, Catalog: IngestCatalogRepository{DB: pool, WorkICCSHA: workSHA}},
		Runs:       IngestRunReader{DB: pool},
		Inspection: IngestInspectionCoordinator{Documents: IngestDocumentBuilder{Plans: plans, Publisher: publisher}, Tasks: plans},
		Decode:     IngestDecodeAdvancer{Documents: IngestPublishedReader{DB: pool}, Outcomes: IngestOutcomeReader{DB: pool, Objects: storage, Bucket: storageConfig.Bucket}, Builder: IngestDecodeCoordinator{Tasks: plans}},
	}
	ingestProcessor := IngestProcessor{Leases: SQLIngestLeases{DB: pool}, Runner: ingestRunner}
	downloadPlans := DownloadExportPlanRepository{DB: pool}
	downloadExports := DownloadExportCoordinator{
		Source:  DownloadExportLoader{DB: pool, Objects: storage, Bucket: storageConfig.Bucket, WorkICCSHA256: workSHA, TargetICCs: profileSHAs},
		Builder: DownloadExportBuilder{Plans: downloadPlans, Publisher: publisher}, Tasks: downloadPlans,
	}
	downloadRunner := DownloadBuildRunnerV2{Phases: DownloadBuildPhaseSource{DB: pool}, Exports: downloadExports,
		Packaging: DownloadPackagingCoordinator{Outcomes: DownloadEncodeOutcomeSource{DB: pool}, Plans: DownloadPackagingRepository{DB: pool}},
		Archives: DownloadArchiveCoordinator{Source: DownloadArchiveContextSource{DB: pool},
			Builder: DownloadArchiveBuilder{Plans: DownloadArchiveRepository{DB: pool}, Publisher: publisher}, Tasks: DownloadArchiveTaskRepository{DB: pool}},
		Finalize: DownloadArchiveFinalizer{DB: pool}}
	downloadProcessor := DownloadBuildProcessor{Leases: SQLDownloadBuildLeases{DB: pool}, Runner: downloadRunner}
	bulkProcessor := collection.BulkProcessor{DB: pool}
	styleProcessor := StyleCandidateMaterializer{Repository: SQLStyleCandidateRepository{DB: pool, Bucket: storageConfig.Bucket}, Objects: storage}
	styleAnalysisProcessor := StyleAnalysisCoordinator{Store: StyleAnalysisStore{DB: pool}, Publisher: artifactjson.Publisher{Repository: StyleAnalysisDocuments{DB: pool, Bucket: storageConfig.Bucket}, Objects: storage}, Objects: storage, Bucket: storageConfig.Bucket}
	styleImportProcessor := StyleImportCoordinator{Store: StyleImportStore{DB: pool}, Publisher: artifactjson.Publisher{Repository: StyleImportDocuments{DB: pool, Bucket: storageConfig.Bucket}, Objects: storage}}
	embeddingProcessor := AssetEmbeddingMaterializer{Repository: SQLAssetEmbeddingRepository{DB: pool, Bucket: storageConfig.Bucket}, Objects: storage}
	exportBatchProcessor := ExportBatchFinalizer{DB: pool}
	renderBatchProcessor := RenderBatchFinalizer{DB: pool}
	sourceDSN := os.Getenv("RENDER_SOURCE_DATABASE_URL")
	if sourceDSN == "" {
		sourceDSN = dsn
	}
	renderStartup, stopRenderStartup := context.WithTimeout(ctx, 10*time.Second)
	renderRecoveryProcessor, closeRenderSources, err := NewRenderRecoveryProcessor(renderStartup, pool, sourceDSN, storage, storageConfig.Bucket)
	stopRenderStartup()
	if err != nil {
		return err
	}
	defer closeRenderSources()

	interactiveProcessor := InteractiveCoordinator{Store: InteractiveStore{DB: pool}, Publisher: artifactjson.Publisher{Repository: InteractiveDocuments{DB: pool, Bucket: storageConfig.Bucket}, Objects: storage}, Objects: storage, Bucket: storageConfig.Bucket}
	interactiveFinalizer := InteractiveFinalizer{DB: pool}
	applyProcessor := ApplyCoordinator{Store: ApplyStore{DB: pool}, Publisher: artifactjson.Publisher{Repository: ApplyDocuments{DB: pool, Bucket: storageConfig.Bucket}, Objects: storage}, Objects: storage, Bucket: storageConfig.Bucket}
	mediaRetryStore := MediaRetryStore{DB: pool, Objects: storage, Bucket: storageConfig.Bucket}
	mediaRetryProcessor := MediaRetryCoordinator{Store: mediaRetryStore, Publisher: artifactjson.Publisher{Repository: MediaRetryDocuments{DB: pool, Bucket: storageConfig.Bucket}, Objects: storage}, SRGBSHA: profileSHAs["srgb_v1"]}
	mediaFinalizer := MediaRetryFinalizer{DB: pool}
	uploadProcessor := upload.CompletionProcessor{Leases: upload.SQLCompletionLeases{DB: pool, Bucket: storageConfig.Bucket}, Objects: storage}
	sessionPlanProcessor := v2input.SessionPlanOrchestrator{
		Store: v2input.SQLSessionPlanStore{DB: pool}, Source: v2input.SQLSessionPlanSource{DB: pool},
		Documents: publisher, Committer: v2input.Admission{DB: pool},
		TransportForLease: func(lease v2input.SessionPlanLease) v2input.Publisher {
			return v2input.ScopedPublisher{Documents: publisher, Tenant: lease.TenantID, Run: lease.RunID}
		},
	}
	sceneMaterializer := v2sessions.PlanMaterializer{DB: pool, Objects: storage,
		Bucket: storageConfig.Bucket}
	builder := DefectBranchBuilder{Artifacts: ExpansionArtifacts{DB: pool, Objects: storage, Bucket: storageConfig.Bucket}, Publisher: artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: pool, Bucket: storageConfig.Bucket}, Objects: storage}}
	processor := ExpansionProcessor{Leases: SQLExpansionLeases{DB: pool}, Build: builder.Build}
	var dispatched, recovered, ingestActivated, ingestSkipped atomic.Uint64
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ping, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if !ready.Load() || (captureEnabled && !captureState.Ready.Load()) || (sessionPlanInstalled && !sessionPlanState.Ready.Load()) || (sceneMaterializationInstalled && !sceneMaterializationState.Ready.Load()) || !expansionState.Ready.Load() || !ingestState.Ready.Load() || !uploadState.Ready.Load() || !downloadState.Ready.Load() || !bulkState.Ready.Load() || !styleState.Ready.Load() || !styleAnalysisState.Ready.Load() || !styleImportState.Ready.Load() || !embeddingState.Ready.Load() || !exportBatchState.Ready.Load() || !renderBatchState.Ready.Load() || !renderRecoveryState.Ready.Load() || !interactiveState.Ready.Load() || !interactiveFinalizeState.Ready.Load() || !applyState.Ready.Load() || !mediaRetryState.Ready.Load() || !mediaFinalizeState.Ready.Load() || pool.Ping(ping) != nil || storage.Ready(ping) != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		w.Write([]byte(`{"status":"ready"}`))
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# TYPE photo_scheduler_ingest_build_transitions_total counter\nphoto_scheduler_ingest_build_transitions_total %d\n# TYPE photo_scheduler_ingest_build_failures_total counter\nphoto_scheduler_ingest_build_failures_total %d\n", ingestState.Materialized.Load(), ingestState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_download_build_transitions_total counter\nphoto_scheduler_download_build_transitions_total %d\n# TYPE photo_scheduler_download_build_failures_total counter\nphoto_scheduler_download_build_failures_total %d\n", downloadState.Materialized.Load(), downloadState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_bulk_chunks_total counter\nphoto_scheduler_bulk_chunks_total %d\n# TYPE photo_scheduler_bulk_failures_total counter\nphoto_scheduler_bulk_failures_total %d\n", bulkState.Materialized.Load(), bulkState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_style_candidates_total counter\nphoto_scheduler_style_candidates_total %d\n# TYPE photo_scheduler_style_candidate_failures_total counter\nphoto_scheduler_style_candidate_failures_total %d\n", styleState.Materialized.Load(), styleState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_style_analysis_transitions_total counter\nphoto_scheduler_style_analysis_transitions_total %d\n# TYPE photo_scheduler_style_analysis_failures_total counter\nphoto_scheduler_style_analysis_failures_total %d\n", styleAnalysisState.Materialized.Load(), styleAnalysisState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_apply_transitions_total counter\nphoto_scheduler_apply_transitions_total %d\n# TYPE photo_scheduler_apply_failures_total counter\nphoto_scheduler_apply_failures_total %d\n", applyState.Materialized.Load(), applyState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_media_retry_plans_total counter\nphoto_scheduler_media_retry_plans_total %d\n# TYPE photo_scheduler_media_retry_failures_total counter\nphoto_scheduler_media_retry_failures_total %d\n", mediaRetryState.Materialized.Load(), mediaRetryState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_media_retry_finalized_total counter\nphoto_scheduler_media_retry_finalized_total %d\n# TYPE photo_scheduler_media_retry_finalize_failures_total counter\nphoto_scheduler_media_retry_finalize_failures_total %d\n", mediaFinalizeState.Materialized.Load(), mediaFinalizeState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_upload_completions_total counter\nphoto_scheduler_upload_completions_total %d\n# TYPE photo_scheduler_upload_completion_failures_total counter\nphoto_scheduler_upload_completion_failures_total %d\n", uploadState.Materialized.Load(), uploadState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_ingest_upstream_skips_total counter\nphoto_scheduler_ingest_upstream_skips_total %d\n", ingestSkipped.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_ingest_activation_confirmations_total counter\nphoto_scheduler_ingest_activation_confirmations_total %d\n", ingestActivated.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_expansions_materialized_total counter\nphoto_scheduler_expansions_materialized_total %d\n# TYPE photo_scheduler_expansion_failures_total counter\nphoto_scheduler_expansion_failures_total %d\n", expansionState.Materialized.Load(), expansionState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_capture_transitions_total counter\nphoto_scheduler_capture_transitions_total %d\n# TYPE photo_scheduler_capture_failures_total counter\nphoto_scheduler_capture_failures_total %d\n", captureState.Materialized.Load(), captureState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_v22_session_plans_total counter\nphoto_scheduler_v22_session_plans_total %d\n# TYPE photo_scheduler_v22_session_plan_failures_total counter\nphoto_scheduler_v22_session_plan_failures_total %d\n", sessionPlanState.Materialized.Load(), sessionPlanState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_v22_scene_materializations_total counter\nphoto_scheduler_v22_scene_materializations_total %d\n# TYPE photo_scheduler_v22_scene_materialization_failures_total counter\nphoto_scheduler_v22_scene_materialization_failures_total %d\n", sceneMaterializationState.Materialized.Load(), sceneMaterializationState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_dispatched_total counter\nphoto_scheduler_dispatched_total %d\n# TYPE photo_scheduler_recovered_transitions_total counter\nphoto_scheduler_recovered_transitions_total %d\n", dispatched.Load(), recovered.Load())
	})
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	defer server.Close()
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	expansionCtx, stopExpansions := context.WithCancel(ctx)
	expansionDone := make(chan error, 1)
	go func() { expansionDone <- RunExpansionLoop(expansionCtx, processor, expansionState, 5*time.Second) }()
	defer func() { stopExpansions(); <-expansionDone }()
	if captureEnabled {
		captureCtx, stopCapture := context.WithCancel(ctx)
		captureDone := make(chan error, 1)
		go func() { captureDone <- RunExpansionLoop(captureCtx, captureProcessor, captureState, time.Second) }()
		defer func() { stopCapture(); <-captureDone }()
	}
	if sessionPlanInstalled {
		sessionCtx, stopSessions := context.WithCancel(ctx)
		sessionDone := make(chan error, 1)
		go func() {
			sessionDone <- RunExpansionLoop(sessionCtx, sessionPlanProcessor, sessionPlanState, 5*time.Second)
		}()
		defer func() { stopSessions(); <-sessionDone }()
	}
	if sceneMaterializationInstalled {
		sceneCtx, stopScenes := context.WithCancel(ctx)
		sceneDone := make(chan error, 1)
		go func() {
			sceneDone <- RunExpansionLoop(sceneCtx, sceneMaterializer,
				sceneMaterializationState, 5*time.Second)
		}()
		defer func() { stopScenes(); <-sceneDone }()
	}
	ingestCtx, stopIngest := context.WithCancel(ctx)
	ingestDone := make(chan error, 1)
	go func() { ingestDone <- RunExpansionLoop(ingestCtx, ingestProcessor, ingestState, 5*time.Second) }()
	defer func() { stopIngest(); <-ingestDone }()
	uploadCtx, stopUploads := context.WithCancel(ctx)
	uploadDone := make(chan error, 1)
	go func() { uploadDone <- RunExpansionLoop(uploadCtx, uploadProcessor, uploadState, 5*time.Second) }()
	defer func() { stopUploads(); <-uploadDone }()
	downloadCtx, stopDownloads := context.WithCancel(ctx)
	downloadDone := make(chan error, 1)
	go func() { downloadDone <- RunExpansionLoop(downloadCtx, downloadProcessor, downloadState, 5*time.Second) }()
	defer func() { stopDownloads(); <-downloadDone }()
	bulkCtx, stopBulk := context.WithCancel(ctx)
	bulkDone := make(chan error, 1)
	go func() { bulkDone <- RunExpansionLoop(bulkCtx, bulkProcessor, bulkState, 5*time.Second) }()
	defer func() { stopBulk(); <-bulkDone }()
	styleCtx, stopStyles := context.WithCancel(ctx)
	styleDone := make(chan error, 1)
	go func() { styleDone <- RunExpansionLoop(styleCtx, styleProcessor, styleState, 5*time.Second) }()
	defer func() { stopStyles(); <-styleDone }()
	styleAnalysisCtx, stopStyleAnalysis := context.WithCancel(ctx)
	styleAnalysisDone := make(chan error, 1)
	go func() {
		styleAnalysisDone <- RunExpansionLoop(styleAnalysisCtx, styleAnalysisProcessor, styleAnalysisState, 5*time.Second)
	}()
	defer func() { stopStyleAnalysis(); <-styleAnalysisDone }()
	styleImportCtx, stopStyleImport := context.WithCancel(ctx)
	styleImportDone := make(chan error, 1)
	go func() {
		styleImportDone <- RunExpansionLoop(styleImportCtx, styleImportProcessor, styleImportState, 5*time.Second)
	}()
	defer func() { stopStyleImport(); <-styleImportDone }()
	embeddingCtx, stopEmbedding := context.WithCancel(ctx)
	embeddingDone := make(chan error, 1)
	go func() {
		embeddingDone <- RunExpansionLoop(embeddingCtx, embeddingProcessor, embeddingState, 5*time.Second)
	}()
	defer func() { stopEmbedding(); <-embeddingDone }()
	exportBatchCtx, stopExportBatch := context.WithCancel(ctx)
	exportBatchDone := make(chan error, 1)
	go func() {
		exportBatchDone <- RunExpansionLoop(exportBatchCtx, exportBatchProcessor, exportBatchState, 5*time.Second)
	}()
	defer func() { stopExportBatch(); <-exportBatchDone }()
	renderBatchCtx, stopRenderBatch := context.WithCancel(ctx)
	renderBatchDone := make(chan error, 1)
	go func() {
		renderBatchDone <- RunExpansionLoop(renderBatchCtx, renderBatchProcessor, renderBatchState, 5*time.Second)
	}()
	defer func() { stopRenderBatch(); <-renderBatchDone }()
	renderRecoveryCtx, stopRenderRecovery := context.WithCancel(ctx)
	renderRecoveryDone := make(chan error, 1)
	go func() {
		renderRecoveryDone <- RunExpansionLoop(renderRecoveryCtx, renderRecoveryProcessor, renderRecoveryState, 5*time.Second)
	}()
	defer func() { stopRenderRecovery(); <-renderRecoveryDone }()

	interactiveCtx, stopInteractive := context.WithCancel(ctx)
	interactiveDone := make(chan error, 1)
	go func() {
		interactiveDone <- RunExpansionLoop(interactiveCtx, interactiveProcessor, interactiveState, 5*time.Second)
	}()
	defer func() { stopInteractive(); <-interactiveDone }()
	interactiveFinalizeCtx, stopInteractiveFinalize := context.WithCancel(ctx)
	interactiveFinalizeDone := make(chan error, 1)
	go func() {
		interactiveFinalizeDone <- RunExpansionLoop(interactiveFinalizeCtx, interactiveFinalizer, interactiveFinalizeState, 5*time.Second)
	}()
	defer func() { stopInteractiveFinalize(); <-interactiveFinalizeDone }()
	applyCtx, stopApply := context.WithCancel(ctx)
	applyDone := make(chan error, 1)
	go func() { applyDone <- RunExpansionLoop(applyCtx, applyProcessor, applyState, 5*time.Second) }()
	defer func() { stopApply(); <-applyDone }()
	mediaRetryCtx, stopMediaRetry := context.WithCancel(ctx)
	mediaRetryDone := make(chan error, 1)
	go func() {
		mediaRetryDone <- RunExpansionLoop(mediaRetryCtx, mediaRetryProcessor, mediaRetryState, 5*time.Second)
	}()
	defer func() { stopMediaRetry(); <-mediaRetryDone }()
	mediaFinalizeCtx, stopMediaFinalize := context.WithCancel(ctx)
	mediaFinalizeDone := make(chan error, 1)
	go func() {
		mediaFinalizeDone <- RunExpansionLoop(mediaFinalizeCtx, mediaFinalizer, mediaFinalizeState, 5*time.Second)
	}()
	defer func() { stopMediaFinalize(); <-mediaFinalizeDone }()
	recovery, dispatcher := Recovery{DB: pool}, Dispatcher{DB: pool}
	ingest := IngestActivator{Source: IngestActivationSource{DB: pool}, Tasks: AdvancementRepository{DB: pool}}
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			ready.Store(false)
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return server.Shutdown(shutdown)
		case e := <-serveErrors:
			if errors.Is(e, http.ErrServerClosed) {
				return nil
			}
			return e
		case <-tick.C:
			cycle, cancel := context.WithTimeout(ctx, 10*time.Second)
			n, e := recovery.Recover(cycle, 20)
			if n > 0 {
				recovered.Add(uint64(n))
			}
			if e == nil {
				n, e = (IngestPlanRepository{DB: pool}).SkipFailedInspection(cycle, 20)
				if n > 0 {
					ingestSkipped.Add(uint64(n))
				}
			}
			if e == nil {
				n, e = ingest.Advance(cycle, 20)
				if n > 0 {
					ingestActivated.Add(uint64(n))
				}
			}
			if e == nil {
				n, e = dispatcher.Dispatch(cycle, 20)
				if n > 0 {
					dispatched.Add(uint64(n))
				}
			}
			cancel()
			if e != nil {
				ready.Store(false)
				if ctx.Err() == nil {
					slog.Warn("scheduler cycle deferred")
				}
				continue
			}
			ready.Store(true)
		}
	}
}
