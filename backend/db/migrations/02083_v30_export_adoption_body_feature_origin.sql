-- +goose Up
-- Original pose.detect/face.landmarks Params for a BodyFit feature edge.
-- Native feature semantics and original detector chain remain separate gates.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_body_feature_origin(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_parent uuid,p_parent_sha text,p_fit uuid,p_fit_sha text,
 p_child uuid,p_child_sha text,p_operation text,p_role text,
 p_image uuid,p_image_sha text,p_bucket text,p_output_key text,
 p_output_version text,p_output_bytes bigint)
RETURNS TABLE(producer_task uuid,producer_operation text,producer_version text,
 producer_attempt integer,producer_fence bigint,
 params_id uuid,params_sha text,params_bucket text,params_key text,
 params_version text,params_bytes bigint,params_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_parent IS NULL OR p_fit IS NULL OR p_child IS NULL OR p_image IS NULL
  OR coalesce(p_parent_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_fit_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_child_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_image_sha,'') !~ '^[0-9a-f]{64}$'
  OR (p_operation,p_role) NOT IN (
   ('pose.detect','pose_set'),('face.landmarks','face_landmarks'))
  OR coalesce(p_bucket,'')='' OR coalesce(p_output_key,'')=''
  OR coalesce(p_output_version,'') IN ('','null')
  OR p_output_bytes NOT BETWEEN 2 AND 2097152
 THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_parent,p_parent_sha,'Params');
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_fit,p_fit_sha,'BodyFit');
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_child,p_child_sha,CASE p_operation WHEN 'pose.detect' THEN 'PoseSet'
   ELSE 'FaceLandmarks' END);
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL OR photo.v30_export_adoption_source_ready(
  p_tenant,w.project_id,w.input_snapshot_id,p_actor) IS DISTINCT FROM true
 THEN RETURN;END IF;
 RETURN QUERY SELECT producer.id,producer.operation,producer.operation_version,
  producer.attempt,producer.fencing_token,
  original.id,original.sha256,original.bucket,original.object_key,
  original.object_version,original.bytes,original.media_type
 FROM photo.artifacts output
 JOIN photo.tasks producer ON producer.tenant_id=output.tenant_id
  AND producer.id=output.created_by_task_id
 JOIN photo.artifacts original ON original.tenant_id=producer.tenant_id
  AND original.id=producer.params_artifact_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
  AND result.id=producer.canonical_result_id AND result.task_id=producer.id
  AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id
  AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt
  AND attempt.fencing_token=producer.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.runs origin ON origin.tenant_id=producer.tenant_id
  AND origin.id=producer.run_id
 LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=origin.tenant_id
  AND workflow.run_id=origin.id
 WHERE output.tenant_id=p_tenant AND output.id=p_child
  AND output.sha256=p_child_sha AND output.bucket=p_bucket
  AND output.object_key=p_output_key AND output.object_version=p_output_version
  AND output.bytes=p_output_bytes AND output.media_type='application/json'
  AND output.output_role=p_role AND output.state='ready'
  AND output.verified_at IS NOT NULL
  AND (output.expires_at IS NULL OR output.expires_at>clock_timestamp())
  AND output.produced_attempt=producer.attempt
  AND output.produced_fence=producer.fencing_token
  AND producer.operation=p_operation AND producer.operation_version='1'
  AND producer.state='succeeded' AND NOT producer.cancel_requested
  AND producer.resource_class='gpu' AND NOT origin.cancel_requested
  AND (workflow.project_id=w.project_id
   OR origin.config_snapshot->>'project_id'=w.project_id::text)
  AND producer.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_image,'sha256',p_image_sha))
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_child,'sha256',p_child_sha))
  AND attempt.backend=producer.execution_profile->>'backend'
  AND attempt.backend=attempt.profile->>'backend'
  AND original.id<>output.id AND original.id<>p_parent
  AND original.id<>p_fit AND original.bucket=p_bucket
  AND original.media_type='application/json'
  AND original.bytes BETWEEN 2 AND 1048576
  AND original.state='ready' AND original.verified_at IS NOT NULL
  AND original.object_version IS NOT NULL
  AND original.object_version NOT IN ('','null')
  AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())
  AND NOT EXISTS(SELECT 1 FROM photo.workflow_consent_links link
   LEFT JOIN photo.consent_grants grant_row ON grant_row.tenant_id=link.tenant_id
    AND grant_row.project_id=link.project_id AND grant_row.id=link.consent_id
   WHERE link.tenant_id=producer.tenant_id AND link.workflow_id=workflow.id
    AND (link.project_id IS DISTINCT FROM w.project_id OR grant_row.id IS NULL
     OR grant_row.status IS DISTINCT FROM 'granted'
     OR grant_row.epoch IS DISTINCT FROM link.consent_epoch))
 FOR SHARE OF output,producer,original;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_body_feature_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,text,text,uuid,text,text,text,text,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_body_feature_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,text,text,uuid,text,text,text,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_body_feature_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,text,uuid,text,text,text,text,bigint)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
