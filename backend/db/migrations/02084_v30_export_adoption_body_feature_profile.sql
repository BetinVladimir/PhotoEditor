-- +goose Up
-- Current execution locks for a claimed BodyFit pose/landmark feature. The caller
-- repeats exact original Params and producer-origin proof around this gate.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_body_feature_profile(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_child uuid,p_child_sha text,p_producer uuid,p_attempt integer,p_fence bigint,
 p_expected jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_child IS NULL OR p_producer IS NULL OR p_attempt<1 OR p_fence<1
  OR coalesce(p_child_sha,'') !~ '^[0-9a-f]{64}$'
  OR p_expected->>'Operation' NOT IN ('pose.detect','face.landmarks')
  OR p_expected->>'Version' IS DISTINCT FROM '1'
  OR p_expected->>'LicenseCleared' IS DISTINCT FROM 'true'
 THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_child,p_child_sha,CASE p_expected->>'Operation' WHEN 'pose.detect' THEN 'PoseSet' ELSE 'FaceLandmarks' END);
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL OR photo.v30_export_adoption_source_ready(
  p_tenant,w.project_id,w.input_snapshot_id,p_actor) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.tasks producer
  JOIN photo.artifacts output ON output.tenant_id=producer.tenant_id
   AND output.created_by_task_id=producer.id AND output.id=p_child
   AND output.sha256=p_child_sha
  JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id
   AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt
   AND attempt.fencing_token=producer.fencing_token AND attempt.finished_at IS NOT NULL
  JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
   AND result.id=producer.canonical_result_id AND result.task_id=producer.id
   AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
  WHERE producer.tenant_id=p_tenant AND producer.id=p_producer
   AND producer.attempt=p_attempt AND producer.fencing_token=p_fence
   AND producer.operation=p_expected->>'Operation'
   AND producer.operation_version=p_expected->>'Version'
   AND producer.state='succeeded' AND NOT producer.cancel_requested
   AND producer.resource_class='gpu' AND output.output_role=CASE p_expected->>'Operation' WHEN 'pose.detect' THEN 'pose_set' ELSE 'face_landmarks' END
   AND output.produced_attempt=p_attempt AND output.produced_fence=p_fence
   AND output.state='ready' AND output.verified_at IS NOT NULL
   AND output.object_version IS NOT NULL AND output.object_version NOT IN ('','null')
   AND (output.expires_at IS NULL OR output.expires_at>clock_timestamp())
   AND result.outputs @> jsonb_build_array(
    jsonb_build_object('artifact_id',p_child,'sha256',p_child_sha))
   AND attempt.backend=p_expected->>'Backend'
   AND producer.execution_profile->>'backend'=p_expected->>'Backend'
   AND attempt.profile->>'backend'=p_expected->>'Backend'
   AND producer.execution_profile->>'model_lock_sha'=p_expected->>'ModelLockSHA'
   AND attempt.profile->>'model_lock_sha'=p_expected->>'ModelLockSHA'
   AND producer.execution_profile->>'runtime_lock_sha'=p_expected->>'RuntimeLockSHA'
   AND attempt.profile->>'runtime_lock_sha'=p_expected->>'RuntimeLockSHA'
   AND producer.execution_profile->>'device_profile_sha'=p_expected->>'DeviceProfileSHA'
   AND attempt.profile->>'device_profile_sha'=p_expected->>'DeviceProfileSHA');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_body_feature_profile(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,integer,bigint,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_body_feature_profile(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,integer,bigint,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_body_feature_profile(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,integer,bigint,jsonb)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
