-- +goose Up
-- Resolve the original mask producer and its three pinned documents for an
-- already qualified pair.register background input under the same claim.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_alignment_background_origin(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_alignment uuid,p_alignment_sha text,p_registration uuid,
 p_attempt integer,p_fence bigint,p_registration_params uuid,
 p_mask uuid,p_mask_sha text,p_image uuid,p_image_sha text,p_bucket text)
RETURNS TABLE(producer_task uuid,producer_operation text,producer_version text,
 producer_attempt integer,producer_fence bigint,
 root_id uuid,root_sha text,root_bucket text,root_key text,root_version text,
 root_bytes bigint,root_type text,
 params_id uuid,params_sha text,params_bucket text,params_key text,
 params_version text,params_bytes bigint,params_type text,
 report_id uuid,report_sha text,report_bucket text,report_key text,
 report_version text,report_bytes bigint,report_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_image IS NULL OR coalesce(p_image_sha,'') !~ '^[0-9a-f]{64}$'
 THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL OR photo.v30_export_adoption_source_ready(
  p_tenant,w.project_id,w.input_snapshot_id,p_actor) IS DISTINCT FROM true
 THEN RETURN;END IF;
 RETURN QUERY SELECT producer.id,producer.operation,producer.operation_version,
  producer.attempt,producer.fencing_token,
  root.id,root.sha256,root.bucket,root.object_key,root.object_version,
  root.bytes,root.media_type,
  params.id,params.sha256,params.bucket,params.object_key,
  params.object_version,params.bytes,params.media_type,
  report.id,report.sha256,report.bucket,report.object_key,
  report.object_version,report.bytes,report.media_type
 FROM photo.artifacts mask
 JOIN LATERAL photo.v30_export_adoption_alignment_background_object(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_alignment,p_alignment_sha,p_registration,p_attempt,p_fence,
  p_registration_params,p_mask,p_mask_sha,p_bucket) gate
  ON gate.bucket=mask.bucket AND gate.object_key=mask.object_key
  AND gate.object_version=mask.object_version AND gate.sha256=mask.sha256
  AND gate.bytes=mask.bytes AND gate.media_type=mask.media_type
 JOIN photo.tasks producer ON producer.tenant_id=mask.tenant_id
  AND producer.id=mask.created_by_task_id
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id
  AND params.id=producer.params_artifact_id
 JOIN photo.artifacts root ON root.tenant_id=producer.tenant_id
  AND root.created_by_task_id=producer.id
  AND root.output_role=CASE producer.operation
   WHEN 'mask.person' THEN 'person_mask_ref'
   WHEN 'mask.prompt' THEN 'prompt_mask_ref' END
 JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id
  AND report.created_by_task_id=producer.id
  AND report.output_role=CASE producer.operation
   WHEN 'mask.person' THEN 'person_report'
   WHEN 'mask.prompt' THEN 'prompt_mask_report' END
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
 WHERE mask.tenant_id=p_tenant AND mask.id=p_mask AND mask.sha256=p_mask_sha
  AND producer.operation IN ('mask.person','mask.prompt')
  AND producer.operation_version='1' AND producer.state='succeeded'
  AND NOT producer.cancel_requested AND NOT origin.cancel_requested
  AND mask.output_role=CASE producer.operation
   WHEN 'mask.person' THEN 'person_mask'
   WHEN 'mask.prompt' THEN 'prompt_mask' END
  AND mask.produced_attempt=producer.attempt
  AND mask.produced_fence=producer.fencing_token
  AND root.produced_attempt=producer.attempt
  AND root.produced_fence=producer.fencing_token
  AND report.produced_attempt=producer.attempt
  AND report.produced_fence=producer.fencing_token
  AND 1=(SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.created_by_task_id=producer.id AND a.output_role=root.output_role
   AND a.produced_attempt=producer.attempt AND a.produced_fence=producer.fencing_token)
  AND 1=(SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=p_tenant
   AND a.created_by_task_id=producer.id AND a.output_role=report.output_role
   AND a.produced_attempt=producer.attempt AND a.produced_fence=producer.fencing_token)
  AND attempt.backend=producer.execution_profile->>'backend'
  AND attempt.backend=attempt.profile->>'backend'
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',mask.id,'sha256',mask.sha256),
   jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),
   jsonb_build_object('artifact_id',report.id,'sha256',report.sha256))
  AND producer.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_image,'sha256',p_image_sha))
  AND (workflow.project_id=w.project_id
   OR origin.config_snapshot->>'project_id'=w.project_id::text)
  AND NOT EXISTS(SELECT 1 FROM photo.workflow_consent_links link
   LEFT JOIN photo.consent_grants grant_row ON grant_row.tenant_id=link.tenant_id
    AND grant_row.project_id=link.project_id AND grant_row.id=link.consent_id
   WHERE link.tenant_id=producer.tenant_id AND link.workflow_id=workflow.id
    AND (link.project_id IS DISTINCT FROM w.project_id OR grant_row.id IS NULL
     OR grant_row.status IS DISTINCT FROM 'granted'
     OR grant_row.epoch IS DISTINCT FROM link.consent_epoch))
  AND NOT EXISTS(SELECT 1 FROM (VALUES
   (root.state,root.verified_at,root.bucket,root.object_version,root.expires_at,root.media_type,root.bytes),
   (params.state,params.verified_at,params.bucket,params.object_version,params.expires_at,params.media_type,params.bytes),
   (report.state,report.verified_at,report.bucket,report.object_version,report.expires_at,report.media_type,report.bytes))
   checked(state,verified,bucket,version,expires,media_type,bytes)
   WHERE state IS DISTINCT FROM 'ready' OR verified IS NULL
    OR bucket IS DISTINCT FROM p_bucket OR version IS NULL OR version IN ('','null')
    OR expires<=clock_timestamp() OR media_type IS DISTINCT FROM 'application/json'
    OR bytes NOT BETWEEN 2 AND 1048576)
 FOR SHARE OF mask,producer,root,params,report;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_alignment_background_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,integer,bigint,uuid,uuid,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_alignment_background_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,integer,bigint,uuid,uuid,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_alignment_background_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,integer,bigint,uuid,uuid,text,uuid,text,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
