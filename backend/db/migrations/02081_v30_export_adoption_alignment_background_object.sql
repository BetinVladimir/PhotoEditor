-- +goose Up
-- The binary mask declared by original pair.register Params must remain an
-- input of that exact canonical registration producer under the export claim.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_alignment_background_object(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_alignment uuid,p_alignment_sha text,p_producer uuid,p_attempt integer,
 p_fence bigint,p_params uuid,p_mask uuid,p_mask_sha text,p_bucket text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,
 bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_alignment IS NULL OR p_producer IS NULL OR p_params IS NULL OR p_mask IS NULL
  OR p_attempt<1 OR p_fence<1
  OR coalesce(p_alignment_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_mask_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_bucket,'')=''
 THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_alignment,p_alignment_sha,'PairAlignment');
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL OR photo.v30_export_adoption_source_ready(
  p_tenant,w.project_id,w.input_snapshot_id,p_actor) IS DISTINCT FROM true
 THEN RETURN;END IF;
 RETURN QUERY SELECT mask.bucket,mask.object_key,mask.object_version,
  mask.sha256,mask.bytes,mask.media_type
 FROM photo.tasks consumer
 JOIN photo.artifacts alignment ON alignment.tenant_id=consumer.tenant_id
  AND alignment.id=p_alignment AND alignment.sha256=p_alignment_sha
  AND alignment.created_by_task_id=consumer.id
 JOIN photo.task_attempts attempt ON attempt.tenant_id=consumer.tenant_id
  AND attempt.task_id=consumer.id AND attempt.attempt=consumer.attempt
  AND attempt.fencing_token=consumer.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.task_results result ON result.tenant_id=consumer.tenant_id
  AND result.id=consumer.canonical_result_id AND result.task_id=consumer.id
  AND result.attempt=consumer.attempt AND result.fencing_token=consumer.fencing_token
 JOIN photo.artifacts mask ON mask.tenant_id=consumer.tenant_id
  AND mask.id=p_mask AND mask.sha256=p_mask_sha
 WHERE consumer.tenant_id=p_tenant AND consumer.id=p_producer
  AND consumer.attempt=p_attempt AND consumer.fencing_token=p_fence
  AND consumer.operation='pair.register' AND consumer.operation_version='1'
  AND consumer.state='succeeded' AND NOT consumer.cancel_requested
  AND consumer.resource_class='cpu' AND consumer.params_artifact_id=p_params
  AND consumer.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_mask,'sha256',p_mask_sha))
  AND alignment.output_role='alignment'
  AND alignment.produced_attempt=p_attempt AND alignment.produced_fence=p_fence
  AND alignment.state='ready' AND alignment.verified_at IS NOT NULL
  AND alignment.object_version IS NOT NULL
  AND alignment.object_version NOT IN ('','null')
  AND (alignment.expires_at IS NULL OR alignment.expires_at>clock_timestamp())
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_alignment,'sha256',p_alignment_sha))
  AND attempt.backend='cpu' AND attempt.profile->>'backend'='cpu'
  AND consumer.execution_profile->>'backend'='cpu'
  AND mask.bucket=p_bucket AND mask.state='ready'
  AND mask.verified_at IS NOT NULL
  AND mask.media_type IN ('application/octet-stream','application/x-npy')
  AND mask.bytes BETWEEN 1 AND 800065536
  AND mask.object_version IS NOT NULL AND mask.object_version NOT IN ('','null')
  AND (mask.expires_at IS NULL OR mask.expires_at>clock_timestamp())
 FOR SHARE OF consumer,alignment,mask;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_alignment_background_object(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,integer,bigint,uuid,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_alignment_background_object(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,integer,bigint,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_alignment_background_object(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,integer,bigint,uuid,uuid,text,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
