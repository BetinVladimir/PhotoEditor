-- +goose Up
-- Seal scope also guards scheduler's generic graph entry point. Calling the
-- generic function directly cannot omit Region authority or qualification.
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) RENAME TO seal_v2_graph_pre_region_qualification;
REVOKE ALL ON FUNCTION photo.seal_v2_graph_pre_region_qualification(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE run photo.runs%ROWTYPE; node jsonb; nodes jsonb;
BEGIN
 IF EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run AND config_snapshot ? 'v26_region')
 OR EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='region.preview') THEN
  PERFORM id FROM photo.tenants WHERE id=p_tenant AND status='active' FOR UPDATE;
  IF NOT FOUND THEN RETURN false;END IF;
  PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
  SELECT * INTO run FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
  IF run.id IS NULL THEN RETURN false;END IF;
  nodes:=run.config_snapshot->'v26_region'->'nodes';
  IF jsonb_typeof(nodes) IS DISTINCT FROM 'array' THEN RETURN false;END IF;
  IF jsonb_array_length(nodes)<>1 OR
   (SELECT count(*) FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='region.preview')<>1 THEN RETURN false;END IF;
  node:=nodes->0;
  PERFORM set_config('app.tenant_id',p_tenant::text,true);
  IF NOT photo.v26_region_seal_scope(p_tenant,p_run,run.created_by,
   (run.config_snapshot->'v26_region'->>'auth_session_id')::uuid,node) THEN RETURN false;END IF;
 END IF;
 RETURN photo.seal_v2_graph_pre_region_qualification(p_tenant,p_run);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
