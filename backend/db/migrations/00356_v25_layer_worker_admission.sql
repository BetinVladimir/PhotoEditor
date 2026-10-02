-- +goose Up
-- Attempt-fenced admission for the implemented original/lens/user-mask path.
-- Other parent families remain denied until their relational proofs exist.
-- +goose StatementBegin
CREATE FUNCTION photo.admit_v25_lens_user_composition(p_tenant uuid,p_run uuid,p_task uuid,
 p_attempt integer,p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE; w photo.v2_workflows%ROWTYPE;
 slot photo.resource_slots%ROWTYPE; node jsonb; params jsonb; matches integer;
BEGIN
 IF p_json IS NULL OR octet_length(p_json)>65536 OR p_attempt IS NULL OR p_attempt<1
  OR p_fence IS NULL OR p_fence<1 OR p_sha IS NULL
  OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN false;END IF;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task;
 IF t.id IS NULL OR t.operation<>'layer.compose' OR t.operation_version<>'1' OR t.resource_class<>'cpu'
  OR t.state<>'running' OR t.cancel_requested OR t.attempt IS DISTINCT FROM p_attempt
  OR t.fencing_token IS DISTINCT FROM p_fence OR t.worker_id IS DISTINCT FROM p_worker
  OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
  OR t.deadline IS NULL OR t.deadline<=clock_timestamp()
  OR t.params_artifact_id IS DISTINCT FROM p_params OR t.input_digest IS DISTINCT FROM p_digest
  OR t.execution_profile->>'backend' IS DISTINCT FROM 'cpu'
  OR NOT EXISTS(SELECT 1 FROM photo.worker_registrations reg WHERE reg.tenant_id=p_tenant
   AND reg.worker_id=p_worker AND reg.db_principal=session_user AND reg.enabled)
  OR NOT EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active') THEN RETURN false;END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE tenant_id=p_tenant AND task_id=p_task;
 IF slot.id IS NULL OR slot.fencing_token IS DISTINCT FROM p_fence
  OR slot.resource_class IS DISTINCT FROM 'cpu'
  OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp()
  OR NOT photo.worker_authorized(p_tenant,p_worker,'cpu',slot.node_id,t.operation,
   t.execution_profile->>'backend',t.execution_profile->>'model_lock_id') THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 IF r.id IS NULL OR w.id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested
  OR r.status NOT IN('queued','running') OR w.renderer_version<>'v2.2'
  OR NOT photo.v2_input_closure_ready(p_task) THEN RETURN false;END IF;
 SELECT count(*) INTO matches FROM jsonb_array_elements(r.config_snapshot->'v25_composition'->'nodes') n
  WHERE n->>'task_id'=p_task::text;
 IF matches<>1 THEN RETURN false;END IF;
 SELECT n INTO node FROM jsonb_array_elements(r.config_snapshot->'v25_composition'->'nodes') n
  WHERE n->>'task_id'=p_task::text;
 IF node->>'params_jcs' IS DISTINCT FROM p_json
  OR node->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',p_params::text,'sha256',p_sha)
  OR node->>'input_digest' IS DISTINCT FROM p_digest
  OR node->'profile' IS DISTINCT FROM
   (t.execution_profile-'contracts_sha'-'v2_input_manifest'-'v2_entry_count'-'v2_entries_sha') THEN RETURN false;END IF;
 params:=p_json::jsonb;
 IF NOT EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=p_task
  AND i.artifact_id=p_params AND i.sha256=p_sha AND i.contract='Params') THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a
  ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id AND a.sha256=i.sha256
  WHERE i.tenant_id=p_tenant AND i.task_id=p_task AND
   (a.id IS NULL OR a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IN('','null')
    OR a.object_version IS NULL OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN false;END IF;
 RETURN photo.v25_layer_original_user_mask_set_scope(p_tenant,w.project_id,r.input_snapshot_id,r.created_by,
  (r.config_snapshot->'v25_composition'->>'auth_session_id')::uuid,params,
  (node->>'width')::bigint,(node->>'height')::bigint)
  AND photo.v25_layer_lens_proposal_scope(p_tenant,w.project_id,(params->'base'->>'asset_version_id')::uuid,
   r.created_by,(r.config_snapshot->'v25_composition'->>'auth_session_id')::uuid,
   (params->'proposal_result'->>'artifact_id')::uuid,params->'proposal_result'->>'sha256',
   (params->'base'->'source'->>'artifact_id')::uuid,params->'base'->'source'->>'sha256',params->'base'->>'canvas_hash');
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN
 RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.admit_v25_lens_user_composition(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.admit_v25_lens_user_composition(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.admit_v25_lens_user_composition(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
