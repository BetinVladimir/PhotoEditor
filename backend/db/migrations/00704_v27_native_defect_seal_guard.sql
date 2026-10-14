-- +goose Up
-- Native defect preparation requires its dedicated qualified admission path.
-- Generic graph sealing must not activate it using registry/transport alone.
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) RENAME TO seal_v2_graph_pre_native_defect;
REVOKE ALL ON FUNCTION photo.seal_v2_graph_pre_native_defect(uuid,uuid) FROM PUBLIC,photo_scheduler;
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 -- Fence graph membership/config inspection using the established admission order.
 PERFORM id FROM photo.tenants WHERE id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM tenant_id FROM photo.tenant_usage WHERE tenant_id=p_tenant FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM id FROM photo.runs WHERE tenant_id=p_tenant AND id=p_run FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM photo.runs r WHERE r.tenant_id=p_tenant AND r.id=p_run
  AND r.config_snapshot ? 'v27_defect_prepare')
 OR EXISTS(SELECT 1 FROM photo.tasks t WHERE t.tenant_id=p_tenant AND t.run_id=p_run
  AND t.operation='defects.propose') THEN RETURN false;END IF;
 RETURN photo.seal_v2_graph_pre_native_defect(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
