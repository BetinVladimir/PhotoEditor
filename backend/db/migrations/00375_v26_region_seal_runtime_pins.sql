-- +goose Up
-- Second-phase live authority gate; caller retains TX through graph sealing.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v26_region_seal_scope(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_node jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE; t photo.tasks%ROWTYPE;
 nodes jsonb; params jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF r.id IS NULL OR r.created_by IS DISTINCT FROM p_actor OR r.cancel_requested OR r.kind<>'v2'
  OR r.config_snapshot->'v26_region'->>'auth_session_id' IS DISTINCT FROM p_session::text THEN RETURN false; END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF w.id IS NULL OR w.workflow_type<>'preview' OR w.renderer_version<>'v2.2' THEN RETURN false; END IF;
 nodes:=r.config_snapshot->'v26_region'->'nodes';
 IF jsonb_typeof(nodes) IS DISTINCT FROM 'array' THEN RETURN false; END IF;
 IF jsonb_array_length(nodes)<>1 OR nodes->0 IS DISTINCT FROM p_node THEN RETURN false; END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(p_node->>'task_id')::uuid FOR UPDATE;
 params:=(p_node->>'params_jcs')::jsonb;
 IF t.id IS NULL OR t.cancel_requested OR t.operation<>'region.preview' OR t.operation_version<>'1'
  OR t.resource_class<>'gpu'
  OR p_node->'profile'->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR p_node->'profile'->>'backend' IS DISTINCT FROM 'cuda'
  OR p_node->'profile'->>'icc_lock_sha' IS DISTINCT FROM params->'frozen_recipe'->>'icc_lock_sha'
  OR p_node->'profile'->>'model_lock_sha' IS DISTINCT FROM params->'frozen_recipe'->>'model_lock_sha'
  OR coalesce(p_node->'profile'->>'region_runtime_sha256','') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_node->'profile'->>'icc_lock_sha','') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_node->'profile'->>'model_lock_sha','') !~ '^[0-9a-f]{64}$'
  OR t.params_artifact_id::text IS DISTINCT FROM p_node->'params'->>'artifact_id'
  OR t.input_digest IS DISTINCT FROM p_node->>'input_digest'
  OR (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') IS DISTINCT FROM p_node->'profile'
  OR params->>'project_id' IS DISTINCT FROM w.project_id::text
  OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id
   AND a.sha256=p_node->'params'->>'sha256'
   AND a.sha256=encode(sha256(convert_to(p_node->>'params_jcs','UTF8')),'hex')) THEN RETURN false; END IF;
 RETURN photo.v26_region_admission_scope(p_tenant,r.input_snapshot_id,p_actor,p_session,params);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_region_seal_scope(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_seal_scope(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_region_seal_scope(uuid,uuid,uuid,uuid,jsonb) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
