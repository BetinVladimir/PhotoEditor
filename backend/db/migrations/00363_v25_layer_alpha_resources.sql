-- +goose Up
-- Reserve both normalized parent alpha mappings in the frozen profile.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION photo.v25_layer_profile_shape(p_params jsonb,p_width bigint,p_height bigint,p_profile jsonb)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE baseline jsonb; expected jsonb; extra bigint; stripped jsonb; mode text;
BEGIN
 IF p_profile IS NULL OR jsonb_typeof(p_profile)<>'object' THEN RETURN false; END IF;
 mode:=coalesce(p_profile->>'layer_alpha_mode','require_float32');
 IF p_profile ? 'layer_alpha_mode' AND (jsonb_typeof(p_profile->'layer_alpha_mode')<>'string'
  OR mode NOT IN ('require_float32','normalize_uint16')) THEN RETURN false; END IF;
 stripped:=p_profile-'layer_alpha_mode';
 IF mode='normalize_uint16' THEN
  baseline:=photo.v25_layer_resources(p_params,p_width,p_height);
  IF baseline IS NULL THEN RETURN false; END IF;
  extra:=8*p_width*p_height+8192;
  expected:=baseline || jsonb_build_object('ram_bytes',(baseline->>'ram_bytes')::bigint+extra,
    'scratch_bytes',(baseline->>'scratch_bytes')::bigint+extra);
  IF stripped->'resource_estimate' IS DISTINCT FROM expected THEN RETURN false; END IF;
  stripped:=jsonb_set(stripped,'{resource_estimate}',baseline);
 END IF;
 RETURN photo.v25_layer_runtime_profile_shape(p_params,p_width,p_height,stripped);
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_profile_shape(jsonb,bigint,bigint,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_profile_shape(jsonb,bigint,bigint,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_profile_shape(jsonb,bigint,bigint,jsonb) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd
