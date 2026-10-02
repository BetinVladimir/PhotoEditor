-- +goose Up
-- Read-only admission evidence for a currently authorized editor. The caller
-- must verify pinned source/recipe bytes before projecting public metadata.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_region_descriptor_node(p_tenant uuid,p_run uuid,p_asset uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(project_id uuid,task_id uuid,node jsonb)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE; t photo.tasks%ROWTYPE;
 n jsonb; params jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid THEN RETURN; END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 IF r.id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested
  OR r.status NOT IN('queued','running','completed','waiting_review') THEN RETURN; END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF w.id IS NULL OR w.workflow_type<>'preview' OR w.renderer_version<>'v2.2'
  OR NOT photo.v2_editor_authorized(p_tenant,w.project_id,p_actor,p_session) THEN RETURN; END IF;
 IF jsonb_typeof(r.config_snapshot->'v26_region'->'nodes') IS DISTINCT FROM 'array'
  OR jsonb_array_length(r.config_snapshot->'v26_region'->'nodes')<>1 THEN RETURN; END IF;
 n:=r.config_snapshot->'v26_region'->'nodes'->0;params:=(n->>'params_jcs')::jsonb;
 IF params->>'tenant_id' IS DISTINCT FROM p_tenant::text OR params->>'asset_id' IS DISTINCT FROM p_asset::text
  OR params->>'project_id' IS DISTINCT FROM w.project_id::text THEN RETURN; END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=(n->>'task_id')::uuid;
 IF t.id IS NULL OR t.cancel_requested OR t.operation<>'region.preview' OR t.operation_version<>'1'
  OR t.resource_class<>'gpu' OR n->'profile'->>'renderer_version' IS DISTINCT FROM 'v2.2'
  OR n->'profile'->>'backend' IS DISTINCT FROM 'cuda'
  OR n->'profile'->>'icc_lock_sha' IS DISTINCT FROM params->'frozen_recipe'->>'icc_lock_sha'
  OR n->'profile'->>'model_lock_sha' IS DISTINCT FROM params->'frozen_recipe'->>'model_lock_sha'
  OR coalesce(n->'profile'->>'region_runtime_sha256','') !~ '^[0-9a-f]{64}$'
  OR t.params_artifact_id::text IS DISTINCT FROM n->'params'->>'artifact_id'
  OR t.input_digest IS DISTINCT FROM n->>'input_digest'
  OR (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') IS DISTINCT FROM n->'profile'
  OR NOT photo.v2_input_closure_ready(t.id) THEN RETURN; END IF;
 IF EXISTS(SELECT FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a
  ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
  WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND (a.id IS NULL OR a.state<>'ready'
   OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null')
   OR a.expires_at<=clock_timestamp())) THEN RETURN; END IF;
 IF NOT EXISTS(SELECT FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
  AND a.sha256=n->'params'->>'sha256'
  AND a.sha256=encode(sha256(convert_to(n->>'params_jcs','UTF8')),'hex')) THEN RETURN; END IF;
 RETURN QUERY SELECT w.project_id,t.id,n;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_region_descriptor_node(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_descriptor_node(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_region_descriptor_node(uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
