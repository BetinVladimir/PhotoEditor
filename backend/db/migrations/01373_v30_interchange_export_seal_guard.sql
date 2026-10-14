-- +goose Up
-- Registered inputs alone cannot dispatch interchange export. A dedicated
-- activation path must additionally bind native/resource qualification.
-- +goose StatementBegin
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) RENAME TO seal_v2_graph_pre_v30_export;
REVOKE ALL ON FUNCTION photo.seal_v2_graph_pre_v30_export(uuid,uuid) FROM PUBLIC,photo_scheduler,photo_api,photo_worker,photo_gateway,photo_portal;
CREATE FUNCTION photo.seal_v2_graph(p_tenant uuid,p_run uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=p_tenant AND run_id=p_run AND operation='v2.interchange.export') THEN RETURN false;END IF;
 RETURN photo.seal_v2_graph_pre_v30_export(p_tenant,p_run);
END $$;
ALTER FUNCTION photo.seal_v2_graph(uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.seal_v2_graph(uuid,uuid) FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.seal_v2_graph(uuid,uuid) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
