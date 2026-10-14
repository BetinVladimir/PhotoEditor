-- +goose Up
-- Validate both missing and registered task state before repair publication.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_native_targets_transfer_repair_state_v3(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_run uuid,p_request_sha text,p_config_sha text)
RETURNS TABLE(graph_sealed boolean,needs_publication boolean,reserved_bytes bigint,registered_tasks uuid[])
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;q photo.quota_reservations%ROWTYPE;t photo.tasks%ROWTYPE;missing boolean:=false;seen integer:=0;expected integer;registered uuid[]:=ARRAY[]::uuid[];
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR photo.v2_editor_authorized(p_tenant,p_project,p_actor,p_session) IS NOT TRUE THEN RETURN;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;IF NOT FOUND THEN RETURN;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND OR r.kind<>'v2' OR r.cancel_requested OR r.created_by IS DISTINCT FROM p_actor
 OR r.config_hash IS DISTINCT FROM p_config_sha OR r.config_snapshot->>'project_id' IS DISTINCT FROM p_project::text
 OR r.config_snapshot->'v27_semantic'->>'auth_session_id' IS DISTINCT FROM p_session::text
 OR r.config_snapshot->'v27_native_targets_transfer'->>'parent_run_id' IS DISTINCT FROM p_run::text
 OR r.config_snapshot->'v27_native_targets_transfer'->>'request_sha' IS DISTINCT FROM p_request_sha
 OR jsonb_typeof(r.config_snapshot->'v27_semantic'->'nodes') IS DISTINCT FROM 'array' THEN RETURN;END IF;
 IF r.graph_sealed THEN RETURN QUERY SELECT true,false,0::bigint,registered;RETURN;END IF;
 IF r.status<>'draft' THEN RETURN;END IF;
 expected:=jsonb_array_length(r.config_snapshot->'v27_semantic'->'nodes');
 IF expected NOT BETWEEN 1 AND 5000 THEN RETURN;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=p_tenant AND holder_type='run' AND holder_id=p_run
  AND principal_type='user' AND principal_id=p_actor AND state='reserved' AND expires_at>clock_timestamp() FOR SHARE;
 IF NOT FOUND OR q.bytes NOT BETWEEN 1 AND 9007199254740991 THEN RETURN;END IF;
 FOR t IN SELECT * FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run ORDER BY id FOR UPDATE LOOP
  seen:=seen+1;
  IF t.operation<>'semantic.transfer' OR t.operation_version<>'1' OR t.state<>'blocked' OR t.cancel_requested OR t.canonical_result_id IS NOT NULL THEN RETURN;END IF;
  IF NOT(t.execution_profile ? 'v2_input_manifest') THEN
   IF t.inputs IS DISTINCT FROM '[]'::jsonb OR t.execution_profile ?| ARRAY['contracts_sha','v2_entry_count','v2_entries_sha']
    OR EXISTS(SELECT 1 FROM photo.v2_task_inputs i WHERE i.tenant_id=p_tenant AND i.task_id=t.id) THEN RETURN;END IF;
   missing:=true;
  ELSE
   -- Hold existing input objects against GC/update while checking liveness.
   PERFORM a.id FROM photo.v2_task_inputs i JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
    WHERE i.tenant_id=p_tenant AND i.task_id=t.id ORDER BY a.id FOR SHARE OF a;
   IF EXISTS(SELECT 1 FROM photo.v2_task_inputs i LEFT JOIN photo.artifacts a ON a.tenant_id=i.tenant_id AND a.id=i.artifact_id
    WHERE i.tenant_id=p_tenant AND i.task_id=t.id AND (a.id IS NULL OR a.state<>'ready' OR a.sha256 IS DISTINCT FROM i.sha256
     OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))) THEN RETURN;END IF;
   registered:=array_append(registered,t.id);
   IF photo.semantic_native_input_transfer_registered_transport(p_tenant,p_run,t.id,p_actor,p_session) IS NULL THEN RETURN;END IF;
  END IF;
 END LOOP;
 IF seen<>expected THEN RETURN;END IF;
 RETURN QUERY SELECT false,missing,q.bytes,registered;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN;
END $$;
ALTER FUNCTION photo.lock_semantic_native_targets_transfer_repair_state_v3(uuid,uuid,uuid,uuid,uuid,text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_native_targets_transfer_repair_state_v3(uuid,uuid,uuid,uuid,uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_native_targets_transfer_repair_state_v3(uuid,uuid,uuid,uuid,uuid,text,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
