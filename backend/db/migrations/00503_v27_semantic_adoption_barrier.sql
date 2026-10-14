-- +goose Up
-- Acceptance and durable expansion creation share the same transaction.
-- +goose StatementBegin
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) RENAME TO adopt_v2_plan_pre_semantic_barrier;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_semantic_barrier(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE accepted boolean; owner_run uuid;
BEGIN
 accepted:=photo.adopt_v2_plan_pre_semantic_barrier(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
 IF accepted THEN
  SELECT run_id INTO owner_run FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task
   AND operation='semantic.transfer' AND operation_version='1';
  IF owner_run IS NOT NULL THEN PERFORM photo.open_v27_semantic_assembly(p_tenant,owner_run);END IF;
 END IF;
 RETURN accepted;
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
