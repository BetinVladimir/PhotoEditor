-- +goose Up
-- Scope observation for the current scheduler lease; no admission permission.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v30_watermark_expansion_scope(p_tenant uuid,p_run uuid,p_expansion uuid,p_token uuid,p_task uuid)
RETURNS TABLE(project_id uuid,actor_id uuid)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE e photo.graph_expansions%ROWTYPE;r photo.runs%ROWTYPE;w photo.v2_workflows%ROWTYPE;payload jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid OR p_token IS NULL THEN RETURN;END IF;
 payload:=photo.v30_delivery_expansion_plan(p_expansion,p_token);IF payload IS NULL THEN RETURN;END IF;
 SELECT * INTO e FROM photo.graph_expansions WHERE tenant_id=p_tenant AND id=p_expansion AND run_id=p_run AND parent_task_id=p_task
 AND kind='v2.delivery/1' AND state='open' AND expansion_token=p_token AND expansion_lease_until>clock_timestamp();IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO r FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND run_id=p_run AND workflow_type='delivery' AND renderer_version='v2.2';
 IF r.created_by IS NULL OR w.id IS NULL OR payload->'proof'->>'project_id' IS DISTINCT FROM w.project_id::text
 OR photo.v2_editor_authorized(p_tenant,w.project_id,r.created_by,(payload->'proof'->>'auth_session_id')::uuid) IS DISTINCT FROM true THEN RETURN;END IF;
 RETURN QUERY SELECT w.project_id,r.created_by;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.read_v30_watermark_expansion_scope(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v30_watermark_expansion_scope(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.read_v30_watermark_expansion_scope(uuid,uuid,uuid,uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
