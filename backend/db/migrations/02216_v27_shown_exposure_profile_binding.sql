-- +goose Up
-- Resource/profile shape gate only. Live qualification and measured runtime
-- installation are separate checks required before child dispatch.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_shown_exposure_profile_bound(p_params jsonb,p_profile jsonb,p_image jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE estimate jsonb;k text;w bigint;h bigint;pixels bigint;
BEGIN
 IF jsonb_typeof(p_params) IS DISTINCT FROM 'object' OR jsonb_typeof(p_image) IS DISTINCT FROM 'object'
 OR p_image->>'dtype' IS DISTINCT FROM 'float32'
 OR p_image->'pixels' IS DISTINCT FROM p_params->'pixels'
 OR p_image->'color'->>'profile_id' IS DISTINCT FROM 'work_rec2020_gamma24_v1'
 OR p_image->'color'->>'encoding' IS DISTINCT FROM 'gamma24'
 OR jsonb_typeof(p_image->'canvas'->'width') IS DISTINCT FROM 'number'
 OR jsonb_typeof(p_image->'canvas'->'height') IS DISTINCT FROM 'number'
 OR coalesce(p_image->'canvas'->>'width','') !~ '^[1-9][0-9]{0,8}$'
 OR coalesce(p_image->'canvas'->>'height','') !~ '^[1-9][0-9]{0,8}$' THEN RETURN false;END IF;
 w:=(p_image->'canvas'->>'width')::bigint;h:=(p_image->'canvas'->>'height')::bigint;
 pixels:=w*h;
 IF w>200000000 OR h>200000000 OR pixels NOT BETWEEN 1 AND 200000000 THEN RETURN false;END IF;
 IF jsonb_typeof(p_profile) IS DISTINCT FROM 'object'
 OR NOT(p_profile ?& ARRAY['backend','renderer_version','resource_estimate','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','qualification'])
 OR (p_profile-ARRAY['backend','renderer_version','resource_estimate','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','qualification'])<>'{}'::jsonb
 OR p_profile->>'backend' IS DISTINCT FROM 'cpu' OR p_profile->>'renderer_version' IS DISTINCT FROM 'v2.2'
 OR jsonb_typeof(p_profile->'qualification') IS DISTINCT FROM 'object'
 OR jsonb_typeof(p_profile->'dataset_sha') IS DISTINCT FROM 'string'
 OR coalesce(p_profile->>'dataset_sha','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 FOREACH k IN ARRAY ARRAY['model_lock_sha','runtime_lock_sha','device_profile_sha'] LOOP
 IF jsonb_typeof(p_profile->k) IS DISTINCT FROM 'string' OR coalesce(p_profile->>k,'') !~ '^[0-9a-f]{64}$'
 OR p_profile->k IS DISTINCT FROM p_params->k THEN RETURN false;END IF;
 END LOOP;
 estimate:=p_profile->'resource_estimate';
 IF jsonb_typeof(estimate) IS DISTINCT FROM 'object'
 OR NOT(estimate ?& ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'])
 OR (estimate-ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'])<>'{}'::jsonb THEN RETURN false;END IF;
 FOREACH k IN ARRAY ARRAY['ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'] LOOP
 IF jsonb_typeof(estimate->k) IS DISTINCT FROM 'number' OR coalesce(estimate->>k,'') !~ '^(0|[1-9][0-9]{0,15})$'
 OR (estimate->>k)::numeric>9007199254740991 THEN RETURN false;END IF;
 END LOOP;
 RETURN (estimate->>'pixel_count')::bigint=pixels AND (estimate->>'vram_bytes')::bigint=0
 AND (estimate->>'ram_bytes')::bigint>=67108864+1
 AND (estimate->>'scratch_bytes')::bigint>=24*pixels+32768+16777216
 AND (estimate->>'output_bytes')::bigint>=12*pixels+16384+1048576;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_shown_exposure_profile_bound(jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_shown_exposure_profile_bound(jsonb,jsonb,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_portal,photo_gateway;
-- +goose StatementEnd
