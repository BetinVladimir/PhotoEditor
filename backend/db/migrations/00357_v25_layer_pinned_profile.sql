-- +goose Up
-- Preserve existing resource checks while admitting an explicit immutable build pin.
ALTER FUNCTION photo.v25_layer_profile_shape(jsonb,bigint,bigint,jsonb) RENAME TO v25_layer_unpinned_profile_shape;
REVOKE ALL ON FUNCTION photo.v25_layer_unpinned_profile_shape(jsonb,bigint,bigint,jsonb) FROM PUBLIC,photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_profile_shape(p_params jsonb,p_width bigint,p_height bigint,p_profile jsonb)
RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT CASE WHEN p_profile ? 'layer_runtime_sha256' THEN
  jsonb_typeof(p_profile->'layer_runtime_sha256')='string'
  AND p_profile->>'layer_runtime_sha256' ~ '^[0-9a-f]{64}$'
  AND photo.v25_layer_unpinned_profile_shape(p_params,p_width,p_height,p_profile-'layer_runtime_sha256')
 ELSE photo.v25_layer_unpinned_profile_shape(p_params,p_width,p_height,p_profile) END;
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_profile_shape(jsonb,bigint,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_profile_shape(jsonb,bigint,bigint,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_profile_shape(jsonb,bigint,bigint,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
