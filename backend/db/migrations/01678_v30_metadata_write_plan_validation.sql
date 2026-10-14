-- +goose Up
-- Private compilation proof; never substitutes current tenant/task authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_metadata_write_plan_valid(p_task uuid,p_encoded jsonb,p_encoded_body text,
 p_frozen text,p_pin text,p_binding jsonb,p_metadata jsonb,p_source text,p_policy text,p_approved text[],
 p_projection text,p_params text) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE image jsonb;projection_id uuid;params_id uuid;expected jsonb;ids uuid[];w numeric;h numeric;
BEGIN
 IF p_task IS NULL OR p_task='00000000-0000-0000-0000-000000000000'::uuid
 OR p_encoded_body IS NULL OR octet_length(p_encoded_body) NOT BETWEEN 2 AND 1048576
 OR p_projection IS NULL OR octet_length(p_projection) NOT BETWEEN 2 AND 1048576
 OR p_params IS NULL OR octet_length(p_params) NOT BETWEEN 2 AND 65536
 OR p_policy IS NULL OR p_policy NOT IN('copyright','approved_exif')
 OR photo.v30_metadata_frozen_valid(p_frozen,p_pin,p_binding,p_metadata,p_source,p_policy,p_approved) IS DISTINCT FROM true
 OR jsonb_typeof(p_encoded) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(p_encoded))<>2
 OR p_encoded->>'sha256' IS DISTINCT FROM encode(sha256(convert_to(p_encoded_body,'UTF8')),'hex') THEN RETURN false;END IF;
 image:=p_encoded_body::jsonb;
 IF jsonb_typeof(image) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(image))<>8
 OR image->>'metadata_policy' IS DISTINCT FROM 'strip'
 OR image->>'format' IS NULL OR image->>'format' NOT IN('jpeg','png','tiff')
 OR image->>'icc_id' IS NULL OR image->>'icc_id' NOT IN('srgb_v1','work_rec2020_gamma24_v1')
 OR image->>'icc_sha256' IS NULL OR image->>'icc_sha256' !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(image->'alpha') IS DISTINCT FROM 'boolean'
 OR (image->>'format'='jpeg' AND image->'alpha'='true'::jsonb)
 OR jsonb_typeof(image->'width') IS DISTINCT FROM 'number' OR jsonb_typeof(image->'height') IS DISTINCT FROM 'number'
 OR jsonb_typeof(image->'artifact') IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(image->'artifact'))<>2
 OR image->'artifact'->>'sha256' IS NULL OR image->'artifact'->>'sha256' !~ '^[0-9a-f]{64}$'
 OR p_projection::jsonb IS DISTINCT FROM p_frozen::jsonb->'projection' THEN RETURN false;END IF;
 w:=(image->>'width')::numeric;h:=(image->>'height')::numeric;
 IF w<1 OR h<1 OR w<>trunc(w) OR h<>trunc(h) OR w*h>200000000 THEN RETURN false;END IF;
 projection_id:=photo.v30_uuid5(p_task,'delivery:metadata:projection:1');
 params_id:=photo.v30_uuid5(p_task,'delivery:metadata:params:1');
 ids:=ARRAY[(p_encoded->>'artifact_id')::uuid,(image->'artifact'->>'artifact_id')::uuid,
 (p_metadata->>'artifact_id')::uuid,(p_binding->'source'->>'artifact_id')::uuid,projection_id,params_id];
 IF array_position(ids,NULL) IS NOT NULL OR '00000000-0000-0000-0000-000000000000'::uuid=ANY(ids)
 OR (SELECT count(DISTINCT id) FROM unnest(ids) id)<>6 THEN RETURN false;END IF;
 expected:=jsonb_build_object('encoded',p_encoded,'document',jsonb_build_object('artifact_id',projection_id,
 'sha256',encode(sha256(convert_to(p_projection,'UTF8')),'hex')),
 'policy',CASE WHEN p_policy='copyright' THEN 'copyright_only' ELSE 'safe_camera' END,
 'copy_gps',p_policy='approved_exif' AND 'gps'=ANY(p_approved),'tool_id','exiftool_v1');
 RETURN p_params::jsonb IS NOT DISTINCT FROM expected;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_write_plan_valid(uuid,jsonb,text,text,text,jsonb,jsonb,text,text,text[],text,text) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_write_plan_valid(uuid,jsonb,text,text,text,jsonb,jsonb,text,text,text[],text,text) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
