-- +goose Up
-- An adopted V31 row is useful to Delivery only when its exact original
-- export, policy and canonical task/result still match retained artifacts.
-- This API-only read does not itself replace physical signed-byte verification.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signed_receipt(p_tenant uuid,p_actor uuid,p_session uuid,p_project uuid,
 p_export uuid,p_original uuid,p_original_sha text,p_policy uuid,p_policy_sha text)
RETURNS TABLE(signed_id uuid,signed_sha text,manifest_id uuid,manifest_sha text,
 validation_sha text,adopted_task uuid,adopted_result uuid,
 bucket text,signed_key text,signed_version text,signed_bytes bigint,signed_media text,
 manifest_key text,manifest_version text,manifest_bytes bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
  OR p_project IS NULL OR p_export IS NULL OR p_original IS NULL OR p_policy IS NULL
  OR p_original_sha !~ '^[0-9a-f]{64}$' OR p_policy_sha !~ '^[0-9a-f]{64}$'
  OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS DISTINCT FROM true
  OR NOT EXISTS(SELECT 1 FROM photo.tenants tenant WHERE tenant.id=p_tenant AND tenant.status='active')
  OR NOT EXISTS(SELECT 1 FROM photo.projects project WHERE project.tenant_id=p_tenant AND project.id=p_project
   AND project.archived_at IS NULL AND project.download_allowed)
  OR photo.v31_signing_policy_ready(p_tenant,p_policy,
   photo.v30_uuid5(p_tenant,'signing:policy:'||p_policy::text||':'||p_policy_sha),p_policy_sha) IS DISTINCT FROM true
 THEN RETURN;END IF;
 RETURN QUERY
 SELECT signed.id,signed.sha256,manifest.id,manifest.sha256,pm.validation_sha,pm.adopted_task_id,pm.adopted_result_id,
  signed.bucket,signed.object_key,signed.object_version,signed.bytes,signed.media_type,
  manifest.object_key,manifest.object_version,manifest.bytes
 FROM photo.provenance_manifests pm
 JOIN photo.exports e ON e.tenant_id=pm.tenant_id AND e.id=pm.export_id AND e.status='ready'
 JOIN photo.asset_versions version ON version.tenant_id=e.tenant_id AND version.id=e.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id AND asset.project_id=p_project
 JOIN photo.artifacts original ON original.tenant_id=e.tenant_id AND original.id=e.artifact_id
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=pm.tenant_id AND workflow.id=pm.workflow_id
  AND workflow.workflow_type='provenance' AND workflow.renderer_version='v2.2'
  AND workflow.project_id=p_project AND workflow.policy_sha=p_policy_sha
  AND workflow.run_id=photo.v30_uuid5(workflow.id,'run')
  AND workflow.input_snapshot_id=photo.v30_uuid5(workflow.id,'provenance:artifact-snapshot:1')
 JOIN photo.runs signing_run ON signing_run.tenant_id=workflow.tenant_id AND signing_run.id=workflow.run_id
  AND signing_run.kind='v2' AND signing_run.graph_sealed AND NOT signing_run.cancel_requested
  AND signing_run.input_snapshot_id=workflow.input_snapshot_id AND signing_run.created_by=p_actor
  AND signing_run.config_snapshot->>'workflow_type'='provenance'
  AND signing_run.config_snapshot->>'project_id'=p_project::text
  AND signing_run.config_snapshot->>'export_id'=p_export::text
  AND signing_run.config_snapshot->>'snapshot_id'=workflow.input_snapshot_id::text
 JOIN photo.tasks task ON task.tenant_id=pm.tenant_id AND task.id=pm.adopted_task_id
  AND task.run_id=workflow.run_id AND task.id=photo.v30_uuid5(workflow.id,'provenance.sign:0')
  AND task.node_key='provenance.sign:0' AND task.state='succeeded' AND task.operation='provenance.sign'
  AND task.operation_version='1' AND task.canonical_result_id=pm.adopted_result_id
 JOIN photo.v2_task_inputs policy_input ON policy_input.tenant_id=task.tenant_id
  AND policy_input.task_id=task.id AND policy_input.contract='SigningPolicy'
  AND policy_input.artifact_id=photo.v30_uuid5(p_tenant,'signing:policy:'||p_policy::text||':'||p_policy_sha)
  AND policy_input.sha256=p_policy_sha
 JOIN photo.v2_task_inputs export_input ON export_input.tenant_id=task.tenant_id
  AND export_input.task_id=task.id AND export_input.contract='QualifiedExport'
  AND export_input.artifact_id=original.id AND export_input.sha256=original.sha256
 JOIN photo.task_results result ON result.tenant_id=task.tenant_id AND result.id=pm.adopted_result_id AND result.task_id=task.id
 JOIN photo.artifacts signed ON signed.tenant_id=pm.tenant_id AND signed.id=pm.signed_artifact_id
 JOIN photo.artifacts manifest ON manifest.tenant_id=pm.tenant_id AND manifest.id=pm.manifest_artifact_id
 WHERE pm.tenant_id=p_tenant AND pm.project_id=p_project AND pm.export_id=p_export
  AND pm.signing_policy_sha=p_policy_sha AND pm.adopted_task_id IS NOT NULL
  AND pm.adopted_result_id IS NOT NULL AND pm.validation_sha ~ '^[0-9a-f]{64}$'
  AND original.id=p_original AND original.sha256=p_original_sha
  AND original.state='ready' AND original.verified_at IS NOT NULL
  AND original.media_type IN('image/jpeg','image/png')
  AND signed.state='ready' AND signed.verified_at IS NOT NULL AND signed.created_by_task_id=task.id
  AND signed.produced_attempt=result.attempt AND signed.produced_fence=result.fencing_token
  AND signed.output_role='signed_export' AND signed.media_type=original.media_type
  AND signed.object_key='tenants/'||p_tenant||'/tasks/'||task.id||'/attempts/'||result.attempt||'/fences/'||result.fencing_token||'/'||signed.id
  AND signed.sha256 ~ '^[0-9a-f]{64}$' AND signed.bytes BETWEEN 1 AND 536870912
  AND manifest.state='ready' AND manifest.verified_at IS NOT NULL AND manifest.created_by_task_id=task.id
  AND manifest.produced_attempt=result.attempt AND manifest.produced_fence=result.fencing_token
  AND manifest.output_role='manifest' AND manifest.media_type='application/c2pa'
  AND manifest.object_key='tenants/'||p_tenant||'/tasks/'||task.id||'/attempts/'||result.attempt||'/fences/'||result.fencing_token||'/'||manifest.id
  AND manifest.sha256 ~ '^[0-9a-f]{64}$' AND manifest.bytes BETWEEN 1 AND 8388608
  AND signed.bucket=original.bucket AND manifest.bucket=original.bucket
  AND coalesce(original.object_version,'') NOT IN('','null')
  AND coalesce(signed.object_version,'') NOT IN('','null')
  AND coalesce(manifest.object_version,'') NOT IN('','null')
  AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())
  AND (signed.expires_at IS NULL OR signed.expires_at>clock_timestamp())
  AND (manifest.expires_at IS NULL OR manifest.expires_at>clock_timestamp())
 FOR SHARE OF e,version,asset,original,workflow,signing_run,signed,manifest;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signed_receipt(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signed_receipt(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_signed_receipt(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
