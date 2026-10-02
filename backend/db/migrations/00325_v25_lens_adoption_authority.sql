-- +goose Up
-- Keep the existing V21/V22/V23 adoption chain behind a lens authority fence.
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)
 RENAME TO adopt_v2_plan_v25_core;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_v25_core(uuid,uuid,integer,bigint,uuid,jsonb,text)
 FROM photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
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
  IF NOT photo.v25_lens_activation_ready(p_tenant,run_id) THEN RETURN false; END IF;
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
