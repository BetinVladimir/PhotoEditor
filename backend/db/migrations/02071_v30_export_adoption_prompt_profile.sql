-- +goose Up
-- Recheck the exact mask.prompt producer, execution locks and live consent
-- after qualification report IO, without borrowing a user session.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_prompt_profile(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_producer uuid,p_attempt integer,p_fence bigint,
 p_root uuid,p_root_sha text,p_report uuid,p_report_sha text,
 p_params uuid,p_params_sha text,p_params_key text,p_params_version text,p_params_bytes bigint,
 p_mask_key text,p_mask_version text,p_mask_bytes bigint,p_mask_type text,
 p_source uuid,p_source_sha text,p_version uuid,p_expected jsonb,p_bucket text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_producer IS NULL OR p_attempt<1 OR p_fence<1
  OR p_root IS NULL OR p_report IS NULL OR p_params IS NULL OR p_source IS NULL
  OR p_version IS NULL OR coalesce(p_root_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_report_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_params_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_source_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_bucket,'')='' OR coalesce(p_params_key,'')=''
  OR coalesce(p_params_version,'') IN ('','null')
  OR coalesce(p_mask_key,'')='' OR coalesce(p_mask_version,'') IN ('','null')
  OR p_mask_bytes<1 OR p_params_bytes<2
  OR p_expected->>'Operation' IS DISTINCT FROM 'mask.prompt'
  OR p_expected->>'Version' IS DISTINCT FROM '1'
  OR p_expected->>'LicenseCleared' IS DISTINCT FROM 'true'
 THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL OR photo.v30_export_adoption_source_ready(
  p_tenant,w.project_id,w.input_snapshot_id,p_actor) IS DISTINCT FROM true
 THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.tasks producer
  JOIN photo.artifacts root ON root.tenant_id=producer.tenant_id
   AND root.created_by_task_id=producer.id AND root.id=p_root AND root.sha256=p_root_sha
  JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id
   AND report.created_by_task_id=producer.id AND report.id=p_report
   AND report.sha256=p_report_sha
  JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id
   AND params.id=producer.params_artifact_id
  JOIN photo.artifacts mask ON mask.tenant_id=producer.tenant_id
   AND mask.created_by_task_id=producer.id AND mask.output_role='prompt_mask'
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
  JOIN photo.asset_versions version ON version.tenant_id=producer.tenant_id
   AND version.id=p_version AND version.master_artifact_id=p_source
   AND version.content_hash=p_source_sha AND version.review_state='accepted'
  JOIN photo.assets asset ON asset.tenant_id=version.tenant_id
   AND asset.id=version.asset_id AND asset.project_id=w.project_id
   AND asset.deleted_at IS NULL AND asset.upload_state='verified'
  JOIN photo.snapshot_items item ON item.tenant_id=version.tenant_id
   AND item.snapshot_id=w.input_snapshot_id AND item.asset_version_id=version.id
   AND item.artifact_id=p_source AND item.sha256=p_source_sha
  WHERE producer.tenant_id=p_tenant AND producer.id=p_producer
   AND producer.attempt=p_attempt AND producer.fencing_token=p_fence
   AND producer.operation='mask.prompt' AND producer.operation_version='1'
   AND producer.state='succeeded' AND NOT producer.cancel_requested
   AND producer.resource_class='gpu' AND NOT origin.cancel_requested
   AND (workflow.project_id=w.project_id
    OR origin.config_snapshot->>'project_id'=w.project_id::text)
   AND root.output_role='prompt_mask_ref' AND report.output_role='prompt_mask_report'
   AND root.produced_attempt=p_attempt AND root.produced_fence=p_fence
   AND report.produced_attempt=p_attempt AND report.produced_fence=p_fence
   AND mask.produced_attempt=p_attempt AND mask.produced_fence=p_fence
   AND params.id=p_params AND params.sha256=p_params_sha
   AND params.bucket=p_bucket AND params.object_key=p_params_key
   AND params.object_version=p_params_version AND params.bytes=p_params_bytes
   AND mask.bucket=p_bucket AND mask.object_key=p_mask_key
   AND mask.object_version=p_mask_version AND mask.bytes=p_mask_bytes
   AND mask.media_type=p_mask_type
   AND producer.inputs @> jsonb_build_array(
    jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha))
   AND result.outputs @> jsonb_build_array(
    jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),
    jsonb_build_object('artifact_id',report.id,'sha256',report.sha256),
    jsonb_build_object('artifact_id',mask.id,'sha256',mask.sha256))
   AND attempt.backend=p_expected->>'Backend'
   AND producer.execution_profile->>'backend'=p_expected->>'Backend'
   AND attempt.profile->>'backend'=p_expected->>'Backend'
   AND producer.execution_profile->>'model_lock_sha'=p_expected->>'ModelLockSHA'
   AND attempt.profile->>'model_lock_sha'=p_expected->>'ModelLockSHA'
   AND producer.execution_profile->>'runtime_lock_sha'=p_expected->>'RuntimeLockSHA'
   AND attempt.profile->>'runtime_lock_sha'=p_expected->>'RuntimeLockSHA'
   AND producer.execution_profile->>'device_profile_sha'=p_expected->>'DeviceProfileSHA'
   AND attempt.profile->>'device_profile_sha'=p_expected->>'DeviceProfileSHA'
   AND NOT EXISTS(SELECT 1 FROM photo.workflow_consent_links consent_link
    LEFT JOIN photo.consent_grants consent_grant
     ON consent_grant.tenant_id=consent_link.tenant_id
     AND consent_grant.project_id=consent_link.project_id
     AND consent_grant.id=consent_link.consent_id
    WHERE consent_link.tenant_id=producer.tenant_id
     AND consent_link.workflow_id=workflow.id
     AND (consent_link.project_id IS DISTINCT FROM workflow.project_id
      OR consent_grant.id IS NULL OR consent_grant.status IS DISTINCT FROM 'granted'
      OR consent_grant.epoch IS DISTINCT FROM consent_link.consent_epoch))
   AND NOT EXISTS(SELECT 1 FROM (VALUES(root.id),(report.id),(params.id),(mask.id)) required(id)
    JOIN photo.artifacts artifact ON artifact.tenant_id=p_tenant AND artifact.id=required.id
    WHERE artifact.state IS DISTINCT FROM 'ready' OR artifact.verified_at IS NULL
     OR artifact.object_version IS NULL OR artifact.object_version IN ('','null')
     OR artifact.expires_at<=clock_timestamp()));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_prompt_profile(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,uuid,text,uuid,text,text,text,bigint,text,text,bigint,text,uuid,text,uuid,jsonb,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_prompt_profile(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,uuid,text,uuid,text,text,text,bigint,text,text,bigint,text,uuid,text,uuid,jsonb,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_prompt_profile(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,uuid,text,uuid,text,text,text,bigint,text,text,bigint,text,uuid,text,uuid,jsonb,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
