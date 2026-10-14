-- +goose Up
-- Region draft/closure transactions execute under scheduler, while original
-- API authority functions keep their existing role grants.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_region_admission_scope_scheduler(p_tenant uuid,p_snapshot uuid,p_actor uuid,p_session uuid,p_params jsonb)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v26_region_admission_scope(p_tenant,p_snapshot,p_actor,p_session,p_params)
$$;
ALTER FUNCTION photo.v26_region_admission_scope_scheduler(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_admission_scope_scheduler(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_region_admission_scope_scheduler(uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
CREATE FUNCTION photo.v26_region_seal_scope_scheduler(p_tenant uuid,p_run uuid,p_actor uuid,p_session uuid,p_node jsonb)
RETURNS boolean LANGUAGE sql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v26_region_seal_scope(p_tenant,p_run,p_actor,p_session,p_node)
$$;
ALTER FUNCTION photo.v26_region_seal_scope_scheduler(uuid,uuid,uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_seal_scope_scheduler(uuid,uuid,uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_region_seal_scope_scheduler(uuid,uuid,uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
