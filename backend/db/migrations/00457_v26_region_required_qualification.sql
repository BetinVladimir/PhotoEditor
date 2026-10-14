-- +goose Up
-- Region never activated by this migration. Every seal/worker admission/adoption
-- now requires a complete measured qualification; no legacy profile bypass.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v26_region_seal_scope(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_node jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE workflow photo.v2_workflows%ROWTYPE;
BEGIN
 IF NOT photo.v26_region_seal_scope_pre_qualification(p_tenant,p_run,p_actor,p_session,p_node) THEN RETURN false;END IF;
 IF NOT (p_node->'profile' ? 'region_qualification') THEN RETURN false;END IF;
 SELECT * INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run;
 RETURN workflow.id IS NOT NULL AND photo.v26_region_qualification_pin_ready(p_tenant,workflow.project_id,workflow.policy_sha,p_node->'profile');
END $$;
ALTER FUNCTION photo.v26_region_seal_scope(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_seal_scope(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_region_seal_scope(uuid,uuid,uuid,uuid,jsonb) TO photo_api;

CREATE OR REPLACE FUNCTION photo.adopt_v2_plan(p_tenant uuid,p_task uuid,p_attempt integer,p_fence bigint,p_result uuid,p_inventory jsonb,p_digest text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE task photo.tasks%ROWTYPE; run photo.runs%ROWTYPE; workflow photo.v2_workflows%ROWTYPE; node jsonb;
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task AND operation='region.preview') THEN
  PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
  IF NOT FOUND THEN RETURN false;END IF;
  SELECT r.* INTO run FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
   WHERE t.tenant_id=p_tenant AND t.id=p_task FOR UPDATE OF r;
  SELECT * INTO task FROM photo.tasks WHERE tenant_id=p_tenant AND id=p_task FOR UPDATE;
  SELECT * INTO workflow FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=run.id;
  node:=run.config_snapshot->'v26_region'->'nodes'->0;
  IF NOT (node->'profile' ? 'region_qualification') THEN RETURN false;END IF;
   PERFORM set_config('app.tenant_id',p_tenant::text,true);
   IF workflow.id IS NULL OR NOT photo.v26_region_qualification_pin_ready(p_tenant,workflow.project_id,workflow.policy_sha,node->'profile') THEN RETURN false;END IF;
 END IF;
 RETURN photo.adopt_v2_plan_pre_region_qualification(p_tenant,p_task,p_attempt,p_fence,p_result,p_inventory,p_digest);
END $$;
ALTER FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.adopt_v2_plan(uuid,uuid,integer,bigint,uuid,jsonb,text) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
