-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION photo.admit_v26_region_preview(p_tenant uuid,p_run uuid,p_task uuid,p_attempt integer,
 p_fence bigint,p_worker uuid,p_params uuid,p_sha text,p_json text,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; r photo.runs%ROWTYPE; slot photo.resource_slots%ROWTYPE; node jsonb;
BEGIN
 IF p_attempt IS NULL OR p_attempt<1 OR p_fence IS NULL OR p_fence<1 OR p_json IS NULL
  OR octet_length(p_json)>1048576 OR p_sha IS NULL
  OR encode(sha256(convert_to(p_json,'UTF8')),'hex') IS DISTINCT FROM p_sha THEN RETURN false; END IF;
 IF NOT EXISTS(SELECT 1 FROM photo.worker_registrations WHERE tenant_id=p_tenant AND worker_id=p_worker
  AND db_principal=session_user AND enabled) THEN RETURN false; END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND id=p_task FOR UPDATE;
 IF r.id IS NULL OR r.kind<>'v2' OR NOT r.graph_sealed OR r.cancel_requested OR r.status NOT IN('queued','running')
  OR t.id IS NULL OR t.state<>'running' OR t.cancel_requested OR t.operation<>'region.preview'
  OR t.operation_version<>'1' OR t.resource_class<>'gpu' OR t.attempt IS DISTINCT FROM p_attempt
  OR t.fencing_token IS DISTINCT FROM p_fence OR t.worker_id IS DISTINCT FROM p_worker
  OR t.params_artifact_id IS DISTINCT FROM p_params OR t.input_digest IS DISTINCT FROM p_digest
  OR t.lease_until IS NULL OR t.lease_until<=clock_timestamp()
  OR t.deadline IS NULL OR t.deadline<=clock_timestamp() THEN RETURN false; END IF;
 SELECT * INTO slot FROM photo.resource_slots WHERE tenant_id=p_tenant AND task_id=p_task FOR SHARE;
 IF slot.id IS NULL OR slot.fencing_token IS DISTINCT FROM p_fence OR slot.resource_class<>'gpu'
  OR slot.lease_until IS NULL OR slot.lease_until<=clock_timestamp()
  OR NOT photo.worker_authorized(p_tenant,p_worker,'gpu',slot.node_id,t.operation,
   t.execution_profile->>'backend',t.execution_profile->>'model_lock_id')
  OR NOT photo.v2_input_closure_ready(p_task) THEN RETURN false; END IF;
 node:=r.config_snapshot->'v26_region'->'nodes'->0;
 IF node->>'task_id' IS DISTINCT FROM p_task::text OR node->>'params_jcs' IS DISTINCT FROM p_json
  OR node->'params'->>'sha256' IS DISTINCT FROM p_sha THEN RETURN false; END IF;
 PERFORM set_config('app.tenant_id',p_tenant::text,true);
 IF NOT photo.v26_region_seal_scope(p_tenant,p_run,r.created_by,
  (r.config_snapshot->'v26_region'->>'auth_session_id')::uuid,node) THEN RETURN false; END IF;
 RETURN t.lease_until>clock_timestamp() AND t.deadline>clock_timestamp() AND slot.lease_until>clock_timestamp();
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.admit_v26_region_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.admit_v26_region_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.admit_v26_region_preview(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text) TO photo_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
