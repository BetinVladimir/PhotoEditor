-- +goose Up
-- Distinguish a terminal current-result scope failure from a fenced/replayed call.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_result uuid,p_inventory jsonb,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE run_id uuid; operation_id text;
BEGIN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false; END IF;
 SELECT t.run_id,t.operation INTO run_id,operation_id FROM photo.tasks t
 WHERE t.tenant_id=p_tenant AND t.id=p_task;
 IF run_id IS NULL THEN RETURN false; END IF;
 IF operation_id='optical.lens' THEN
  PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=run_id FOR UPDATE;
  PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
  IF NOT photo.v25_lens_activation_ready(p_tenant,run_id) THEN
   IF EXISTS(SELECT 1 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
    WHERE t.tenant_id=p_tenant AND t.id=p_task AND t.state='succeeded' AND NOT t.cancel_requested
    AND t.attempt=p_attempt AND t.fencing_token=p_fence AND t.canonical_result_id=p_result
    AND r.kind='v2' AND r.status='running' AND NOT r.cancel_requested)
   AND NOT EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=p_tenant AND i.consumer='v2-adopt'
    AND i.event_id=photo.v2_adoption_event_id(p_task,p_result)) THEN
    RAISE EXCEPTION 'V25_LENS_SCOPE_STALE' USING ERRCODE='23514';
   END IF;
   RETURN false;
  END IF;
 END IF;
 RETURN photo.adopt_v2_plan_v25_core(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
