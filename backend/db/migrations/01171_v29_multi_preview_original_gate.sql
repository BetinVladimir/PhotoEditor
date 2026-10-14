-- +goose Up
-- Compare a completed source read to the exact fresh occurrence inventory.
-- Private helper: caller still owes output/body checks and atomic graph seal.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_multi_preview_originals_current(p_tenant uuid,p_run uuid,p_inventory jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE r photo.runs%ROWTYPE;s record;fresh jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR jsonb_typeof(p_inventory) IS DISTINCT FROM 'object'
 OR octet_length(p_inventory::text)>8388608 THEN RETURN false;END IF;
 PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO s FROM photo.v29_multi_preview_intent_source(p_tenant,p_run);
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.projects WHERE tenant_id=p_tenant AND id=s.project_id FOR SHARE;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=s.run_id FOR UPDATE;
 PERFORM id FROM photo.tasks WHERE tenant_id=p_tenant AND id=r.v29_multi_preview_task_id FOR UPDATE;
 PERFORM id FROM photo.artifacts WHERE tenant_id=p_tenant AND id=s.root_id FOR SHARE;
 IF NOT EXISTS(SELECT 1 FROM photo.v29_multi_preview_intent_source(p_tenant,p_run) current_source
 WHERE current_source.result_id=s.result_id AND current_source.root_id=s.root_id AND current_source.root_sha=s.root_sha
 AND current_source.attempt=s.attempt AND current_source.fence=s.fence) THEN RETURN false;END IF;
 -- Inventory resolver checks every exact frozen source role and locks artifacts
 -- in UUID order. JSONB equality includes binding, root locator and ordered
 -- occurrence list: omitted/added/reordered roles or changed versions fail.
 fresh:=photo.v29_adopted_multi_original_inventory(p_tenant,s.project_id,r.v29_multi_preview_task_id,
 s.actor_id,s.auth_session_id,r.config_snapshot->>'operation',s.root_id,s.root_sha);
 RETURN fresh IS NOT NULL AND fresh=p_inventory;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v29_multi_preview_originals_current(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_multi_preview_originals_current(uuid,uuid,jsonb) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_portal,photo_gateway;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
