-- +goose Up
-- Claim-scoped Params producer lookup with current consent dependencies and
-- unique task/attempt/fence. Native OperationParams checks the original bytes.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_operation_params(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_params uuid,p_params_sha text,p_operation text,p_image uuid,p_image_sha text,
 p_bucket text,p_params_key text,p_params_version text,p_params_bytes bigint,p_required jsonb)
RETURNS TABLE(producer_task uuid,producer_operation text,producer_version text,
 producer_attempt integer,producer_fence bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_params IS NULL OR p_image IS NULL OR p_params=p_image
  OR coalesce(p_params_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_image_sha,'') !~ '^[0-9a-f]{64}$'
  OR p_operation NOT IN('image.denoise','image.upscale','portrait.hair',
   'portrait.clothing','image.local','geometry.body','portrait.detail',
   'defects.repair','color.stats')
  OR coalesce(p_bucket,'')='' OR coalesce(p_params_key,'')=''
  OR coalesce(p_params_version,'') IN ('','null')
  OR p_params_bytes<2 OR p_params_bytes>1048576
  OR jsonb_typeof(p_required) IS DISTINCT FROM 'array'
  OR (p_operation='geometry.body' AND jsonb_array_length(p_required) NOT BETWEEN 3 AND 5)
 THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_params,p_params_sha,'Params');
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_image,p_image_sha,'WorkingImage');
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN;END IF;
 RETURN QUERY SELECT producer.id,producer.operation,producer.operation_version,
  producer.attempt,producer.fencing_token
 FROM photo.tasks producer
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id
  AND params.id=producer.params_artifact_id
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
 JOIN photo.snapshot_items item ON item.tenant_id=producer.tenant_id
  AND item.snapshot_id=w.input_snapshot_id AND item.artifact_id=p_image
  AND item.sha256=p_image_sha
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
  AND version.id=item.asset_version_id AND version.master_artifact_id=p_image
  AND version.content_hash=p_image_sha AND version.review_state='accepted'
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id
  AND asset.id=version.asset_id AND asset.project_id=w.project_id
  AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 WHERE producer.tenant_id=p_tenant AND params.id=p_params
  AND params.sha256=p_params_sha AND producer.operation=p_operation
  AND producer.operation_version='1' AND producer.state='succeeded'
  AND NOT producer.cancel_requested AND NOT origin.cancel_requested
  AND attempt.backend=producer.execution_profile->>'backend'
  AND attempt.backend=attempt.profile->>'backend'
  AND (workflow.project_id=w.project_id
   OR origin.config_snapshot->>'project_id'=w.project_id::text)
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
  AND producer.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_image,'sha256',p_image_sha))
  AND (p_operation<>'geometry.body' OR producer.inputs @> p_required)
  AND params.bucket=p_bucket AND params.object_key=p_params_key
  AND params.object_version=p_params_version AND params.bytes=p_params_bytes
  AND params.media_type='application/json' AND params.state='ready'
  AND params.verified_at IS NOT NULL
  AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
 ORDER BY producer.id LIMIT 2;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_operation_params(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,uuid,text,text,text,text,bigint,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_operation_params(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,uuid,text,text,text,text,bigint,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_operation_params(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,uuid,text,text,text,text,bigint,jsonb)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
