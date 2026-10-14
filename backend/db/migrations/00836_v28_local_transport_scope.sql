-- +goose Up
-- Restrict local portrait transport to the exact staged draft and live intent.
-- Preserve previous scope for other workflows, including semantic lease guard.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_local_transport_scope(p_tenant uuid,p_run uuid,p_task uuid,p_purpose text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT COALESCE(p_purpose IN('v2_input_chunk_0','v2_input_manifest') AND EXISTS(
  SELECT 1 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
  JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
  JOIN photo.backend_documents d ON d.tenant_id=t.tenant_id AND d.run_id=t.run_id AND d.owner_id=t.id AND d.purpose='task_params'
  JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
  WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=p_task
   AND r.kind='v2' AND r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested
   AND t.operation IN('portrait.hair','portrait.clothing') AND t.operation_version='1' AND t.resource_class='cpu'
   AND t.state='blocked' AND t.attempt=0 AND NOT t.cancel_requested
   AND w.workflow_type='preview' AND w.renderer_version='v2.2'
   AND r.v28_local_task_plan->'task'->>'id'=t.id::text
   AND r.v28_local_task_plan->'task'->>'node_key'=t.node_key
   AND r.v28_local_task_plan->'task'->>'operation'=t.operation
   AND r.v28_local_task_plan->'task'->>'params_artifact_id'=t.params_artifact_id::text
   AND r.v28_local_task_plan->'task'->>'params_sha'=d.sha256
   AND r.v28_local_task_plan->'task'->>'input_digest'=t.input_digest
   AND (t.execution_profile=r.v28_local_task_plan->'task'->'execution_profile' OR (photo.v2_input_closure_ready(t.id)
    AND (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=r.v28_local_task_plan->'task'->'execution_profile'))
   AND d.state='ready' AND d.artifact_id=t.params_artifact_id AND d.bytes=octet_length(r.v28_local_task_plan->>'params_jcs')
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=d.sha256 AND a.bytes=d.bytes
   AND a.object_version=d.object_version AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND photo.v28_local_params_document_scope(p_tenant,p_run,p_task,'task_params',d.sha256,d.bytes) IS TRUE
   AND photo.v28_local_inventory_digest_ready(t.id,t.params_artifact_id,d.sha256,t.operation,t.input_digest,r.v28_local_task_plan->'entries') IS TRUE),false);
$$;
ALTER FUNCTION photo.v28_local_transport_scope(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_transport_scope(uuid,uuid,uuid,text) FROM PUBLIC;
ALTER FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) RENAME TO v2_input_document_scope_pre_v28;
CREATE FUNCTION photo.v2_input_document_scope(p_tenant uuid,p_run uuid,p_task uuid,p_purpose text)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT CASE WHEN EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_run AND r.config_snapshot ? 'v28_local_portrait')
  OR EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=p_task AND t.operation IN('portrait.hair','portrait.clothing'))
  THEN photo.v28_local_transport_scope(p_tenant,p_run,p_task,p_purpose)
  ELSE photo.v2_input_document_scope_pre_v28(p_tenant,p_run,p_task,p_purpose) END;
$$;
ALTER FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) FROM PUBLIC;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
