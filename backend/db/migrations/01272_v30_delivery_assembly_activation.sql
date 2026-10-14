-- +goose Up
-- Final parent barrier transition. Caller must commit or roll back this admission.
-- +goose StatementBegin
CREATE FUNCTION photo.activate_v30_delivery_assembly(p_expansion uuid,p_token uuid,p_task jsonb,p_params text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;t photo.tasks%ROWTYPE;q photo.quota_reservations%ROWTYPE;
 previous_context text;source_ready boolean;
BEGIN
 IF photo.v30_delivery_assembly_transport_ready(p_expansion,p_token,p_task,p_params) IS DISTINCT FROM true THEN RETURN false;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE id=p_expansion FOR UPDATE;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=e.tenant_id AND id=e.run_id FOR UPDATE;
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND id=(p_task->>'id')::uuid FOR UPDATE;
 IF e.id IS NULL OR e.state<>'open' OR e.expansion_token IS DISTINCT FROM p_token
 OR e.expansion_lease_until<=clock_timestamp() OR e.cursor::integer<>e.expected_count
 OR r.id IS NULL OR r.kind<>'v2' OR r.status<>'running' OR r.graph_sealed OR r.cancel_requested
 OR t.id IS NULL OR t.state<>'blocked' OR t.attempt<>0 OR t.cancel_requested THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.graph_expansions WHERE tenant_id=e.tenant_id AND run_id=e.run_id AND id<>e.id AND state<>'closed') THEN RETURN false;END IF;
 PERFORM version FROM photo.operation_registry WHERE operation=t.operation AND version=t.operation_version
 AND resource_class='cpu' AND v2_enabled AND backends ? 'cpu' FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO q FROM photo.quota_reservations WHERE tenant_id=e.tenant_id AND holder_type='run' AND holder_id=e.run_id
 AND state='reserved' AND expires_at>clock_timestamp() FOR UPDATE;
 IF NOT FOUND OR q.bytes<8388608 OR NOT EXISTS(SELECT 1 FROM photo.tenant_usage WHERE tenant_id=e.tenant_id AND reserved_bytes>=q.bytes) THEN RETURN false;END IF;
 previous_context:=current_setting('app.tenant_id',true);
 PERFORM set_config('app.tenant_id',e.tenant_id::text,true);
 BEGIN
  source_ready:=photo.v30_delivery_assembly_source_ready(e.tenant_id,e.run_id,t.id,t.params_artifact_id,p_task->>'params_sha',p_params);
 EXCEPTION WHEN OTHERS THEN
  PERFORM set_config('app.tenant_id',coalesce(previous_context,''),true);RAISE;
 END;
 PERFORM set_config('app.tenant_id',coalesce(previous_context,''),true);
 IF source_ready IS DISTINCT FROM true THEN RETURN false;END IF;
 UPDATE photo.graph_expansions SET state='closed' WHERE id=e.id;
 UPDATE photo.runs SET graph_sealed=true,revision=revision+1 WHERE tenant_id=e.tenant_id AND id=e.run_id;
 IF photo.activate_bound_task(t.id,t.input_digest,t.params_artifact_id) IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'DELIVERY_ASSEMBLY_ACTIVATION_REFUSED' USING ERRCODE='23514';END IF;
 IF e.expansion_lease_until<=clock_timestamp() OR q.expires_at<=clock_timestamp() THEN
  RAISE EXCEPTION 'DELIVERY_ASSEMBLY_ACTIVATION_FENCE_EXPIRED' USING ERRCODE='40001';END IF;
 RETURN true;
END $$;
ALTER FUNCTION photo.activate_v30_delivery_assembly(uuid,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.activate_v30_delivery_assembly(uuid,uuid,jsonb,text) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.activate_v30_delivery_assembly(uuid,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
