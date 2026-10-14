-- +goose Up
-- Exact conservative local CPU envelope; hardware/visual qualification is separate.
-- +goose StatementBegin
CREATE FUNCTION photo.v28_local_portrait_resources(p_width bigint,p_height bigint)
RETURNS jsonb LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE pixels bigint;
BEGIN
 IF p_width IS NULL OR p_height IS NULL OR p_width<1 OR p_width>200000000 OR p_height<1 THEN RETURN NULL;END IF;
 IF p_height>200000000/p_width THEN RETURN NULL;END IF;
 pixels:=p_width*p_height;
 RETURN jsonb_build_object('pixel_count',pixels,'ram_bytes',400*pixels+536870912,
  'scratch_bytes',64*pixels+67108864,'output_bytes',16*pixels+33554432,'vram_bytes',0);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v28_local_portrait_profile_shape(p_width bigint,p_height bigint,p_runtime_sha text,p_profile jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE resources jsonb;
BEGIN
 resources:=photo.v28_local_portrait_resources(p_width,p_height);
 IF resources IS NULL OR p_runtime_sha IS NULL OR p_runtime_sha !~ '^[0-9a-f]{64}$'
  OR p_profile IS NULL OR jsonb_typeof(p_profile) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 RETURN p_profile=jsonb_build_object('backend','cpu','renderer_version','v2.2',
  'portrait_runtime_sha',p_runtime_sha,'resource_estimate',resources);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v28_local_portrait_resources(bigint,bigint) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.v28_local_portrait_profile_shape(bigint,bigint,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v28_local_portrait_resources(bigint,bigint),photo.v28_local_portrait_profile_shape(bigint,bigint,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v28_local_portrait_resources(bigint,bigint),photo.v28_local_portrait_profile_shape(bigint,bigint,text,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
