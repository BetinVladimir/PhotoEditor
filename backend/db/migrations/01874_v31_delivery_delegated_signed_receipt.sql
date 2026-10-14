-- +goose Up
-- Scheduler-only adopted signer receipt. The parent lease replaces the API
-- editor session; physical signed bytes remain a separate required check.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_delegated_signed_receipt(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid)
RETURNS TABLE(signed_id uuid,signed_sha text,manifest_id uuid,manifest_sha text,
 validation_sha text,adopted_task uuid,adopted_result uuid,
 bucket text,signed_key text,signed_version text,signed_bytes bigint,
 signed_media text,manifest_key text,manifest_version text,manifest_bytes bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE x photo.graph_expansions%ROWTYPE;parent photo.runs%ROWTYPE;
 package photo.delivery_packages%ROWTYPE;item photo.delivery_items%ROWTYPE;
 frozen jsonb;policy uuid;policy_sha text;status text;observed_export uuid;
 observed_task uuid;child_key text;wid uuid;rid uuid;tid uuid;
BEGIN
 SELECT signing_status,export_id,task_id INTO status,observed_export,observed_task
 FROM photo.read_v30_delivery_signer_status(
  p_expansion,p_token,p_ordinal,p_preset);
 IF status IS DISTINCT FROM 'adopted' THEN RETURN;END IF;
 SELECT * INTO x FROM photo.graph_expansions WHERE id=p_expansion
  AND kind='v2.delivery/1' AND state='open' AND expansion_token=p_token
  AND expansion_lease_until>clock_timestamp() FOR SHARE;
 IF NOT FOUND OR x.cursor IS NULL OR x.cursor !~ '^[0-9]+$' THEN RETURN;END IF;
 IF x.cursor::bigint<>x.expected_count THEN RETURN;END IF;
 SELECT * INTO parent FROM photo.runs WHERE tenant_id=x.tenant_id
  AND id=x.run_id AND kind='v2' AND status='running'
  AND NOT cancel_requested AND created_by IS NOT NULL FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO package FROM photo.delivery_packages WHERE tenant_id=x.tenant_id
  AND workflow_id=photo.v30_uuid5(x.run_id,'workflow')
  AND state='building' FOR SHARE;
 IF NOT FOUND OR NOT EXISTS(SELECT 1 FROM photo.projects p
  WHERE p.tenant_id=x.tenant_id AND p.id=package.project_id
   AND p.archived_at IS NULL AND p.download_allowed) THEN RETURN;END IF;
 SELECT * INTO item FROM photo.delivery_items WHERE tenant_id=x.tenant_id
  AND package_id=package.id AND project_id=package.project_id
  AND ordinal=p_ordinal AND preset_id=p_preset AND state='ready'
  AND export_id=observed_export FOR SHARE;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT (b.v30_delivery_proof->>'frozen_input_jcs')::jsonb INTO frozen
 FROM photo.backend_documents b WHERE b.tenant_id=x.tenant_id
  AND b.run_id=x.run_id AND b.owner_id=x.parent_task_id
  AND b.purpose='task_params' AND b.state='ready'
  AND b.v30_delivery_proof IS NOT NULL FOR SHARE;
 IF frozen IS NULL OR frozen->'policy'->>'provenance' IS DISTINCT FROM 'required'
  OR frozen->'policy'->>'signing_policy_sha' !~ '^[0-9a-f]{64}$'
  THEN RETURN;END IF;
 policy:=(frozen->'policy'->>'signing_policy_id')::uuid;
 policy_sha:=frozen->'policy'->>'signing_policy_sha';
 IF policy IS NULL OR photo.v31_signing_policy_ready(x.tenant_id,policy,
  photo.v30_uuid5(x.tenant_id,
   'signing:policy:'||policy::text||':'||policy_sha),policy_sha)
  IS DISTINCT FROM true THEN RETURN;END IF;
 child_key:=photo.v30_uuid5(x.run_id,
  p_ordinal::text||':'||p_preset::text)::text;
 wid:=photo.v30_uuid5(x.tenant_id,
  'provenance:'||parent.created_by::text||':'||child_key);
 rid:=photo.v30_uuid5(wid,'run');
 tid:=photo.v30_uuid5(wid,'provenance.sign:0');
 IF observed_task IS DISTINCT FROM tid THEN RETURN;END IF;
 RETURN QUERY SELECT signed.id,signed.sha256,manifest.id,manifest.sha256,
  pm.validation_sha,pm.adopted_task_id,pm.adopted_result_id,
  signed.bucket,signed.object_key,signed.object_version,signed.bytes,
  signed.media_type,manifest.object_key,manifest.object_version,manifest.bytes
 FROM photo.provenance_manifests pm
 JOIN photo.exports e ON e.tenant_id=pm.tenant_id AND e.id=pm.export_id
  AND e.status='ready' AND e.id=item.export_id AND e.project_id=package.project_id
 JOIN photo.artifacts original ON original.tenant_id=e.tenant_id
  AND original.id=e.artifact_id AND original.state='ready'
  AND original.verified_at IS NOT NULL
 JOIN photo.v2_workflows w ON w.tenant_id=pm.tenant_id AND w.id=pm.workflow_id
  AND w.id=wid AND w.run_id=rid AND w.project_id=package.project_id
  AND w.workflow_type='provenance' AND w.renderer_version='v2.2'
  AND w.policy_sha=policy_sha
 JOIN photo.runs sr ON sr.tenant_id=w.tenant_id AND sr.id=w.run_id
  AND sr.kind='v2' AND sr.graph_sealed AND NOT sr.cancel_requested
  AND sr.created_by=parent.created_by
  AND sr.input_snapshot_id=w.input_snapshot_id
  AND sr.config_snapshot->>'workflow_type'='provenance'
  AND sr.config_snapshot->>'project_id'=package.project_id::text
  AND sr.config_snapshot->>'export_id'=item.export_id::text
  AND sr.config_snapshot->>'snapshot_id'=w.input_snapshot_id::text
 JOIN photo.tasks task ON task.tenant_id=pm.tenant_id
  AND task.id=pm.adopted_task_id AND task.id=tid AND task.run_id=rid
  AND task.node_key='provenance.sign:0' AND task.operation='provenance.sign'
  AND task.operation_version='1' AND task.state='succeeded'
  AND task.canonical_result_id=pm.adopted_result_id
 JOIN photo.task_results result ON result.tenant_id=task.tenant_id
  AND result.id=pm.adopted_result_id AND result.task_id=task.id
 JOIN photo.v2_task_inputs policy_input ON policy_input.tenant_id=task.tenant_id
  AND policy_input.task_id=task.id AND policy_input.contract='SigningPolicy'
  AND policy_input.artifact_id=photo.v30_uuid5(x.tenant_id,
   'signing:policy:'||policy::text||':'||policy_sha)
  AND policy_input.sha256=policy_sha
 JOIN photo.v2_task_inputs export_input ON export_input.tenant_id=task.tenant_id
  AND export_input.task_id=task.id AND export_input.contract='QualifiedExport'
  AND export_input.artifact_id=original.id
  AND export_input.sha256=original.sha256
 JOIN photo.artifacts signed ON signed.tenant_id=pm.tenant_id
  AND signed.id=pm.signed_artifact_id
 JOIN photo.artifacts manifest ON manifest.tenant_id=pm.tenant_id
  AND manifest.id=pm.manifest_artifact_id
 WHERE pm.tenant_id=x.tenant_id AND pm.project_id=package.project_id
  AND pm.export_id=item.export_id AND pm.workflow_id=wid
  AND pm.signing_policy_sha=policy_sha
  AND pm.adopted_task_id=tid AND pm.validation_sha ~ '^[0-9a-f]{64}$'
  AND original.media_type IN('image/jpeg','image/png')
  AND original.object_version NOT IN('','null')
  AND signed.state='ready' AND signed.verified_at IS NOT NULL
  AND signed.created_by_task_id=tid
  AND signed.produced_attempt=result.attempt
  AND signed.produced_fence=result.fencing_token
  AND signed.output_role='signed_export'
  AND signed.media_type=original.media_type
  AND signed.object_key='tenants/'||x.tenant_id||'/tasks/'||tid||
   '/attempts/'||result.attempt||'/fences/'||result.fencing_token||'/'||signed.id
  AND signed.sha256 ~ '^[0-9a-f]{64}$'
  AND signed.bytes BETWEEN 1 AND 536870912
  AND manifest.state='ready' AND manifest.verified_at IS NOT NULL
  AND manifest.created_by_task_id=tid
  AND manifest.produced_attempt=result.attempt
  AND manifest.produced_fence=result.fencing_token
  AND manifest.output_role='manifest'
  AND manifest.media_type='application/c2pa'
  AND manifest.object_key='tenants/'||x.tenant_id||'/tasks/'||tid||
   '/attempts/'||result.attempt||'/fences/'||result.fencing_token||'/'||manifest.id
  AND manifest.sha256 ~ '^[0-9a-f]{64}$'
  AND manifest.bytes BETWEEN 1 AND 8388608
  AND signed.bucket=original.bucket AND manifest.bucket=original.bucket
  AND signed.object_version NOT IN('','null')
  AND manifest.object_version NOT IN('','null')
  AND (original.expires_at IS NULL OR original.expires_at>clock_timestamp())
  AND (signed.expires_at IS NULL OR signed.expires_at>clock_timestamp())
  AND (manifest.expires_at IS NULL OR manifest.expires_at>clock_timestamp())
 FOR SHARE OF e,original,w,sr,signed,manifest;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range
 THEN RETURN;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_delegated_signed_receipt(uuid,uuid,integer,uuid)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_delegated_signed_receipt(uuid,uuid,integer,uuid)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_delegated_signed_receipt(uuid,uuid,integer,uuid)
 TO photo_scheduler;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
