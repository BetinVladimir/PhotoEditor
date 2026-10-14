-- +goose Up
-- Sessionless, exact-claim provenance for SeriesCorrections. Native schema and
-- original series inputs are checked by the caller, not this metadata gate.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_series(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_series uuid,p_series_sha text,p_bucket text,p_object_key text,
 p_version text,p_bytes bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_series IS NULL OR coalesce(p_series_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_bucket,'')='' OR coalesce(p_object_key,'')=''
  OR coalesce(p_version,'') IN ('','null') OR p_bytes<2 OR p_bytes>8388608
 THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_series,p_series_sha,'SeriesCorrections');
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.artifacts series
 JOIN photo.tasks producer ON producer.tenant_id=series.tenant_id
  AND producer.id=series.created_by_task_id
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id
  AND params.id=producer.params_artifact_id
 JOIN photo.runs origin ON origin.tenant_id=producer.tenant_id AND origin.id=producer.run_id
 LEFT JOIN photo.v2_workflows origin_workflow ON origin_workflow.tenant_id=origin.tenant_id
  AND origin_workflow.run_id=origin.id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
  AND result.id=producer.canonical_result_id AND result.task_id=producer.id
  AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id
  AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt
  AND attempt.fencing_token=producer.fencing_token AND attempt.finished_at IS NOT NULL
 WHERE series.tenant_id=p_tenant AND series.id=p_series AND series.sha256=p_series_sha
  AND series.bucket=p_bucket AND series.object_key=p_object_key
  AND series.object_version=p_version AND series.bytes=p_bytes
  AND series.media_type='application/json' AND series.output_role='series_corrections'
  AND series.produced_attempt=producer.attempt AND series.produced_fence=producer.fencing_token
  AND producer.operation='series.balance' AND producer.operation_version='1'
  AND producer.state='succeeded' AND NOT producer.cancel_requested
  AND producer.resource_class='cpu' AND attempt.backend='cpu'
  AND attempt.profile->>'backend'='cpu' AND producer.execution_profile->>'backend'='cpu'
  AND NOT origin.cancel_requested
  AND (origin_workflow.project_id=w.project_id
   OR origin.config_snapshot->>'project_id'=w.project_id::text)
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_series,'sha256',p_series_sha))
  AND params.media_type='application/json' AND params.bytes BETWEEN 2 AND 1048576
  AND NOT EXISTS(SELECT 1 FROM (VALUES
   (series.state,series.verified_at,series.object_version,series.expires_at),
   (params.state,params.verified_at,params.object_version,params.expires_at))
   checked(state,verified,version,expires)
   WHERE state IS DISTINCT FROM 'ready' OR verified IS NULL OR version IS NULL
    OR version IN('','null') OR expires<=clock_timestamp()));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_series(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,text,text,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_series(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,text,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_series(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,text,text,bigint)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
