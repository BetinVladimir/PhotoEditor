-- +goose Up
-- Repeat V31 adoption authority after the parent expansion has closed.
-- Physical immutable bytes are still verified by the signed coordinator;
-- this private predicate checks current SQL identity and exact object versions.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_signed_current_receipts(p_tenant uuid,p_run uuid,p_task uuid,
 p_params uuid,p_sha text,p_json text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;closure jsonb;entry jsonb;actor uuid;project uuid;package uuid;
 policy uuid;policy_sha text;ordinal integer;preset uuid;export_id uuid;
 signing_key text;signing_workflow uuid;ready_count integer;seen integer:=0;
 prior_ordinal integer:=-1;prior_preset text:='';
BEGIN
 source:=photo.v30_delivery_signed_runtime_documents(p_tenant,p_run,p_task,p_params,p_sha,p_json);
 IF source IS NULL THEN RETURN false;END IF;
 closure:=(source->>'closure_jcs')::jsonb;actor:=(source->>'actor_id')::uuid;
 project:=(source->>'project_id')::uuid;package:=(source->>'package_id')::uuid;
 policy:=(closure->>'policy_id')::uuid;policy_sha:=closure->>'policy_sha';
 IF jsonb_typeof(closure->'entries') IS DISTINCT FROM 'array'
  OR jsonb_array_length(closure->'entries') NOT BETWEEN 1 AND 80000
  OR NOT EXISTS(SELECT 1 FROM photo.projects WHERE tenant_id=p_tenant AND id=project
   AND archived_at IS NULL AND download_allowed)
  OR photo.v31_signing_policy_ready(p_tenant,policy,
   photo.v30_uuid5(p_tenant,'signing:policy:'||policy::text||':'||policy_sha),policy_sha)
   IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT count(*) INTO ready_count FROM photo.delivery_items WHERE tenant_id=p_tenant
  AND package_id=package AND state='ready';
 IF ready_count<>jsonb_array_length(closure->'entries') OR EXISTS(SELECT 1 FROM photo.delivery_items
  WHERE tenant_id=p_tenant AND package_id=package AND state NOT IN('ready','omitted')) THEN RETURN false;END IF;
 FOR entry IN SELECT value FROM jsonb_array_elements(closure->'entries') LOOP
  IF jsonb_typeof(entry) IS DISTINCT FROM 'object'
   OR (SELECT count(*) FROM jsonb_object_keys(entry))<>10 THEN RETURN false;END IF;
  ordinal:=(entry->>'ordinal')::integer;preset:=(entry->>'preset_id')::uuid;
  export_id:=(entry->>'export_id')::uuid;signing_key:=entry->>'signing_key';
  IF ordinal NOT BETWEEN 0 AND 4999 OR
   (ordinal<prior_ordinal OR (ordinal=prior_ordinal AND preset::text<=prior_preset))
   OR signing_key IS DISTINCT FROM photo.v30_uuid5(p_run,ordinal::text||':'||preset::text)::text
   THEN RETURN false;END IF;
  prior_ordinal:=ordinal;prior_preset:=preset::text;
  signing_workflow:=photo.v30_uuid5(p_tenant,'provenance:'||actor::text||':'||signing_key);
  IF entry->>'task_id' IS DISTINCT FROM photo.v30_uuid5(signing_workflow,'provenance.sign:0')::text
   OR NOT EXISTS(SELECT 1 FROM photo.delivery_items i
    JOIN photo.exports e ON e.tenant_id=i.tenant_id AND e.id=i.export_id AND e.status='ready'
    JOIN photo.artifacts original ON original.tenant_id=e.tenant_id AND original.id=e.artifact_id
    JOIN photo.provenance_manifests pm ON pm.tenant_id=e.tenant_id AND pm.export_id=e.id
     AND pm.project_id=project AND pm.signing_policy_sha=policy_sha
    JOIN photo.v2_workflows sw ON sw.tenant_id=pm.tenant_id AND sw.id=pm.workflow_id
     AND sw.id=signing_workflow AND sw.workflow_type='provenance' AND sw.renderer_version='v2.2'
     AND sw.project_id=project AND sw.policy_sha=policy_sha
     AND sw.run_id=photo.v30_uuid5(signing_workflow,'run')
     AND sw.input_snapshot_id=photo.v30_uuid5(signing_workflow,'provenance:artifact-snapshot:1')
    JOIN photo.runs signer_run ON signer_run.tenant_id=sw.tenant_id AND signer_run.id=sw.run_id
     AND signer_run.kind='v2' AND signer_run.graph_sealed AND NOT signer_run.cancel_requested
     AND signer_run.input_snapshot_id=sw.input_snapshot_id AND signer_run.created_by=actor
     AND signer_run.config_snapshot->>'workflow_type'='provenance'
     AND signer_run.config_snapshot->>'project_id'=project::text
     AND signer_run.config_snapshot->>'export_id'=i.export_id::text
     AND signer_run.config_snapshot->>'snapshot_id'=sw.input_snapshot_id::text
    JOIN photo.tasks task ON task.tenant_id=pm.tenant_id AND task.id=pm.adopted_task_id
     AND task.run_id=sw.run_id AND task.node_key='provenance.sign:0'
     AND task.state='succeeded' AND NOT task.cancel_requested
     AND task.operation='provenance.sign' AND task.operation_version='1'
     AND task.canonical_result_id=pm.adopted_result_id
    JOIN photo.v2_task_inputs policy_input ON policy_input.tenant_id=task.tenant_id
     AND policy_input.task_id=task.id AND policy_input.contract='SigningPolicy'
     AND policy_input.artifact_id=photo.v30_uuid5(p_tenant,'signing:policy:'||policy::text||':'||policy_sha)
     AND policy_input.sha256=policy_sha
    JOIN photo.v2_task_inputs export_input ON export_input.tenant_id=task.tenant_id
     AND export_input.task_id=task.id AND export_input.contract='QualifiedExport'
     AND export_input.artifact_id=original.id AND export_input.sha256=original.sha256
    JOIN photo.task_results result ON result.tenant_id=task.tenant_id
     AND result.id=pm.adopted_result_id AND result.task_id=task.id
    JOIN photo.artifacts signed ON signed.tenant_id=pm.tenant_id AND signed.id=pm.signed_artifact_id
    JOIN photo.artifacts manifest ON manifest.tenant_id=pm.tenant_id AND manifest.id=pm.manifest_artifact_id
    WHERE i.tenant_id=p_tenant AND i.package_id=package AND i.ordinal=ordinal
     AND i.preset_id=preset AND i.export_id=export_id AND i.state='ready'
     AND original.id=(entry->'export'->>'artifact_id')::uuid
     AND original.sha256=entry->'export'->>'sha256' AND original.state='ready'
     AND original.verified_at IS NOT NULL AND original.media_type IN('image/jpeg','image/png')
     AND coalesce(original.object_version,'') NOT IN('','null')
     AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())
     AND pm.adopted_task_id=(entry->>'task_id')::uuid
     AND pm.adopted_result_id=(entry->>'result_id')::uuid
     AND pm.validation_sha=entry->>'validation_sha' AND pm.validation_sha ~ '^[0-9a-f]{64}$'
     AND signed.id=(entry->'signed'->>'artifact_id')::uuid
     AND signed.sha256=entry->'signed'->>'sha256' AND signed.state='ready'
     AND signed.verified_at IS NOT NULL AND signed.created_by_task_id=task.id
     AND signed.produced_attempt=result.attempt AND signed.produced_fence=result.fencing_token
     AND signed.output_role='signed_export' AND signed.media_type=original.media_type
     AND signed.bucket=original.bucket AND signed.bytes BETWEEN 1 AND 536870912
     AND signed.object_key='tenants/'||p_tenant||'/tasks/'||task.id||'/attempts/'||result.attempt||'/fences/'||result.fencing_token||'/'||signed.id
     AND coalesce(signed.object_version,'') NOT IN('','null')
     AND (signed.expires_at IS NULL OR signed.expires_at>clock_timestamp())
     AND manifest.id=(entry->'manifest'->>'artifact_id')::uuid
     AND manifest.sha256=entry->'manifest'->>'sha256' AND manifest.state='ready'
     AND manifest.verified_at IS NOT NULL AND manifest.created_by_task_id=task.id
     AND manifest.produced_attempt=result.attempt AND manifest.produced_fence=result.fencing_token
     AND manifest.output_role='manifest' AND manifest.media_type='application/c2pa'
     AND manifest.bucket=original.bucket AND manifest.bytes BETWEEN 1 AND 8388608
     AND manifest.object_key='tenants/'||p_tenant||'/tasks/'||task.id||'/attempts/'||result.attempt||'/fences/'||result.fencing_token||'/'||manifest.id
     AND coalesce(manifest.object_version,'') NOT IN('','null')
     AND (manifest.expires_at IS NULL OR manifest.expires_at>clock_timestamp())) THEN RETURN false;END IF;
  seen:=seen+1;
 END LOOP;
 RETURN seen=ready_count;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v30_delivery_signed_current_receipts(uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_signed_current_receipts(uuid,uuid,uuid,uuid,text,text)
 FROM PUBLIC,photo_api,photo_scheduler,photo_worker,photo_gateway,photo_portal,photo_maintenance;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
