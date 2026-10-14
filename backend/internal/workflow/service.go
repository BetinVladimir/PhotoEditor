package workflow

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
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
	"photoeditor/backend/internal/v2learning"
	"photoeditor/backend/internal/v2optical"
	"photoeditor/backend/internal/v2sessions"
	"sync/atomic"
	"time"
)

// RunScheduler dispatches sealed ready graphs, recovers attempts and advances
// persisted ingest/review work in independent bounded loops. Worker messages do
// not directly manufacture downstream tasks.
func RunScheduler(ctx context.Context, dsn, listen string, storageConfig objectstore.Config, iccDirectory string) error {
	return RunSchedulerWithNativeRecovery(ctx, dsn, listen, storageConfig, iccDirectory, nil)
}

// The native factory supplies verified deployment policy and scoped dependencies.
// No qualification reports or protection refs are synthesized during startup.
func RunSchedulerWithNativeRecovery(ctx context.Context, dsn, listen string, storageConfig objectstore.Config, iccDirectory string, nativeFactory SchedulerNativeRecoveryFactory, importFactories ...SchedulerInterchangeImportFactory) error {
	return RunSchedulerWithArchiveCompletion(ctx, dsn, listen, storageConfig, iccDirectory, nativeFactory, nil, importFactories...)
}

type SchedulerDeliverySigningFactory func(context.Context, string, *pgxpool.Pool,
	*objectstore.Store, string, string) (DeliverySigningRuntime, func(), error)

func RunSchedulerWithArchiveCompletion(ctx context.Context, dsn, listen string, storageConfig objectstore.Config, iccDirectory string, nativeFactory SchedulerNativeRecoveryFactory, archiveFactory SchedulerArchiveCompletionFactory, importFactories ...SchedulerInterchangeImportFactory) error {
	return runSchedulerWithDeliverySigning(ctx, dsn, listen, storageConfig,
		iccDirectory, nativeFactory, archiveFactory, nil, nil, importFactories...)
}

func RunSchedulerWithDeliverySigning(ctx context.Context, dsn, listen string,
	storageConfig objectstore.Config, iccDirectory string,
	nativeFactory SchedulerNativeRecoveryFactory,
	archiveFactory SchedulerArchiveCompletionFactory,
	signingFactory SchedulerDeliverySigningFactory,
	importFactories ...SchedulerInterchangeImportFactory) error {
	return runSchedulerWithDeliverySigning(ctx, dsn, listen, storageConfig,
		iccDirectory, nativeFactory, archiveFactory, signingFactory, nil, importFactories...)
}

// A configured export factory must provide the complete bundle authority.
// It is installed alongside the other scheduler jobs, never inside an HTTP
// request or with an expired user session.
func RunSchedulerWithInterchangeExport(ctx context.Context, dsn, listen string,
	storageConfig objectstore.Config, iccDirectory string,
	nativeFactory SchedulerNativeRecoveryFactory,
	archiveFactory SchedulerArchiveCompletionFactory,
	signingFactory SchedulerDeliverySigningFactory,
	exportFactory SchedulerInterchangeExportFactory,
	importFactories ...SchedulerInterchangeImportFactory) error {
	return runSchedulerWithDeliverySigning(ctx, dsn, listen, storageConfig,
		iccDirectory, nativeFactory, archiveFactory, signingFactory, exportFactory, importFactories...)
}

func runSchedulerWithDeliverySigning(ctx context.Context, dsn, listen string, storageConfig objectstore.Config, iccDirectory string, nativeFactory SchedulerNativeRecoveryFactory, archiveFactory SchedulerArchiveCompletionFactory, signingFactory SchedulerDeliverySigningFactory, exportFactory SchedulerInterchangeExportFactory, importFactories ...SchedulerInterchangeImportFactory) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	capacityPin := os.Getenv("PHOTO_V32_SCHEDULER_POLICY_SHA256")
	if capacityPin != "" && !capacityPolicyDigest.MatchString(capacityPin) {
		return errors.New("invalid V32 scheduler policy SHA256")
	}
	if len(importFactories) > 1 || (len(importFactories) == 1 && importFactories[0] == nil) {
		return errors.New("invalid interchange import scheduler factory")
	}
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
	var capacityRuntime *CapacityRuntime
	if capacityPin != "" {
		capacityStartup, stopCapacityStartup := context.WithTimeout(ctx, 10*time.Second)
		capacityRuntime, err = NewCapacityRuntime(capacityStartup, pool, capacityPin)
		stopCapacityStartup()
		if err != nil {
			return err
		}
		defer capacityRuntime.Close()
	}
	var localCapacityMonitor *LocalCapacityMonitor
	localScaleAlerts := os.Getenv("PHOTO_V32_LOCAL_SCALE_ALERTS")
	if localScaleAlerts != "" && localScaleAlerts != "true" && localScaleAlerts != "false" {
		return errors.New("invalid PHOTO_V32_LOCAL_SCALE_ALERTS")
	}
	if localScaleAlerts == "true" {
		if capacityRuntime == nil {
			return errors.New("local capacity alerts require V32 scheduler policy")
		}
		localCapacityMonitor, err = NewLocalCapacityMonitor(capacityRuntime, []string{"cpu", "gpu", "signer"})
		if err != nil {
			return err
		}
	}
	startup, finishStartup := context.WithTimeout(ctx, 10*time.Second)
	var installed bool
	err = pool.QueryRow(startup, `SELECT to_regprocedure('photo.adopt_ingest_image(uuid,uuid,bigint,uuid,text,jsonb,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.reject_ingest_upload(uuid,uuid,bigint,uuid,text,text,text)') IS NOT NULL
 AND to_regprocedure('photo.finish_ingest_run(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.cancel_idle_task()') IS NOT NULL
 AND to_regprocedure('photo.read_v29_negative_scheduler_decisions(uuid,integer)') IS NOT NULL
 AND to_regprocedure('photo.release_next_v29_negative_cancelled_quota()') IS NOT NULL
 AND to_regprocedure('photo.close_v29_negative_skipped_dependencies(uuid,uuid,uuid,uuid,uuid,text,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.reconcile_v29_negative_invalidation(uuid,uuid,uuid,uuid,uuid,text,jsonb,integer,integer)') IS NOT NULL
 AND to_regprocedure('photo.cancel_ingest_upload()') IS NOT NULL
 AND to_regprocedure('photo.finalize_run_cancellation()') IS NOT NULL
 AND to_regprocedure('photo.release_next_v29_replacement_detector_quota()') IS NOT NULL
 AND has_function_privilege(current_user,to_regprocedure('photo.release_next_v29_replacement_detector_quota()'),'EXECUTE')
 AND to_regprocedure('photo.settle_next_semantic_child()') IS NOT NULL
 AND to_regprocedure('photo.finalize_next_semantic_apply_child()') IS NOT NULL
 AND to_regprocedure('photo.claim_upload_completion()') IS NOT NULL
 AND to_regprocedure('photo.claim_upload_completion_v2()') IS NOT NULL
 AND has_function_privilege(current_user,to_regprocedure('photo.claim_upload_completion_v2()'),'EXECUTE')
 AND EXISTS(SELECT FROM pg_catalog.pg_attribute WHERE attrelid=to_regclass('photo.upload_sessions') AND attname='raw_development' AND NOT attisdropped)
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
 AND to_regprocedure('photo.v30_next_interchange_collection()') IS NOT NULL
 AND to_regprocedure('photo.prepare_v30_interchange_draft_collection(uuid,uuid,text)') IS NOT NULL
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
 AND to_regprocedure('photo.release_next_v26_render_derivative_quota()') IS NOT NULL
 AND to_regprocedure('photo.finalize_failed_v26_render_derivative()') IS NOT NULL
 AND to_regprocedure('photo.finalize_successful_v26_render_derivative()') IS NOT NULL
 AND to_regprocedure('photo.cancel_stale_v26_render_derivative()') IS NOT NULL
 AND to_regprocedure('photo.next_v29_perspective_quality_producer(uuid)') IS NOT NULL
 AND to_regprocedure('photo.seal_v29_perspective_quality(uuid,uuid,text,text,text,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.v29_perspective_quality_receipt(uuid,uuid,uuid,uuid,text)') IS NOT NULL
 AND to_regprocedure('photo.next_v29_multi_quality_producer(uuid)') IS NOT NULL
 AND to_regprocedure('photo.seal_v29_multi_quality(uuid,uuid,text,text,jsonb,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.v29_multi_quality_receipt(uuid,uuid,uuid,uuid,text,text,integer,bigint)') IS NOT NULL
 AND to_regprocedure('photo.next_v29_multi_preview_producer(uuid,text)') IS NOT NULL
 AND to_regprocedure('photo.seal_v29_multi_preview(uuid,uuid,text,text,jsonb,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.v29_multi_preview_receipt(uuid,uuid,uuid,uuid,text,text,text,integer,bigint)') IS NOT NULL
 AND to_regprocedure('photo.v29_multi_preview_read_originals(uuid,uuid,text,integer,bigint,uuid,text)') IS NOT NULL
 AND to_regprocedure('photo.next_v29_perspective_preview_producer(uuid,text)') IS NOT NULL
 AND to_regprocedure('photo.seal_v29_perspective_preview(uuid,uuid,text,text,text,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.finalize_successful_v29_perspective_preview(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.maintain_v29_perspective_preview(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.next_v26_render_derivative(uuid)') IS NOT NULL
 AND to_regprocedure('photo.enqueue_next_v26_render_derivative(text)') IS NOT NULL
 AND to_regprocedure('photo.enqueue_next_semantic_exposure_preview(text)') IS NOT NULL
 AND to_regprocedure('photo.v26_render_derivative_artifact(uuid,uuid,uuid,text)') IS NOT NULL
 AND to_regprocedure('photo.reserve_v26_render_derivative_document(uuid,uuid,uuid,text,text,bigint,text,text,text,text)') IS NOT NULL
 AND to_regprocedure('photo.materialize_v26_render_derivative_task(uuid,uuid,text,text,text,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.seal_v26_render_derivative(uuid,uuid,text,text)') IS NOT NULL
 AND to_regprocedure('photo.next_v26_render_recovery(uuid)') IS NOT NULL
 AND to_regprocedure('photo.record_v26_render_recovery(uuid,uuid,boolean)') IS NOT NULL
 AND to_regprocedure('photo.claim_interactive_task()') IS NOT NULL
 AND to_regprocedure('photo.plan_interactive_params(uuid,uuid,jsonb,text)') IS NOT NULL
 AND to_regprocedure('photo.fail_interactive_manual_preflight(uuid,uuid,uuid,uuid,text,jsonb,text)') IS NOT NULL
 AND coalesce(has_function_privilege(current_user,to_regprocedure('photo.fail_interactive_manual_preflight(uuid,uuid,uuid,uuid,text,jsonb,text)'),'EXECUTE'),false)
 AND NOT coalesce(has_function_privilege(current_user,to_regprocedure('photo.fail_interactive_manual_preflight(uuid,uuid,uuid,uuid,text)'),'EXECUTE'),false)
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
	negativeBackend, err := buildNegativeReplacementScheduler(ctx, pool, storage, storageConfig.Bucket, workSHA)
	if err != nil {
		return err
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
	var learningEvaluateInstalled bool
	err = pool.QueryRow(ctx, `SELECT
 to_regproc('photo.claim_v23_learning_evaluate') IS NOT NULL AND
 to_regproc('photo.renew_v23_learning_evaluate') IS NOT NULL AND
 to_regproc('photo.release_v23_learning_evaluate') IS NOT NULL AND
 to_regproc('photo.v23_evaluate_manifest_source') IS NOT NULL AND
 to_regproc('photo.v23_evaluate_frozen_root') IS NOT NULL AND
 to_regproc('photo.v23_evaluate_reviewed_defect_authority') IS NOT NULL AND
 to_regproc('photo.v23_evaluate_defect_closure_current') IS NOT NULL AND
 to_regprocedure('photo.v23_evaluate_reviewed_dust_authority(uuid,uuid,uuid,uuid,uuid,uuid,text)') IS NOT NULL AND
 to_regprocedure('photo.v23_evaluate_dust_closure_current(uuid,uuid,uuid,uuid,uuid)') IS NOT NULL AND
 to_regprocedure('photo.v23_evaluate_detail_authority(uuid,uuid,uuid,uuid,uuid,uuid,text)') IS NOT NULL AND
 to_regprocedure('photo.v23_evaluate_detail_nested(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text)') IS NOT NULL AND
 to_regprocedure('photo.v23_evaluate_detail_closure_current(uuid,uuid,uuid,uuid,uuid)') IS NOT NULL AND
 to_regprocedure('photo.v23_evaluate_detail_protection_current(uuid,uuid,uuid,uuid,uuid)') IS NOT NULL AND
 to_regproc('photo.close_v23_learning_evaluate_guarded') IS NOT NULL AND
 to_regproc('photo.v23_evaluate_image_child') IS NOT NULL AND
 to_regproc('photo.v23_evaluate_protected_mask_child') IS NOT NULL AND
 to_regproc('photo.v23_evaluate_effective_mask_child') IS NOT NULL AND
 to_regproc('photo.v23_evaluate_base_style_source') IS NOT NULL AND
 to_regproc('photo.v23_pin_evaluate_identity_consents') IS NOT NULL AND
 to_regproc('photo.stage_v23_learning_evaluate') IS NOT NULL AND
 to_regproc('photo.close_v23_learning_evaluate') IS NOT NULL AND
 coalesce(has_function_privilege(current_user,
  to_regprocedure('photo.v23_evaluate_reviewed_defect_authority(uuid,uuid,uuid,uuid,uuid,uuid,text)'),
  'EXECUTE'),false) AND
 coalesce(has_function_privilege(current_user,
  to_regprocedure('photo.v23_evaluate_detail_authority(uuid,uuid,uuid,uuid,uuid,uuid,text)'),
  'EXECUTE'),false) AND
 coalesce(has_function_privilege(current_user,
  to_regprocedure('photo.v23_evaluate_detail_nested(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,text)'),
  'EXECUTE'),false) AND
 coalesce(has_function_privilege(current_user,
  to_regprocedure('photo.v23_evaluate_detail_closure_current(uuid,uuid,uuid,uuid,uuid)'),
  'EXECUTE'),false)`).Scan(&learningEvaluateInstalled)
	if err != nil {
		return errors.New("scheduler V23 evaluate capability check unavailable")
	}
	var semanticAssemblyInstalled bool
	err = pool.QueryRow(ctx, `SELECT to_regprocedure('photo.claim_v27_semantic_assembly()') IS NOT NULL
 AND to_regprocedure('photo.renew_v27_semantic_assembly(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.release_v27_semantic_assembly(uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.v27_semantic_assembly_source_inventory(uuid,uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.v27_semantic_assembly_lease_live(uuid,uuid,uuid,uuid)') IS NOT NULL
 AND to_regprocedure('photo.stage_v27_semantic_assembly(uuid,uuid,uuid,uuid,uuid,uuid,text,text,jsonb,jsonb)') IS NOT NULL
 AND to_regprocedure('photo.reserve_v27_semantic_assembly_document(uuid,uuid,uuid,uuid,text,text,bigint,text)') IS NOT NULL
 AND to_regprocedure('photo.complete_v27_semantic_assembly_document(uuid,uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)') IS NOT NULL
 AND to_regprocedure('photo.reserve_v27_semantic_transport_document(uuid,uuid,uuid,uuid,text,text,bigint,text)') IS NOT NULL
 AND to_regprocedure('photo.complete_v27_semantic_transport_document(uuid,uuid,uuid,uuid,text,uuid,uuid,text,bigint,text,text)') IS NOT NULL
 AND to_regprocedure('photo.v27_semantic_frozen_assembly_qualification(uuid,uuid,uuid,uuid,text,text,uuid)') IS NOT NULL
 AND to_regprocedure('photo.commit_v27_semantic_assembly(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,text,jsonb)') IS NOT NULL`).Scan(&semanticAssemblyInstalled)
	if err != nil {
		return errors.New("scheduler V27 assembly capability check unavailable")
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
	semanticAssemblyState := new(ExpansionLoopState)
	learningEvaluateState := new(ExpansionLoopState)
	sessionPlanState := new(ExpansionLoopState)
	sceneMaterializationState := new(ExpansionLoopState)
	expansionState := new(ExpansionLoopState)
	ingestState := new(ExpansionLoopState)
	interchangeCollectionState := new(ExpansionLoopState)
	interchangeImportState := new(ExpansionLoopState)
	interchangeExportState := new(ExpansionLoopState)
	archiveCompletionState := new(ExpansionLoopState)
	archiveExpiryState := new(ExpansionLoopState)
	archiveCleanupState := new(ExpansionLoopState)
	archiveRecoveryState := new(ExpansionLoopState)
	archiveRevocationState := new(ExpansionLoopState)
	archiveInvalidationState := new(ExpansionLoopState)
	uploadState := new(ExpansionLoopState)
	downloadState := new(ExpansionLoopState)
	bulkState := new(ExpansionLoopState)
	styleState := new(ExpansionLoopState)
	styleAnalysisState := new(ExpansionLoopState)
	styleImportState := new(ExpansionLoopState)
	embeddingState := new(ExpansionLoopState)
	exportBatchState := new(ExpansionLoopState)
	renderBatchState := new(ExpansionLoopState)
	renderDerivativeCleanupState := new(ExpansionLoopState)
	renderDerivativeState := new(ExpansionLoopState)
	restorationPreviewState := new(ExpansionLoopState)
	perspectivePreviewState := new(ExpansionLoopState)
	multiPreviewState := new(ExpansionLoopState)
	multiQualityState := new(ExpansionLoopState)
	perspectiveQualityState := new(ExpansionLoopState)
	renderRecoveryState := new(ExpansionLoopState)
	nativeRecoveryState := new(ExpansionLoopState)
	perspectiveLinesRecoveryState := new(ExpansionLoopState)
	perspectiveLinesLifecycleState := new(ExpansionLoopState)
	interactiveState := new(ExpansionLoopState)
	interactiveFinalizeState := new(ExpansionLoopState)
	applyState := new(ExpansionLoopState)
	mediaRetryState := new(ExpansionLoopState)
	mediaFinalizeState := new(ExpansionLoopState)
	plans := IngestPlanRepository{DB: pool}
	publisher := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: pool, Bucket: storageConfig.Bucket}, Objects: storage}
	originals := OriginalRepository{DB: pool, Bucket: storageConfig.Bucket}
	ingestRunner := IngestRunner{
		Catalog:    IngestCatalogRouter{DB: pool, Source: IngestCatalogLoader{DB: pool, Objects: storage, Bucket: storageConfig.Bucket, WorkICCSHA: workSHA}, WorkICCSHA: workSHA, Legacy: IngestCatalogCoordinator{Source: IngestCatalogLoader{DB: pool, Objects: storage, Bucket: storageConfig.Bucket, WorkICCSHA: workSHA}, Originals: OriginalCoordinator{Reservations: originals, Sources: originals, Objects: storage}, Catalog: IngestCatalogRepository{DB: pool, WorkICCSHA: workSHA}}},
		Runs:       IngestRunReader{DB: pool},
		Inspection: IngestInspectionCoordinator{Documents: IngestDocumentBuilder{Plans: plans, Publisher: publisher}, Tasks: plans},
		Decode:     IngestDecodeAdvancer{Documents: IngestPublishedReader{DB: pool}, Outcomes: IngestOutcomeReader{DB: pool, Objects: storage, Bucket: storageConfig.Bucket}, Builder: IngestDecodeCoordinator{Tasks: plans}},
	}
	ingestProcessor := IngestProcessor{Leases: SQLIngestLeases{DB: pool}, Runner: ingestRunner}
	var archiveCompletionProcessor ExpansionJob
	var archiveExpiryProcessor ExpansionJob
	var archiveCleanupProcessor ExpansionJob
	var archiveRecoveryProcessor ExpansionJob
	var archiveRevocationProcessor ExpansionJob
	var archiveInvalidationProcessor ExpansionJob
	if archiveFactory != nil {
		archiveCompletionProcessor, err = archiveFactory.BuildArchiveCompletion(ctx, pool, storage, storageConfig.Bucket)
		if err != nil {
			return err
		}
		if archiveCompletionProcessor == nil {
			return errors.New("archive completion processor unavailable")
		}
	}
	if expiryFactory, ok := archiveFactory.(SchedulerArchiveExpiryFactory); ok {
		archiveExpiryProcessor, err = expiryFactory.BuildArchiveExpiry(ctx, pool)
		if err != nil {
			return err
		}
		if archiveExpiryProcessor == nil {
			return errors.New("archive expiry processor unavailable")
		}
	}
	if cleanupFactory, ok := archiveFactory.(SchedulerArchiveCleanupFactory); ok {
		archiveCleanupProcessor, err = cleanupFactory.BuildArchiveCleanup(ctx, pool, storage, storageConfig.Bucket)
		if err != nil {
			return err
		}
		if archiveCleanupProcessor == nil {
			return errors.New("archive cleanup processor unavailable")
		}
	}
	if recoveryFactory, ok := archiveFactory.(SchedulerArchiveRecoveryFactory); ok {
		archiveRecoveryProcessor, err = recoveryFactory.BuildArchiveRecovery(ctx, pool, storage, storageConfig.Bucket)
		if err != nil {
			return err
		}
		if archiveRecoveryProcessor == nil {
			return errors.New("archive recovery processor unavailable")
		}
	}
	if revocationFactory, ok := archiveFactory.(SchedulerArchiveRevocationFactory); ok {
		archiveRevocationProcessor, err = revocationFactory.BuildArchiveRevocation(ctx, pool, storage, storageConfig.Bucket)
		if err != nil {
			return err
		}
		if archiveRevocationProcessor == nil {
			return errors.New("archive revocation processor unavailable")
		}
	}
	if invalidationFactory, ok := archiveFactory.(SchedulerArchiveInvalidationFactory); ok {
		archiveInvalidationProcessor, err = invalidationFactory.BuildArchiveInvalidation(ctx, pool)
		if err != nil {
			return err
		}
		if archiveInvalidationProcessor == nil {
			return errors.New("archive invalidation processor unavailable")
		}
	}
	interchangeCollectionProcessor := InterchangeCollectionProcessor{Store: SQLInterchangeCollectionStore{DB: pool}, Objects: storage, Bucket: storageConfig.Bucket}
	var interchangeImportProcessor *InterchangeImportProcessor
	var interchangeImportRuntimeReady atomic.Bool
	var interchangeExportProcessor ExpansionJob
	if exportFactory != nil {
		interchangeExportProcessor, err = installSchedulerInterchangeExport(ctx,
			exportFactory, pool, storage, storageConfig.Bucket)
		if err != nil {
			return err
		}
	}
	if len(importFactories) == 1 {
		continuation, buildErr := importFactories[0].BuildSchedulerInterchangeImport(ctx, pool, storage, storageConfig.Bucket)
		if buildErr != nil {
			return buildErr
		}
		if continuation == nil {
			return errors.New("interchange import continuation unavailable")
		}
		if err := validateInterchangeImportRuntimeSQL(ctx, pool); err != nil {
			return err
		}
		interchangeImportProcessor = &InterchangeImportProcessor{Store: SQLInterchangeImportStore{DB: pool}, Objects: storage, Continuation: continuation, Bucket: storageConfig.Bucket, Timeout: 10 * time.Minute}
	}

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
	renderDerivativeCleanupProcessor := RenderDerivativeCleanup{DB: pool}
	renderDerivativeProcessor := &RenderDerivativeCoordinator{DB: pool, Objects: storage, Bucket: storageConfig.Bucket, WorkSHA: workSHA, SRGBSHA: profileSHAs["srgb_v1"]}
	perspectiveQualityProcessor := &PerspectiveQualityProcessor{Source: SQLPerspectiveQualityProducers{DB: pool},
		Preparer: PerspectiveQualityCoordinator{DB: pool, Objects: storage, Bucket: storageConfig.Bucket, WorkingSHA: workSHA}}
	perspectivePreviewProcessor := &PerspectivePreviewProcessor{Source: SQLPerspectivePreviewProducers{DB: pool},
		Preparer: PerspectivePreviewCoordinator{DB: pool, Objects: storage, Bucket: storageConfig.Bucket, WorkingSHA: workSHA, SRGBSHA: profileSHAs["srgb_v1"]}, SRGBSHA: profileSHAs["srgb_v1"]}
	multiPreviewProcessor := &MultiPreviewProcessor{Source: SQLMultiPreviewProducers{DB: pool}, Preparer: MultiPreviewCoordinator{DB: pool, Objects: storage, Bucket: storageConfig.Bucket, WorkingSHA: workSHA, SRGBSHA: profileSHAs["srgb_v1"]}, SRGBSHA: profileSHAs["srgb_v1"]}
	multiQualityProcessor := &MultiQualityProcessor{Source: SQLMultiQualityProducers{DB: pool}, Preparer: MultiQualityCoordinator{DB: pool, Objects: storage, Bucket: storageConfig.Bucket, WorkingSHA: workSHA}}
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
	nativeStartup, stopNativeStartup := context.WithTimeout(ctx, 10*time.Second)
	nativeRecoveryProcessor, closeNativeRecovery, err := newSchedulerNativeRecovery(nativeStartup, nativeFactory, pool, storage, storageConfig.Bucket)
	if err == nil && nativeRecoveryProcessor != nil {
		err = verifyNativeRecoveryInstallation(nativeStartup, pool)
		if err != nil && closeNativeRecovery != nil {
			closeNativeRecovery()
			closeNativeRecovery = nil
		}
	}
	stopNativeStartup()
	if err != nil {
		return err
	}
	if closeNativeRecovery != nil {
		defer closeNativeRecovery()
	}

	linesConfig, linesEnabled, err := PerspectiveLinesRecoveryFromEnvironment(os.Getenv)
	if err != nil {
		return err
	}
	var linesRecoveryProcessor *PerspectiveLinesRecoveryDispatcher
	if linesEnabled {
		linesStartup, stopLinesStartup := context.WithTimeout(ctx, 10*time.Second)
		linesRecoveryProcessor, err = NewPerspectiveLinesRecoveryProcessor(linesStartup, pool, storage, storageConfig.Bucket, workSHA, linesConfig)
		stopLinesStartup()
		if err != nil {
			return err
		}
	}

	replacementAdoptionRunner, replacementAdoptionEnabled, err := ReplacementDetectionAdoptionFromEnvironment(os.Getenv)
	if err != nil {
		return err
	}
	var replacementAdoptionProcessor *ReplacementDetectionAdoptionDispatcher
	replacementAdoptionState := new(ExpansionLoopState)
	if replacementAdoptionEnabled {
		replacementAdoptionProcessor, err = NewReplacementDetectionAdoptionProcessor(ctx, pool, storage, storageConfig.Bucket, replacementAdoptionRunner)
		if err != nil {
			return err
		}
	}

	restorationConfig, restorationEnabled, err := RestorationAdoptionFromEnvironment(os.Getenv)
	if err != nil {
		return err
	}
	var restorationAdoptionProcessor ExpansionJob
	restorationAdoptionState := new(ExpansionLoopState)
	if restorationEnabled {
		restorationStartup, stopRestorationStartup := context.WithTimeout(ctx, 10*time.Second)
		var closeRestoration func()
		restorationAdoptionProcessor, closeRestoration, err = NewRestorationAdoptionProcessor(
			restorationStartup, pool, storage, storageConfig.Bucket, restorationConfig)
		stopRestorationStartup()
		if err != nil {
			return err
		}
		defer closeRestoration()
	}
	restorationPreviewEnabled, err := RestorationPreviewFromEnvironment(os.Getenv)
	if err != nil {
		return err
	}
	renderBatchProcessor.Restoration = restorationPreviewEnabled
	var restorationPreviewProcessor *RestorationPreviewProcessor
	if restorationPreviewEnabled {
		previewStartup, stopPreviewStartup := context.WithTimeout(ctx, 10*time.Second)
		restorationPreviewProcessor, err = NewRestorationPreviewProcessor(previewStartup,
			pool, storage, storageConfig.Bucket, workSHA, profileSHAs["srgb_v1"])
		stopPreviewStartup()
		if err != nil {
			return err
		}
	}

	deliveryConfig, err := DeliveryRuntimeFromEnvironment(os.Getenv)
	if err != nil {
		return err
	}
	deliveryState := new(ExpansionLoopState)
	var deliveryRuntimeReady atomic.Bool
	signingDeliveryTenants := []uuid.UUID{}
	var deliveryProcessor DeliveryExpansionJob
	if deliveryConfig.Enabled {
		if err := validateDeliveryRuntimeSQL(ctx, pool); err != nil {
			return err
		}
		deliveryRegistry, registryErr := LoadRegistry()
		if registryErr != nil {
			return registryErr
		}
		deliveryProcessor, err = NewDeliveryExpansionJob(pool, storage, storage, storageConfig.Bucket, workSHA, profileSHAs, deliveryRegistry, deliveryConfig)
		if err != nil {
			return err
		}
	}
	if signingFactory != nil {
		if !deliveryConfig.Enabled {
			return errors.New("delivery signing requires delivery scheduler")
		}
		startup, cancel := context.WithTimeout(ctx, 30*time.Second)
		signingRuntime, closeSigning, signingErr := signingFactory(startup, dsn,
			pool, storage, storageConfig.Bucket, workSHA)
		cancel()
		if signingErr != nil {
			return signingErr
		}
		if closeSigning == nil {
			return errors.New("delivery signing lifecycle missing")
		}
		defer closeSigning()
		deliveryProcessor, err = InstallDeliverySigningStage(deliveryProcessor, signingRuntime)
		if err != nil {
			return err
		}
		for id := range signingRuntime.Tenants {
			signingDeliveryTenants = append(signingDeliveryTenants, id)
		}
	}

	metadataAdoptionRunner, metadataAdoptionEnabled, err := MetadataAdoptionFromEnvironment(os.Getenv)
	if err != nil {
		return err
	}
	metadataAdoptionState := new(ExpansionLoopState)
	var metadataAdoptionProcessor *MetadataAdoptionDispatcher
	if metadataAdoptionEnabled {
		if !deliveryConfig.Enabled {
			return fmt.Errorf("metadata adoption requires delivery scheduler")
		}
		metadataAdoptionProcessor, err = NewMetadataAdoptionProcessor(ctx, pool, storageConfig.Bucket, metadataAdoptionRunner)
		if err != nil {
			return err
		}
	}

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
	var semanticAssemblyProcessor v2input.SemanticAssemblyOrchestrator
	if semanticAssemblyInstalled {
		semanticAssemblyProcessor, err = (v2input.SemanticAssemblyRuntime{DB: pool, Objects: storage, Bucket: storageConfig.Bucket}).Orchestrator()
		if err != nil {
			return err
		}
	}
	var learningEvaluateProcessor v2learning.LearningEvaluateOrchestrator
	if learningEvaluateInstalled {
		learningEvaluateProcessor, err = (v2learning.TrainingFitRuntime{
			DB: pool, Objects: storage, Bucket: storageConfig.Bucket,
		}).EvaluateOrchestrator()
		if err != nil {
			return err
		}
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
		if (learningEvaluateInstalled && !learningEvaluateState.Ready.Load()) || (restorationEnabled && !restorationAdoptionState.Ready.Load()) || (restorationPreviewEnabled && !restorationPreviewState.Ready.Load()) || (metadataAdoptionEnabled && !metadataAdoptionState.Ready.Load()) || (replacementAdoptionEnabled && !replacementAdoptionState.Ready.Load()) || !ready.Load() || (deliveryConfig.Enabled && (!deliveryState.Ready.Load() || !deliveryRuntimeReady.Load())) || (captureEnabled && !captureState.Ready.Load()) || (sessionPlanInstalled && !sessionPlanState.Ready.Load()) || (semanticAssemblyInstalled && !semanticAssemblyState.Ready.Load()) || (sceneMaterializationInstalled && !sceneMaterializationState.Ready.Load()) || !expansionState.Ready.Load() || !ingestState.Ready.Load() || !interchangeCollectionState.Ready.Load() || (interchangeImportProcessor != nil && (!interchangeImportState.Ready.Load() || !interchangeImportRuntimeReady.Load())) || (interchangeExportProcessor != nil && !interchangeExportState.Ready.Load()) || (archiveInvalidationProcessor != nil && !archiveInvalidationState.Ready.Load()) || (archiveRevocationProcessor != nil && !archiveRevocationState.Ready.Load()) || (archiveRecoveryProcessor != nil && !archiveRecoveryState.Ready.Load()) || (archiveCleanupProcessor != nil && !archiveCleanupState.Ready.Load()) || (archiveExpiryProcessor != nil && !archiveExpiryState.Ready.Load()) || (archiveCompletionProcessor != nil && !archiveCompletionState.Ready.Load()) || !uploadState.Ready.Load() || !downloadState.Ready.Load() || !bulkState.Ready.Load() || !styleState.Ready.Load() || !styleAnalysisState.Ready.Load() || !styleImportState.Ready.Load() || !embeddingState.Ready.Load() || !exportBatchState.Ready.Load() || !renderBatchState.Ready.Load() || !renderDerivativeCleanupState.Ready.Load() || !renderDerivativeState.Ready.Load() || !perspectivePreviewState.Ready.Load() || !multiQualityState.Ready.Load() || !multiPreviewState.Ready.Load() || !perspectiveQualityState.Ready.Load() || !renderRecoveryState.Ready.Load() || (nativeRecoveryProcessor != nil && !nativeRecoveryState.Ready.Load()) || (linesEnabled && (!perspectiveLinesRecoveryState.Ready.Load() || !perspectiveLinesLifecycleState.Ready.Load())) || !interactiveState.Ready.Load() || !interactiveFinalizeState.Ready.Load() || !applyState.Ready.Load() || !mediaRetryState.Ready.Load() || !mediaFinalizeState.Ready.Load() || pool.Ping(ping) != nil || storage.Ready(ping) != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"unavailable"}`))
			return
		}
		w.Write([]byte(`{"status":"ready"}`))
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "# TYPE photo_scheduler_v28_restoration_previews_total counter\nphoto_scheduler_v28_restoration_previews_total %d\n# TYPE photo_scheduler_v28_restoration_preview_failures_total counter\nphoto_scheduler_v28_restoration_preview_failures_total %d\n", restorationPreviewState.Materialized.Load(), restorationPreviewState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_v28_restoration_adoptions_total counter\nphoto_scheduler_v28_restoration_adoptions_total %d\n# TYPE photo_scheduler_v28_restoration_adoption_failures_total counter\nphoto_scheduler_v28_restoration_adoption_failures_total %d\n", restorationAdoptionState.Materialized.Load(), restorationAdoptionState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_archive_invalidated_total counter\nphoto_scheduler_archive_invalidated_total %d\n# TYPE photo_scheduler_archive_invalidation_failures_total counter\nphoto_scheduler_archive_invalidation_failures_total %d\n", archiveInvalidationState.Materialized.Load(), archiveInvalidationState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_archive_revoked_total counter\nphoto_scheduler_archive_revoked_total %d\n# TYPE photo_scheduler_archive_revocation_failures_total counter\nphoto_scheduler_archive_revocation_failures_total %d\n", archiveRevocationState.Materialized.Load(), archiveRevocationState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_archive_recovered_total counter\nphoto_scheduler_archive_recovered_total %d\n# TYPE photo_scheduler_archive_recovery_failures_total counter\nphoto_scheduler_archive_recovery_failures_total %d\n", archiveRecoveryState.Materialized.Load(), archiveRecoveryState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_archive_cleaned_total counter\nphoto_scheduler_archive_cleaned_total %d\n# TYPE photo_scheduler_archive_cleanup_failures_total counter\nphoto_scheduler_archive_cleanup_failures_total %d\n", archiveCleanupState.Materialized.Load(), archiveCleanupState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_archive_expired_total counter\nphoto_scheduler_archive_expired_total %d\n# TYPE photo_scheduler_archive_expiry_failures_total counter\nphoto_scheduler_archive_expiry_failures_total %d\n", archiveExpiryState.Materialized.Load(), archiveExpiryState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_archive_completion_handled_total counter\nphoto_scheduler_archive_completion_handled_total %d\n# TYPE photo_scheduler_archive_completion_failures_total counter\nphoto_scheduler_archive_completion_failures_total %d\n", archiveCompletionState.Materialized.Load(), archiveCompletionState.Failures.Load())
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# TYPE photo_scheduler_perspective_quality_materialized_total counter\nphoto_scheduler_perspective_quality_materialized_total %d\n# TYPE photo_scheduler_perspective_quality_failures_total counter\nphoto_scheduler_perspective_quality_failures_total %d\n", perspectiveQualityState.Materialized.Load(), perspectiveQualityState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_multi_preview_materialized_total counter\nphoto_scheduler_multi_preview_materialized_total %d\n# TYPE photo_scheduler_multi_preview_failures_total counter\nphoto_scheduler_multi_preview_failures_total %d\n", multiPreviewState.Materialized.Load(), multiPreviewState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_multi_quality_materialized_total counter\nphoto_scheduler_multi_quality_materialized_total %d\n# TYPE photo_scheduler_multi_quality_failures_total counter\nphoto_scheduler_multi_quality_failures_total %d\n", multiQualityState.Materialized.Load(), multiQualityState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_perspective_preview_materialized_total counter\nphoto_scheduler_perspective_preview_materialized_total %d\n# TYPE photo_scheduler_perspective_preview_failures_total counter\nphoto_scheduler_perspective_preview_failures_total %d\n", perspectivePreviewState.Materialized.Load(), perspectivePreviewState.Failures.Load())
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
		fmt.Fprintf(w, "# TYPE photo_scheduler_v27_assemblies_total counter\nphoto_scheduler_v27_assemblies_total %d\n# TYPE photo_scheduler_v27_assembly_failures_total counter\nphoto_scheduler_v27_assembly_failures_total %d\n", semanticAssemblyState.Materialized.Load(), semanticAssemblyState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_v23_evaluations_total counter\nphoto_scheduler_v23_evaluations_total %d\n# TYPE photo_scheduler_v23_evaluation_failures_total counter\nphoto_scheduler_v23_evaluation_failures_total %d\n", learningEvaluateState.Materialized.Load(), learningEvaluateState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_v30_delivery_pages_total counter\nphoto_scheduler_v30_delivery_pages_total %d\n# TYPE photo_scheduler_v30_delivery_failures_total counter\nphoto_scheduler_v30_delivery_failures_total %d\n", deliveryState.Materialized.Load(), deliveryState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_v30_export_adoptions_total counter\nphoto_scheduler_v30_export_adoptions_total %d\n# TYPE photo_scheduler_v30_export_adoption_failures_total counter\nphoto_scheduler_v30_export_adoption_failures_total %d\n", interchangeExportState.Materialized.Load(), interchangeExportState.Failures.Load())
		fmt.Fprintf(w, "# TYPE photo_scheduler_v30_metadata_adoptions_total counter\nphoto_scheduler_v30_metadata_adoptions_total %d\n# TYPE photo_scheduler_v30_metadata_adoption_failures_total counter\nphoto_scheduler_v30_metadata_adoption_failures_total %d\n", metadataAdoptionState.Materialized.Load(), metadataAdoptionState.Failures.Load())
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
	if deliveryConfig.Enabled {
		deliveryCtx, stopDelivery := context.WithCancel(ctx)
		deliveryDone := make(chan error, 1)
		runtimeDone := make(chan struct{})
		instance := uuid.New()
		go func() { deliveryDone <- RunExpansionLoop(deliveryCtx, deliveryProcessor, deliveryState, 5*time.Second) }()
		go func() {
			maintainDeliveryRuntime(deliveryCtx, pool, deliveryState, &deliveryRuntimeReady, instance, storageConfig.Bucket, signingDeliveryTenants)
			close(runtimeDone)
		}()
		defer func() { stopDelivery(); <-deliveryDone; <-runtimeDone }()
	}
	if metadataAdoptionEnabled {
		metadataCtx, stopMetadata := context.WithCancel(ctx)
		metadataDone := make(chan error, 1)
		go func() {
			metadataDone <- RunExpansionLoop(metadataCtx, metadataAdoptionProcessor, metadataAdoptionState, 5*time.Second)
		}()
		defer func() { stopMetadata(); <-metadataDone }()
	}
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
	if semanticAssemblyInstalled {
		semanticCtx, stopSemantic := context.WithCancel(ctx)
		semanticDone := make(chan error, 1)
		go func() {
			semanticDone <- RunExpansionLoop(semanticCtx, semanticAssemblyProcessor, semanticAssemblyState, 5*time.Second)
		}()
		defer func() { stopSemantic(); <-semanticDone }()
	}
	if learningEvaluateInstalled {
		learningCtx, stopLearning := context.WithCancel(ctx)
		learningDone := make(chan error, 1)
		go func() {
			learningDone <- RunExpansionLoop(learningCtx, learningEvaluateProcessor,
				learningEvaluateState, 5*time.Second)
		}()
		defer func() { stopLearning(); <-learningDone }()
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
	collectionCtx, stopCollections := context.WithCancel(ctx)
	collectionDone := make(chan error, 1)
	go func() {
		collectionDone <- RunExpansionLoop(collectionCtx, interchangeCollectionProcessor, interchangeCollectionState, 5*time.Second)
	}()
	defer func() { stopCollections(); <-collectionDone }()
	if interchangeImportProcessor != nil {
		importCtx, stopImport := context.WithCancel(ctx)
		importDone := make(chan error, 1)
		runtimeDone := make(chan struct{})
		instance := uuid.New()
		go func() {
			importDone <- RunExpansionLoop(importCtx, interchangeImportProcessor, interchangeImportState, 5*time.Second)
		}()
		go func() {
			maintainInterchangeImportRuntime(importCtx, pool, interchangeImportState,
				&interchangeImportRuntimeReady, instance, storageConfig.Bucket)
			close(runtimeDone)
		}()
		defer func() { stopImport(); <-importDone; <-runtimeDone }()
	}
	if interchangeExportProcessor != nil {
		exportCtx, stopExport := context.WithCancel(ctx)
		exportDone := make(chan error, 1)
		go func() {
			exportDone <- RunExpansionLoop(exportCtx, interchangeExportProcessor, interchangeExportState, 5*time.Second)
		}()
		defer func() { stopExport(); <-exportDone }()
	}

	if archiveInvalidationProcessor != nil {
		invalidationCtx, stopInvalidation := context.WithCancel(ctx)
		invalidationDone := make(chan error, 1)
		go func() {
			invalidationDone <- RunExpansionLoop(invalidationCtx, archiveInvalidationProcessor, archiveInvalidationState, 5*time.Second)
		}()
		defer func() { stopInvalidation(); <-invalidationDone }()
	}
	if archiveRevocationProcessor != nil {
		revokeCtx, stopRevoke := context.WithCancel(ctx)
		revokeDone := make(chan error, 1)
		go func() {
			revokeDone <- RunExpansionLoop(revokeCtx, archiveRevocationProcessor, archiveRevocationState, 5*time.Second)
		}()
		defer func() { stopRevoke(); <-revokeDone }()
	}
	if archiveRecoveryProcessor != nil {
		recoveryCtx, stopRecovery := context.WithCancel(ctx)
		recoveryDone := make(chan error, 1)
		go func() {
			recoveryDone <- RunExpansionLoop(recoveryCtx, archiveRecoveryProcessor, archiveRecoveryState, 5*time.Second)
		}()
		defer func() { stopRecovery(); <-recoveryDone }()
	}
	if archiveCleanupProcessor != nil {
		cleanupCtx, stopCleanup := context.WithCancel(ctx)
		cleanupDone := make(chan error, 1)
		go func() {
			cleanupDone <- RunExpansionLoop(cleanupCtx, archiveCleanupProcessor, archiveCleanupState, 5*time.Second)
		}()
		defer func() { stopCleanup(); <-cleanupDone }()
	}
	if archiveExpiryProcessor != nil {
		expiryCtx, stopExpiry := context.WithCancel(ctx)
		expiryDone := make(chan error, 1)
		go func() {
			expiryDone <- RunExpansionLoop(expiryCtx, archiveExpiryProcessor, archiveExpiryState, 5*time.Second)
		}()
		defer func() { stopExpiry(); <-expiryDone }()
	}
	if archiveCompletionProcessor != nil {
		archiveCtx, stopArchives := context.WithCancel(ctx)
		archiveDone := make(chan error, 1)
		go func() {
			archiveDone <- RunExpansionLoop(archiveCtx, archiveCompletionProcessor, archiveCompletionState, 5*time.Second)
		}()
		defer func() { stopArchives(); <-archiveDone }()
	}
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
	qualityCtx, stopPerspectiveQuality := context.WithCancel(ctx)
	qualityDone := make(chan error, 1)
	go func() {
		qualityDone <- RunExpansionLoop(qualityCtx, perspectiveQualityProcessor, perspectiveQualityState, 5*time.Second)
	}()
	defer func() { stopPerspectiveQuality(); <-qualityDone }()
	perspectiveCtx, stopPerspectivePreview := context.WithCancel(ctx)
	perspectiveDone := make(chan error, 1)
	go func() {
		perspectiveDone <- RunExpansionLoop(perspectiveCtx, perspectivePreviewProcessor, perspectivePreviewState, 5*time.Second)
	}()
	defer func() { stopPerspectivePreview(); <-perspectiveDone }()
	multiPreviewCtx, stopMultiPreview := context.WithCancel(ctx)
	multiPreviewDone := make(chan error, 1)
	go func() {
		multiPreviewDone <- RunExpansionLoop(multiPreviewCtx, multiPreviewProcessor, multiPreviewState, 5*time.Second)
	}()
	defer func() { stopMultiPreview(); <-multiPreviewDone }()
	multiQualityCtx, stopMultiQuality := context.WithCancel(ctx)
	multiQualityDone := make(chan error, 1)
	go func() {
		multiQualityDone <- RunExpansionLoop(multiQualityCtx, multiQualityProcessor, multiQualityState, 5*time.Second)
	}()
	defer func() { stopMultiQuality(); <-multiQualityDone }()

	derivativeCtx, stopDerivative := context.WithCancel(ctx)
	derivativeDone := make(chan error, 1)
	go func() {
		derivativeDone <- RunExpansionLoop(derivativeCtx, renderDerivativeProcessor, renderDerivativeState, 5*time.Second)
	}()
	defer func() { stopDerivative(); <-derivativeDone }()
	derivativeCleanupCtx, stopDerivativeCleanup := context.WithCancel(ctx)
	derivativeCleanupDone := make(chan error, 1)
	go func() {
		derivativeCleanupDone <- RunExpansionLoop(derivativeCleanupCtx, renderDerivativeCleanupProcessor, renderDerivativeCleanupState, 5*time.Second)
	}()
	defer func() { stopDerivativeCleanup(); <-derivativeCleanupDone }()
	renderRecoveryCtx, stopRenderRecovery := context.WithCancel(ctx)
	renderRecoveryDone := make(chan error, 1)
	go func() {
		renderRecoveryDone <- RunExpansionLoop(renderRecoveryCtx, renderRecoveryProcessor, renderRecoveryState, 5*time.Second)
	}()
	defer func() { stopRenderRecovery(); <-renderRecoveryDone }()

	if nativeRecoveryProcessor != nil {
		nativeCtx, stopNative := context.WithCancel(ctx)
		nativeDone := make(chan error, 1)
		go func() {
			nativeDone <- RunExpansionLoop(nativeCtx, nativeRecoveryProcessor, nativeRecoveryState, 5*time.Second)
		}()
		defer func() { stopNative(); <-nativeDone }()
	}

	if linesRecoveryProcessor != nil {
		linesCtx, stopLines := context.WithCancel(ctx)
		linesDone := make(chan error, 1)
		go func() {
			linesDone <- RunExpansionLoop(linesCtx, linesRecoveryProcessor, perspectiveLinesRecoveryState, 5*time.Second)
		}()
		defer func() { stopLines(); <-linesDone }()
		lifecycleCtx, stopLifecycle := context.WithCancel(ctx)
		lifecycleDone := make(chan error, 1)
		go func() {
			lifecycleDone <- RunExpansionLoop(lifecycleCtx, PerspectiveLinesLifecycle{DB: pool}, perspectiveLinesLifecycleState, 5*time.Second)
		}()
		defer func() { stopLifecycle(); <-lifecycleDone }()
	}

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
	// Independent from lease recovery/dispatch. Configured replacement work reads
	// original object versions; qualification evidence is never synthesized.
	negativeCtx, stopNegative := context.WithCancel(ctx)
	negativeDone := make(chan error, 1)
	negativeState := new(ExpansionLoopState)
	negativeProcessor := &v2optical.NegativeScheduler{Backend: negativeBackend}
	go func() { negativeDone <- RunExpansionLoop(negativeCtx, negativeProcessor, negativeState, 5*time.Second) }()
	defer func() { stopNegative(); <-negativeDone }()
	if replacementAdoptionProcessor != nil {
		adoptionCtx, stopAdoption := context.WithCancel(ctx)
		adoptionDone := make(chan error, 1)
		go func() {
			adoptionDone <- RunExpansionLoop(adoptionCtx, replacementAdoptionProcessor, replacementAdoptionState, 5*time.Second)
		}()
		defer func() { stopAdoption(); <-adoptionDone }()
	}
	if restorationAdoptionProcessor != nil {
		restorationCtx, stopRestoration := context.WithCancel(ctx)
		restorationDone := make(chan error, 1)
		go func() {
			restorationDone <- RunExpansionLoop(restorationCtx, restorationAdoptionProcessor,
				restorationAdoptionState, 5*time.Second)
		}()
		defer func() { stopRestoration(); <-restorationDone }()
	}
	if restorationPreviewProcessor != nil {
		previewCtx, stopPreview := context.WithCancel(ctx)
		previewDone := make(chan error, 1)
		go func() {
			previewDone <- RunExpansionLoop(previewCtx, restorationPreviewProcessor,
				restorationPreviewState, 5*time.Second)
		}()
		defer func() { stopPreview(); <-previewDone }()
	}
	detectorQuotaCtx, stopDetectorQuota := context.WithCancel(ctx)
	detectorQuotaDone := make(chan error, 1)
	detectorQuotaState := new(ExpansionLoopState)
	go func() {
		detectorQuotaDone <- RunExpansionLoop(detectorQuotaCtx, ReplacementDetectorQuotaCleanup{DB: pool}, detectorQuotaState, 5*time.Second)
	}()
	defer func() { stopDetectorQuota(); <-detectorQuotaDone }()
	negativeQuotaCtx, stopNegativeQuota := context.WithCancel(ctx)
	negativeQuotaDone := make(chan error, 1)
	negativeQuotaState := new(ExpansionLoopState)
	go func() {
		negativeQuotaDone <- RunExpansionLoop(negativeQuotaCtx, NegativeCancellationQuota{DB: pool}, negativeQuotaState, 5*time.Second)
	}()
	defer func() { stopNegativeQuota(); <-negativeQuotaDone }()
	recovery, dispatcher := Recovery{DB: pool}, Dispatcher{DB: pool, LegacyOnly: capacityRuntime != nil}
	ingest := IngestActivator{Source: IngestActivationSource{DB: pool}, Tasks: AdvancementRepository{DB: pool}}
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	var nextLocalCapacityObservation time.Time
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
			capacityFailed := false
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
			if e == nil && capacityRuntime != nil {
				n, e = capacityRuntime.Dispatch(cycle, 20)
				if n > 0 {
					dispatched.Add(uint64(n))
				}
				capacityFailed = e != nil
			}
			if e == nil && localCapacityMonitor != nil && !time.Now().Before(nextLocalCapacityObservation) {
				alerts, observationErr := localCapacityMonitor.Tick(cycle)
				nextLocalCapacityObservation = time.Now().Add(10 * time.Second)
				if observationErr != nil {
					slog.Warn("local capacity observation deferred")
				}
				for _, alert := range alerts {
					if alert.Active {
						slog.Warn("local capacity pressure", "pool", alert.Pool, "reason", alert.Reason)
					}
					if !alert.Active {
						slog.Info("local capacity pressure cleared", "pool", alert.Pool, "reason", alert.Reason)
					}
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
				if capacityFailed && ctx.Err() == nil {
					return fmt.Errorf("V32 capacity dispatcher stopped: %w", e)
				}
				if ctx.Err() == nil {
					slog.Warn("scheduler cycle deferred")
				}
				continue
			}
			ready.Store(true)
		}
	}
}
