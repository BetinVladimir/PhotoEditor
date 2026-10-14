-- +goose Up
-- A statistic's image must be the accepted frozen source consumed by its
-- canonical series.measure task, under the same claimed balance chain.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_series_stat_image(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_balance uuid,p_attempt integer,p_fence bigint,p_params uuid,p_params_sha text,
 p_params_bucket text,p_params_key text,p_params_version text,p_params_bytes bigint,
 p_stat uuid,p_stat_sha text,p_image uuid,p_image_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_image IS NULL OR p_image=p_stat OR p_image=p_params
  OR coalesce(p_image_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_series_stat(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_balance,p_attempt,p_fence,p_params,p_params_sha,p_params_bucket,p_params_key,
  p_params_version,p_params_bytes,p_stat,p_stat_sha);
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_image,p_image_sha,'WorkingImage');
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.artifacts stats
 JOIN photo.tasks measure ON measure.tenant_id=stats.tenant_id
  AND measure.id=stats.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=measure.tenant_id
  AND result.id=measure.canonical_result_id AND result.task_id=measure.id
  AND result.attempt=measure.attempt AND result.fencing_token=measure.fencing_token
 JOIN photo.snapshot_items item ON item.tenant_id=measure.tenant_id
  AND item.snapshot_id=w.input_snapshot_id AND item.artifact_id=p_image
  AND item.sha256=p_image_sha
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
  AND version.id=item.asset_version_id AND version.master_artifact_id=p_image
  AND version.content_hash=p_image_sha AND version.review_state='accepted'
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id
  AND asset.id=version.asset_id AND asset.project_id=w.project_id
  AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 JOIN photo.artifacts image ON image.tenant_id=item.tenant_id
  AND image.id=p_image AND image.sha256=p_image_sha
  AND image.output_role='working_image' AND image.media_type='application/json'
  AND image.state='ready' AND image.verified_at IS NOT NULL
  AND image.object_version IS NOT NULL AND image.object_version NOT IN('','null')
  AND (image.expires_at IS NULL OR image.expires_at>clock_timestamp())
 WHERE stats.tenant_id=p_tenant AND stats.id=p_stat AND stats.sha256=p_stat_sha
  AND stats.produced_attempt=measure.attempt AND stats.produced_fence=measure.fencing_token
  AND measure.operation='series.measure' AND measure.operation_version='1'
  AND measure.state='succeeded' AND NOT measure.cancel_requested
  AND measure.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_image,'sha256',p_image_sha))
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_stat,'sha256',p_stat_sha)));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_series_stat_image(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,text,text,text,bigint,uuid,text,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_series_stat_image(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,text,text,text,bigint,uuid,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_series_stat_image(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,text,text,text,bigint,uuid,text,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
