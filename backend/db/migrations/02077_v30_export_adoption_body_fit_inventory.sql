-- +goose Up
-- Complete original pair.fit.body input inventory and accepted paired images
-- under the exact claimed BodyFit child. Native content checks remain separate.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_body_fit_inventory(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_parent uuid,p_parent_sha text,p_fit uuid,p_fit_sha text,
 p_producer uuid,p_attempt integer,p_fence bigint,p_params uuid,
 p_inputs jsonb,p_before uuid,p_before_sha text,p_after uuid,p_after_sha text,
 p_bucket text,p_fit_key text,p_fit_version text,p_fit_bytes bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_parent IS NULL OR p_fit IS NULL OR p_producer IS NULL OR p_params IS NULL
  OR p_before IS NULL OR p_after IS NULL OR p_before=p_after
  OR p_attempt<1 OR p_fence<1
  OR coalesce(p_parent_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_fit_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_before_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_after_sha,'') !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(p_inputs) IS DISTINCT FROM 'array'
  OR jsonb_array_length(p_inputs) NOT IN (7,9)
  OR coalesce(p_bucket,'')='' OR coalesce(p_fit_key,'')=''
  OR coalesce(p_fit_version,'') IN ('','null') OR p_fit_bytes<2
 THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_parent,p_parent_sha,'Params');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_fit,p_fit_sha,'BodyFit');
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL OR photo.v30_export_adoption_source_ready(
  p_tenant,w.project_id,w.input_snapshot_id,p_actor) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.tasks producer
  JOIN photo.artifacts fit ON fit.tenant_id=producer.tenant_id
   AND fit.created_by_task_id=producer.id AND fit.id=p_fit AND fit.sha256=p_fit_sha
  JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id
   AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt
   AND attempt.fencing_token=producer.fencing_token AND attempt.finished_at IS NOT NULL
  JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
   AND result.id=producer.canonical_result_id AND result.task_id=producer.id
   AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
  WHERE producer.tenant_id=p_tenant AND producer.id=p_producer
   AND producer.attempt=p_attempt AND producer.fencing_token=p_fence
   AND producer.operation='pair.fit.body' AND producer.operation_version='1'
   AND producer.state='succeeded' AND NOT producer.cancel_requested
   AND producer.resource_class='cpu' AND producer.params_artifact_id=p_params
   AND producer.inputs @> p_inputs
   AND fit.output_role='body_fit' AND fit.produced_attempt=p_attempt
   AND fit.produced_fence=p_fence AND fit.bucket=p_bucket
   AND fit.object_key=p_fit_key AND fit.object_version=p_fit_version
   AND fit.bytes=p_fit_bytes AND fit.state='ready' AND fit.verified_at IS NOT NULL
   AND fit.media_type='application/json'
   AND (fit.expires_at IS NULL OR fit.expires_at>clock_timestamp())
   AND result.outputs @> jsonb_build_array(
    jsonb_build_object('artifact_id',p_fit,'sha256',p_fit_sha))
   AND attempt.backend='cpu' AND attempt.profile->>'backend'='cpu'
   AND producer.execution_profile->>'backend'='cpu'
   AND NOT EXISTS(SELECT 1 FROM (VALUES(p_before,p_before_sha),(p_after,p_after_sha))
    required(id,sha) WHERE NOT EXISTS(SELECT 1 FROM photo.snapshot_items item
     JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
      AND version.id=item.asset_version_id
      AND version.master_artifact_id=required.id AND version.content_hash=required.sha
      AND version.review_state='accepted'
     JOIN photo.assets asset ON asset.tenant_id=version.tenant_id
      AND asset.id=version.asset_id AND asset.project_id=w.project_id
      AND asset.deleted_at IS NULL AND asset.upload_state='verified'
     JOIN photo.artifacts master ON master.tenant_id=version.tenant_id
      AND master.id=required.id AND master.sha256=required.sha
      AND master.state='ready' AND master.verified_at IS NOT NULL
      AND master.object_version IS NOT NULL AND master.object_version NOT IN ('','null')
      AND (master.expires_at IS NULL OR master.expires_at>clock_timestamp())
     WHERE item.tenant_id=p_tenant AND item.snapshot_id=w.input_snapshot_id
      AND item.artifact_id=required.id AND item.sha256=required.sha)));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_body_fit_inventory(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,integer,bigint,uuid,jsonb,uuid,text,uuid,text,text,text,text,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_body_fit_inventory(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,integer,bigint,uuid,jsonb,uuid,text,uuid,text,text,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_body_fit_inventory(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,integer,bigint,uuid,jsonb,uuid,text,uuid,text,text,text,bigint)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
