package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2render"
	"time"
)

// A missing catalog blocks fresh work, but does not prevent exact pending or
// completed replay. Availability of this service never enables a GPU backend.
func buildRenderAdmission(ctx context.Context, api *pgxpool.Pool, dsn string, objects *objectstore.Store, bucket string, calibrations v2render.RenderCalibrationCatalog) (httpapi.RenderQueue, func(), error) {
	noop := func() {}
	if api == nil || objects == nil || len(bucket) < 3 || len(bucket) > 63 {
		return nil, noop, errors.New("render dependencies unavailable")
	}
	required := []string{
		"begin_v26_render_batch", "begin_v26_render_batch_part", "v26_render_batch_manifest", "v26_render_batch_part_state", "v26_render_draft_state",
		"stage_v26_render_recipe_task", "v26_render_input_state", "resolve_v26_render_qualification", "v26_render_qualification_ready",
		"bind_v26_render_submission", "claim_v26_render_submission", "probe_v26_render_submission", "read_v26_render_submission_ticket", "complete_v26_render_submission",
		"quarantine_v26_render_worker_cache", "v26_render_worker_cache_artifacts", "v26_render_worker_cache_context", "lookup_v26_render_worker_cache", "v26_render_cache_artifacts", "lookup_v26_render_cache", "publish_v26_render_cache", "guard_v26_render_cache", "v26_render_worker_qualification", "v26_render_adoption_qualification", "v26_render_adoption_inputs", "adopt_v2_plan_pre_render_qualification", "v26_render_adoption_inputs_ready", "v26_render_live_qualification_ready", "bind_v26_render_qualification", "guard_v26_qualification_pin", "guard_v26_submission_identity", "guard_v26_render_response", "reserve_v2_input_document", "complete_v2_input_document", "register_v2_task_input_batch", "seal_v2_task_inputs", "seal_v2_graph",
	}
	var installed bool
	if err := api.QueryRow(ctx, `SELECT count(DISTINCT p.proname)=cardinality($1::text[]) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='photo' AND p.proname=ANY($1::text[])`, required).Scan(&installed); err != nil {
		return nil, noop, err
	}
	if !installed {
		return nil, noop, nil
	}
	if err := v2draft.RequireRenderConsentDiagnostic(ctx, api); err != nil {
		return nil, noop, err
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, noop, errors.New("render database configuration invalid")
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	config.ConnConfig.RuntimeParams["statement_timeout"] = "30000"
	config.MaxConns = 8
	config.MinConns = 0
	scheduler, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, noop, errors.New("render database unavailable")
	}
	for _, check := range []struct {
		pool *pgxpool.Pool
		role string
	}{{api, "photo_api"}, {scheduler, "photo_scheduler"}} {
		var role string
		if err := check.pool.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil || role != check.role {
			scheduler.Close()
			return nil, noop, errors.New("render database role unavailable")
		}
	}
	return v2render.SQLRenderSubmissionService{Factory: v2render.SQLRenderBatchFactory{API: api, Scheduler: scheduler, Sources: objects, Documents: objects, SourceBucket: bucket, DocumentBucket: bucket}, Calibrations: calibrations, Timeout: 2 * time.Minute}, scheduler.Close, nil
}
