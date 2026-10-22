-- +goose Up
-- Preserve all installed adoption gates; shown-copy requires a dedicated commit.
-- +goose StatementBegin
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text)
 RENAME TO adopt_v2_plan_before_shown_exposure;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan_before_shown_exposure(uuid,uuid,integer,bigint,uuid,jsonb,text)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
CREATE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,
 p_result uuid,p_inventory jsonb,p_digest text) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task
 AND operation='semantic.adopt_shown_exposure') THEN RETURN false;END IF;
 RETURN photo.adopt_v2_plan_before_shown_exposure(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
