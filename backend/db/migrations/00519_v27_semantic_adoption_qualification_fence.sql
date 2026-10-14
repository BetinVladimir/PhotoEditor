-- +goose Up
-- Frozen qualification is mandatory for semantic adoption, including replays.
-- +goose StatementBegin
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) RENAME TO adopt_v2_plan_pre_semantic_qualification;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_pre_semantic_qualification(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE t photo.tasks%ROWTYPE; accepted boolean;
BEGIN
 SELECT * INTO t FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task;
 IF t.operation IN('semantic.transfer','v2.semantic.assemble') THEN
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=t.run_id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=t.run_id ORDER BY id FOR UPDATE;
 IF NOT photo.v27_semantic_adoption_qualification_ready(p_tenant,p_task,p_attempt,p_fence,p_result) THEN RETURN false;END IF;
 accepted:=photo.adopt_v2_plan_pre_semantic_qualification(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
 IF accepted AND NOT photo.v27_semantic_adoption_qualification_ready(p_tenant,p_task,p_attempt,p_fence,p_result) THEN
 RAISE EXCEPTION 'SEMANTIC_ADOPTION_QUALIFICATION_STALE' USING ERRCODE='23514';END IF;
 RETURN accepted;
 END IF;
 RETURN photo.adopt_v2_plan_pre_semantic_qualification(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
