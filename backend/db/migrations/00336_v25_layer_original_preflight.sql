-- +goose Up
-- Read-only composition preflight. Atomic admission must repeat under locks.
-- It proves original metadata provenance/profile, not parent object bytes.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_original_preflight(p_tenant uuid,p_project uuid,p_snapshot uuid,
 p_actor uuid,p_session uuid,p_params jsonb,p_width bigint,p_height bigint,p_profile jsonb)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v25_layer_original_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_params)
  AND photo.v25_layer_profile_shape(p_params,p_width,p_height,p_profile)
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_original_preflight(uuid,uuid,uuid,uuid,uuid,jsonb,bigint,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_original_preflight(uuid,uuid,uuid,uuid,uuid,jsonb,bigint,bigint,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_original_preflight(uuid,uuid,uuid,uuid,uuid,jsonb,bigint,bigint,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
