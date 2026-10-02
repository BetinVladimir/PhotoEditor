-- +goose Up
-- Require live render root/session and frozen task pins for transport publication.
-- Preserve the prior composition and other-operation scope.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v2_input_document_scope(p_tenant uuid,p_run uuid,p_task uuid,p_purpose text)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE scope record; reserved bigint; node jsonb; version_id uuid;
BEGIN
 IF p_purpose IS NULL OR NOT (p_purpose='v2_input_manifest' OR
  (p_purpose ~ '^v2_input_chunk_([0-9]|[1-9][0-9]{1,3})$'
   AND substring(p_purpose from 16)::integer BETWEEN 0 AND 1953)) THEN RETURN false;END IF;
 SELECT t.operation,t.operation_version,t.resource_class,t.state,t.attempt,t.node_key,
  t.params_artifact_id,t.input_digest,t.execution_profile,r.status,r.graph_sealed,
  r.input_snapshot_id,r.created_by,r.config_snapshot,w.id AS workflow_id,w.project_id
 INTO scope FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 WHERE t.tenant_id=p_tenant AND t.run_id=p_run AND t.id=p_task
  AND t.state IN('blocked','ready') AND NOT t.cancel_requested
  AND r.kind='v2' AND r.status IN('draft','queued','running','waiting_review')
  AND NOT r.cancel_requested AND w.renderer_version='v2.2';
 IF NOT FOUND THEN RETURN false;END IF;
 IF scope.operation='render.compose' OR scope.config_snapshot ? 'v26_render' THEN
  IF scope.operation<>'render.compose' OR scope.operation_version<>'1'
   OR scope.state<>'blocked' OR scope.attempt<>0 OR scope.status<>'draft' OR scope.graph_sealed
   OR scope.config_snapshot->>'project_id' IS DISTINCT FROM scope.project_id::text
   OR scope.config_snapshot->>'input_snapshot_id' IS DISTINCT FROM scope.input_snapshot_id::text
   OR jsonb_typeof(scope.config_snapshot->'v26_render'->'nodes') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  SELECT value INTO node FROM jsonb_array_elements(scope.config_snapshot->'v26_render'->'nodes')
   WHERE value->>'task_id'=p_task::text;
  IF node IS NULL OR scope.params_artifact_id::text IS DISTINCT FROM node->'params'->>'artifact_id'
   OR scope.input_digest IS DISTINCT FROM node->>'input_digest'
   OR NOT (scope.execution_profile=node->'profile' OR (photo.v2_input_closure_ready(p_task)
    AND (scope.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=node->'profile')) THEN RETURN false;END IF;
  IF NOT EXISTS(SELECT 1 FROM photo.quota_reservations q WHERE q.tenant_id=p_tenant
   AND q.holder_type='run' AND q.holder_id=p_run AND q.state='reserved' AND q.expires_at>clock_timestamp()) THEN RETURN false;END IF;
  version_id:=((node->>'params_jcs')::jsonb->'scalars'->'source'->>'asset_version_id')::uuid;
  RETURN photo.v26_render_recipe_scope(p_tenant,scope.project_id,version_id,scope.params_artifact_id,
   node->'params'->>'sha256',scope.created_by,(scope.config_snapshot->'v26_render'->>'auth_session_id')::uuid);
 END IF;
 IF scope.operation<>'layer.compose' AND NOT(scope.config_snapshot ? 'v25_composition') THEN RETURN true;END IF;
 IF scope.operation<>'layer.compose' OR scope.operation_version<>'1' OR scope.resource_class<>'cpu'
  OR scope.state<>'blocked' OR scope.attempt<>0 OR scope.status<>'draft' OR scope.graph_sealed THEN RETURN false;END IF;
 SELECT bytes INTO reserved FROM photo.quota_reservations WHERE tenant_id=p_tenant
  AND holder_type='run' AND holder_id=p_run AND state='reserved' AND expires_at>clock_timestamp();
 IF reserved IS NULL THEN RETURN false;END IF;
 IF NOT photo.v25_layer_original_plan_scope(p_tenant,scope.project_id,scope.input_snapshot_id,scope.created_by,
  (scope.config_snapshot->'v25_composition'->>'auth_session_id')::uuid,scope.workflow_id,
  scope.config_snapshot,reserved) THEN RETURN false;END IF;
 SELECT value INTO node FROM jsonb_array_elements(scope.config_snapshot->'v25_composition'->'nodes')
  WHERE value->>'task_id'=p_task::text;
 RETURN node IS NOT NULL AND scope.node_key='layer.compose:'||(node->>'ordinal')
  AND scope.params_artifact_id::text=node->'params'->>'artifact_id'
  AND scope.input_digest=node->>'input_digest'
  AND (scope.execution_profile=node->'profile' OR (photo.v2_input_closure_ready(p_task)
   AND (scope.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')=node->'profile'));
EXCEPTION WHEN invalid_text_representation THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v2_input_document_scope(uuid,uuid,uuid,text) FROM PUBLIC;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
