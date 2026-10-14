-- +goose Up
-- Declared origin locators only. Physical replay and live worker admission are
-- separate; no arbitrary historical/newest producer fallback.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_manual_pair_origin(p_tenant uuid,p_project uuid,
 p_snapshot uuid,p_actor uuid,p_session uuid,p_base jsonb,
 p_target uuid,p_target_sha text,p_guard uuid,p_guard_sha text,
 p_target_pixel uuid,p_target_pixel_sha text,p_guard_pixel uuid,p_guard_pixel_sha text)
RETURNS TABLE(root_id uuid,root_sha text,params_id uuid,params_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 WITH candidates AS (
 SELECT root.id AS root_id,root.sha256 AS root_sha,params.id AS params_id,params.sha256 AS params_sha,
  count(*) OVER() AS matches
 FROM photo.artifacts target
 JOIN photo.artifacts guard ON guard.tenant_id=target.tenant_id AND guard.id=p_guard AND guard.sha256=p_guard_sha
  AND guard.created_by_task_id=target.created_by_task_id
 JOIN photo.tasks t ON t.tenant_id=target.tenant_id AND t.id=target.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=t.tenant_id AND result.id=t.canonical_result_id
  AND result.task_id=t.id AND result.attempt=t.attempt AND result.fencing_token=t.fencing_token
 JOIN photo.artifacts root ON root.tenant_id=t.tenant_id AND root.created_by_task_id=t.id
  AND root.output_role='rasterized_mask' AND root.produced_attempt=t.attempt AND root.produced_fence=t.fencing_token
 JOIN photo.artifacts params ON params.tenant_id=t.tenant_id AND params.id=t.params_artifact_id
 WHERE target.tenant_id=p_tenant AND target.id=p_target AND target.sha256=p_target_sha
  AND photo.v28_manual_mask_document_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base,
   p_target,p_target_sha,p_target_pixel,p_target_pixel_sha,false) IS TRUE
  AND photo.v28_manual_mask_document_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_base,
   p_guard,p_guard_sha,p_guard_pixel,p_guard_pixel_sha,true) IS TRUE
  AND root.media_type='application/json' AND root.bytes BETWEEN 2 AND 1048576
  AND params.media_type='application/json' AND params.bytes BETWEEN 2 AND 1048576
  AND jsonb_typeof(result.outputs)='array'
  AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256))
  AND NOT EXISTS(SELECT 1 FROM (VALUES(root.state,root.verified_at,root.object_version,root.expires_at),
   (params.state,params.verified_at,params.object_version,params.expires_at)) objects(state,verified,version,expires)
   WHERE state IS DISTINCT FROM 'ready' OR verified IS NULL OR version IS NULL
    OR version IN('','null') OR expires<=clock_timestamp()))
 SELECT root_id,root_sha,params_id,params_sha FROM candidates WHERE matches=1;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_manual_pair_origin(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_manual_pair_origin(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_manual_pair_origin(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,uuid,text,uuid,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
