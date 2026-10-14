-- +goose Up
-- Canonical negative CPU envelope. Caller must establish original source dimensions/alpha.
-- +goose StatementBegin
CREATE FUNCTION photo.v29_negative_resources(p_width bigint,p_height bigint,p_alpha boolean)
RETURNS jsonb LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE pixels bigint;owners bigint;material bigint;output bigint;
BEGIN
 IF p_width IS NULL OR p_height IS NULL OR p_alpha IS NULL OR p_width<1 OR p_width>200000000 OR p_height<1 THEN RETURN NULL;END IF;
 IF p_height>200000000/p_width THEN RETURN NULL;END IF;
 pixels:=p_width*p_height;owners:=28+CASE WHEN p_alpha THEN 4 ELSE 0 END;
 material:=(12+CASE WHEN p_alpha THEN 4 ELSE 0 END)*pixels+32768;output:=16*pixels+32768;
 RETURN jsonb_build_object('pixel_count',pixels,'vram_bytes',0,
 'ram_bytes',owners*pixels+128*least(pixels,262144)+536870912,
 'material_scratch_bytes',material,'output_scratch_bytes',output,
 'scratch_bytes',material+output,'output_bytes',16*pixels+32768+262144);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION photo.v29_negative_profile_shape(p_width bigint,p_height bigint,p_alpha boolean,
 p_runtime_sha text,p_pin jsonb,p_profile jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,photo AS $$
DECLARE resources jsonb;
BEGIN
 resources:=photo.v29_negative_resources(p_width,p_height,p_alpha);
 IF resources IS NULL OR p_runtime_sha IS NULL OR p_runtime_sha !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(p_pin) IS DISTINCT FROM 'object' OR p_pin->>'backend' IS DISTINCT FROM 'cpu'
 OR p_pin->>'runtime_lock_sha' IS DISTINCT FROM p_runtime_sha
 OR jsonb_typeof(p_profile) IS DISTINCT FROM 'object' THEN RETURN false;END IF;
 RETURN p_profile=jsonb_build_object('backend','cpu','renderer_version','v2.2',
 'negative_runtime_sha',p_runtime_sha,'negative_qualification',p_pin,'resource_estimate',resources);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v29_negative_resources(bigint,bigint,boolean) OWNER TO photo_fn_owner;
ALTER FUNCTION photo.v29_negative_profile_shape(bigint,bigint,boolean,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v29_negative_resources(bigint,bigint,boolean),photo.v29_negative_profile_shape(bigint,bigint,boolean,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v29_negative_resources(bigint,bigint,boolean),photo.v29_negative_profile_shape(bigint,bigint,boolean,text,jsonb,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
