-- +goose Up
-- Bind region commit to the frozen run node before generic relational adoption.
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)
 RENAME TO adopt_v2_plan_pre_region_authority;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_region_authority(uuid,uuid,integer,bigint,uuid,jsonb,text)
 FROM PUBLIC,photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_result uuid,p_inventory jsonb,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE;
 nodes jsonb; node jsonb; params jsonb; matches integer; session uuid;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.id IS NULL THEN RETURN false; END IF;
 IF t.operation='region.preview' THEN
  BEGIN
   -- Disabled catalog cannot publish manually seeded region results.
   IF NOT EXISTS(SELECT 1 FROM photo.operation_registry WHERE operation='region.preview' AND version='1' AND v2_enabled) THEN RETURN false; END IF;
   SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
   SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
   SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id;
   IF r.id IS NULL OR w.id IS NULL OR NOT r.graph_sealed OR r.cancel_requested OR t.cancel_requested
    OR t.state<>'succeeded' OR t.operation_version<>'1' OR t.attempt IS DISTINCT FROM p_attempt
    OR t.fencing_token IS DISTINCT FROM p_fence OR t.canonical_result_id IS DISTINCT FROM p_result
    OR NOT EXISTS(SELECT 1 FROM photo.v26_region_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result))
    THEN RETURN false; END IF;
   nodes:=r.config_snapshot->'v26_region'->'nodes';
   IF jsonb_typeof(nodes) IS DISTINCT FROM 'array' THEN RETURN false; END IF;
   SELECT count(*) INTO matches FROM jsonb_array_elements(nodes) n WHERE n->>'task_id'=p_task::text;
   IF matches<>1 THEN RETURN false; END IF;
   SELECT n INTO node FROM jsonb_array_elements(nodes) n WHERE n->>'task_id'=p_task::text;
   params:=(node->>'params_jcs')::jsonb;
   session:=(r.config_snapshot->'v26_region'->>'auth_session_id')::uuid;
   IF node->>'input_digest' IS DISTINCT FROM t.input_digest
    OR node->'params'->>'artifact_id' IS DISTINCT FROM t.params_artifact_id::text
    OR params->>'project_id' IS DISTINCT FROM w.project_id::text
    OR node->'profile' IS DISTINCT FROM (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')
    OR node->'profile'->>'renderer_version' IS DISTINCT FROM 'v2.2'
    OR node->'profile'->>'backend' IS DISTINCT FROM 'cuda'
    OR node->'profile'->>'icc_lock_sha' IS DISTINCT FROM params->'frozen_recipe'->>'icc_lock_sha'
    OR node->'profile'->>'model_lock_sha' IS DISTINCT FROM params->'frozen_recipe'->>'model_lock_sha'
    OR coalesce(node->'profile'->>'region_runtime_sha256','') !~ '^[0-9a-f]{64}$'
    OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id
     AND a.sha256=node->'params'->>'sha256'
     AND a.sha256=encode(sha256(convert_to(node->>'params_jcs','UTF8')),'hex')) THEN RETURN false; END IF;
   IF NOT photo.v26_region_locked_scope(p_tenant,r.created_by,session,params) THEN RETURN false; END IF;
   IF NOT photo.v2_draft_artifact_scope(p_tenant,w.project_id,(params->'request'->>'version_id')::uuid,
     (params->'frozen_recipe'->'base_style'->>'artifact_id')::uuid,params->'frozen_recipe'->'base_style'->>'sha256',
     'StyleV2',r.created_by,session,NULL,NULL) THEN RETURN false; END IF;
  EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
  END;
 END IF;
 RETURN photo.adopt_v2_plan_pre_region_authority(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
