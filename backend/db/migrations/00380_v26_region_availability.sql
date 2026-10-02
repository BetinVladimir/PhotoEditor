-- +goose Up
-- Early availability gate only. Live source/recipe and seal authority remain mandatory.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_region_available(p_tenant uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT p_tenant::text IS NOT DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')
  AND EXISTS(SELECT 1 FROM photo.tenants WHERE id=p_tenant AND status='active')
  AND EXISTS(SELECT 1 FROM photo.operation_registry WHERE operation='region.preview' AND version='1' AND v2_enabled)
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_region_available(uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_available(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_region_available(uuid) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
