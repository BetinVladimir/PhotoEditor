-- +goose Up
-- Serialize composition adoption against authority mutations through commit.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_result uuid,p_inventory jsonb,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE;
 node jsonb; params jsonb; matches integer;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.id IS NULL THEN RETURN false;END IF;
 IF t.operation='layer.compose' THEN
  BEGIN
  SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
  SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
  SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=r.id;
  -- Keep the authority rows stable until links/inbox/event commit.
  PERFORM id FROM photo.users WHERE id=r.created_by FOR SHARE;
  PERFORM id FROM photo.sessions WHERE id=(r.config_snapshot->'v25_composition'->>'auth_session_id')::uuid
   AND user_id=r.created_by FOR SHARE;
  PERFORM user_id FROM photo.memberships WHERE tenant_id=p_tenant AND user_id=r.created_by FOR SHARE;
  PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=w.project_id FOR SHARE;
  PERFORM user_id FROM photo.project_acl WHERE tenant_id=p_tenant AND project_id=w.project_id
   AND user_id=r.created_by FOR SHARE;
  IF r.id IS NULL OR w.id IS NULL OR NOT r.graph_sealed OR r.cancel_requested OR t.cancel_requested
   OR t.state<>'succeeded' OR t.attempt IS DISTINCT FROM p_attempt
   OR t.fencing_token IS DISTINCT FROM p_fence OR t.canonical_result_id IS DISTINCT FROM p_result
   OR NOT EXISTS(SELECT 1 FROM photo.v25_layer_adoption_params(p_tenant,p_task,p_attempt,p_fence,p_result))
   THEN RETURN false;END IF;
  SELECT count(*) INTO matches FROM jsonb_array_elements(r.config_snapshot->'v25_composition'->'nodes') n
   WHERE n->>'task_id'=p_task::text;
  IF matches<>1 THEN RETURN false;END IF;
  SELECT n INTO node FROM jsonb_array_elements(r.config_snapshot->'v25_composition'->'nodes') n
   WHERE n->>'task_id'=p_task::text;
  params:=(node->>'params_jcs')::jsonb;
  IF node->>'input_digest' IS DISTINCT FROM t.input_digest
   OR node->'params'->>'artifact_id' IS DISTINCT FROM t.params_artifact_id::text
   OR node->'profile' IS DISTINCT FROM (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha')
   OR coalesce(node->'profile'->>'layer_runtime_sha256','') !~ '^[0-9a-f]{64}$'
   OR NOT EXISTS(SELECT 1 FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id=t.params_artifact_id
    AND a.sha256=node->'params'->>'sha256'
    AND a.sha256=encode(sha256(convert_to(node->>'params_jcs','UTF8')),'hex')) THEN RETURN false;END IF;
  IF NOT photo.v25_layer_original_user_mask_set_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,
   (r.config_snapshot->'v25_composition'->>'auth_session_id')::uuid,params,
   (node->>'width')::bigint,(node->>'height')::bigint)
   OR NOT photo.v25_layer_lens_proposal_scope(p_tenant,w.project_id,(params->'base'->>'asset_version_id')::uuid,
    r.created_by,(r.config_snapshot->'v25_composition'->>'auth_session_id')::uuid,
    (params->'proposal_result'->>'artifact_id')::uuid,params->'proposal_result'->>'sha256',
    (params->'base'->'source'->>'artifact_id')::uuid,params->'base'->'source'->>'sha256',params->'base'->>'canvas_hash')
   THEN RETURN false;END IF;
  EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN
   RETURN false;
  END;
 END IF;
 RETURN photo.adopt_v2_plan_pre_layer_authority(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
