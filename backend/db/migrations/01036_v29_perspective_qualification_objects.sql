-- +goose Up
-- Exact original locators checked while current qualification locks are held.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v29_perspective_qualification_objects(p_tenant uuid,p_project uuid,
 p_actor uuid,p_session uuid,p_pin jsonb,p_objects jsonb) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE current_objects jsonb;
BEGIN
 IF p_objects IS NULL OR jsonb_typeof(p_objects) IS DISTINCT FROM 'array'
 OR octet_length(p_objects::text)>16384 THEN RETURN false;END IF;
 IF jsonb_array_length(p_objects)<>2 THEN RETURN false;END IF;
 current_objects:=photo.v29_perspective_qualification_sources(p_tenant,p_project,p_actor,p_session,p_pin);
 RETURN current_objects IS NOT NULL AND current_objects=p_objects;
END $$;
ALTER FUNCTION photo.lock_v29_perspective_qualification_objects(uuid,uuid,uuid,uuid,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_perspective_qualification_objects(uuid,uuid,uuid,uuid,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v29_perspective_qualification_objects(uuid,uuid,uuid,uuid,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
