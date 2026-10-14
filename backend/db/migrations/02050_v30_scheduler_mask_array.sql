-- +goose Up
-- Same-producer MaskRef/MaskPixels authority under an exact export claim.
-- Producers with multiple frozen-source inputs are refused until a stronger
-- per-mask source receipt is available.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_mask_array(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_parent uuid,p_parent_sha text,p_mask uuid,p_mask_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_parent IS NULL OR p_mask IS NULL OR p_parent=p_mask
  OR coalesce(p_parent_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_mask_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_parent,p_parent_sha,'MaskRef');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_mask,p_mask_sha,'MaskPixels');
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.artifacts root
 JOIN photo.artifacts pixels ON pixels.tenant_id=root.tenant_id AND pixels.id=p_mask
  AND pixels.sha256=p_mask_sha AND pixels.created_by_task_id=root.created_by_task_id
 JOIN photo.tasks producer ON producer.tenant_id=root.tenant_id
  AND producer.id=root.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
  AND result.id=producer.canonical_result_id AND result.task_id=producer.id
  AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id
  AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt
  AND attempt.fencing_token=producer.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.runs origin ON origin.tenant_id=producer.tenant_id AND origin.id=producer.run_id
 LEFT JOIN photo.v2_workflows producer_workflow ON producer_workflow.tenant_id=origin.tenant_id
  AND producer_workflow.run_id=origin.id
 WHERE root.tenant_id=p_tenant AND root.id=p_parent AND root.sha256=p_parent_sha
  AND root.output_role IN('person_mask_ref','prompt_mask_ref','skin_mask_ref',
   'mask_target_ref','mask_guard_ref','mask_protected_ref','transformed_mask_ref',
   'defect_protection_ref')
  AND pixels.output_role IN('person_mask','prompt_mask','skin_mask',
   'mask_pixels','mask_target','mask_protected')
  AND (producer_workflow.project_id=w.project_id
   OR origin.config_snapshot->>'project_id'=w.project_id::text)
  AND NOT origin.cancel_requested AND producer.state='succeeded'
  AND NOT producer.cancel_requested AND producer.operation_version='1'
  AND root.produced_attempt=producer.attempt AND root.produced_fence=producer.fencing_token
  AND pixels.produced_attempt=producer.attempt AND pixels.produced_fence=producer.fencing_token
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_parent,'sha256',p_parent_sha),
   jsonb_build_object('artifact_id',p_mask,'sha256',p_mask_sha))
  AND attempt.backend=producer.execution_profile->>'backend'
  AND attempt.backend=attempt.profile->>'backend'
  AND root.media_type='application/json'
  AND pixels.media_type IN('application/octet-stream','application/x-npy')
  AND root.state='ready' AND root.verified_at IS NOT NULL
  AND root.object_version IS NOT NULL AND root.object_version NOT IN('','null')
  AND (root.expires_at IS NULL OR root.expires_at>clock_timestamp())
  AND pixels.state='ready' AND pixels.verified_at IS NOT NULL
  AND pixels.object_version IS NOT NULL AND pixels.object_version NOT IN('','null')
  AND (pixels.expires_at IS NULL OR pixels.expires_at>clock_timestamp())
  AND (SELECT count(*) FROM jsonb_array_elements(producer.inputs) input(value)
   JOIN photo.artifacts candidate ON candidate.tenant_id=p_tenant
    AND candidate.id=(input.value->>'artifact_id')::uuid
    AND candidate.sha256=input.value->>'sha256' AND candidate.output_role='working_image')=1
  AND (SELECT count(*) FROM photo.snapshot_items item
   JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
    AND version.id=item.asset_version_id AND version.master_artifact_id=item.artifact_id
    AND version.content_hash=item.sha256 AND version.review_state='accepted'
   JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
    AND asset.project_id=w.project_id AND asset.deleted_at IS NULL AND asset.upload_state='verified'
   WHERE item.tenant_id=p_tenant AND item.snapshot_id=w.input_snapshot_id
    AND producer.inputs @> jsonb_build_array(
     jsonb_build_object('artifact_id',item.artifact_id,'sha256',item.sha256)))=1);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_mask_array(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_mask_array(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_mask_array(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
