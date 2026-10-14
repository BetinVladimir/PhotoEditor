-- +goose Up
-- Private value validation only; callers still need authenticated task authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_metadata_frozen_valid(p_raw text,p_pin text,p_binding jsonb,
 p_metadata jsonb,p_source text,p_policy text,p_approved text[]) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE f jsonb;s jsonb;projection jsonb;field text;value jsonb;names text[];
BEGIN
 IF p_raw IS NULL OR octet_length(p_raw) NOT BETWEEN 2 AND 1048576
 OR p_source IS NULL OR octet_length(p_source) NOT BETWEEN 2 AND 1048576
 OR p_pin IS NULL OR p_pin !~ '^[0-9a-f]{64}$'
 OR encode(sha256(convert_to(p_raw,'UTF8')),'hex')<>p_pin
 OR p_policy IS NULL OR p_policy NOT IN('strip','copyright','approved_exif')
 OR p_approved IS NULL OR cardinality(p_approved)>7 OR array_position(p_approved,NULL) IS NOT NULL
 OR (p_policy<>'approved_exif' AND cardinality(p_approved)>0) THEN RETURN false;END IF;
 IF EXISTS(SELECT 1 FROM unnest(p_approved) x WHERE x NOT IN('author','copyright','camera_make','camera_model','lens','datetime_original','gps'))
 OR (SELECT count(DISTINCT x) FROM unnest(p_approved) x)<>cardinality(p_approved) THEN RETURN false;END IF;
 f:=p_raw::jsonb;s:=p_source::jsonb;
 IF jsonb_typeof(f)<>'object' OR jsonb_typeof(s)<>'object'
 OR (SELECT count(*) FROM jsonb_object_keys(f))<>6
 OR f->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR f->'binding' IS DISTINCT FROM p_binding OR f->'metadata_source' IS DISTINCT FROM p_metadata
 OR f->>'policy' IS DISTINCT FROM p_policy
 OR p_metadata->>'sha256' IS DISTINCT FROM encode(sha256(convert_to(p_source,'UTF8')),'hex')
 OR jsonb_typeof(p_metadata) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(p_metadata))<>2
 OR jsonb_typeof(p_binding) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(p_binding))<>3
 OR (p_binding->>'asset_version_id')::uuid IS NULL OR (p_binding->>'asset_version_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
 OR (p_metadata->>'artifact_id')::uuid IS NULL OR (p_metadata->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
 OR p_metadata->>'artifact_id' IS NOT DISTINCT FROM p_binding->'source'->>'artifact_id'
 OR p_binding->>'canvas_hash' IS NULL OR p_binding->>'canvas_hash' !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(p_binding->'source') IS DISTINCT FROM 'object'
 OR (SELECT count(*) FROM jsonb_object_keys(p_binding->'source'))<>2
 OR (p_binding->'source'->>'artifact_id')::uuid IS NULL
 OR (p_binding->'source'->>'artifact_id')::uuid='00000000-0000-0000-0000-000000000000'::uuid
 OR p_binding->'source'->>'sha256' IS NULL OR p_binding->'source'->>'sha256' !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 SELECT coalesce(array_agg(x ORDER BY x COLLATE "C"),ARRAY[]::text[]) INTO names FROM unnest(p_approved) x;
 IF f->'approved_fields' IS DISTINCT FROM to_jsonb(names) THEN RETURN false;END IF;
 names:=ARRAY['author','copyright','camera_make','camera_model','lens','datetime_original','gps'];
 IF (SELECT count(*) FROM jsonb_object_keys(s))<>7 OR EXISTS(SELECT 1 FROM unnest(names) x WHERE NOT s ? x) THEN RETURN false;END IF;
 FOREACH field IN ARRAY names LOOP
  value:=s->field;
  IF value='null'::jsonb THEN CONTINUE;END IF;
  IF field='gps' THEN
   IF jsonb_typeof(value)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(value))<>3
   OR jsonb_typeof(value->'latitude') IS DISTINCT FROM 'number' OR jsonb_typeof(value->'longitude') IS DISTINCT FROM 'number'
   OR (value->>'latitude')::numeric NOT BETWEEN -90 AND 90 OR (value->>'longitude')::numeric NOT BETWEEN -180 AND 180
   OR value->'altitude_m' IS NULL OR (value->'altitude_m'<>'null'::jsonb AND (jsonb_typeof(value->'altitude_m')<>'number' OR (value->>'altitude_m')::numeric NOT BETWEEN -1000 AND 100000)) THEN RETURN false;END IF;
  ELSIF jsonb_typeof(value)<>'string' OR char_length(s->>field) NOT BETWEEN 1 AND 256
   OR (field='datetime_original' AND s->>field !~ '^[0-9]{4}:[0-9]{2}:[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}$') THEN RETURN false;
  END IF;
 END LOOP;
 projection:=jsonb_build_object('author',NULL,'copyright',NULL,'camera_make',NULL,'camera_model',NULL,'lens',NULL,'datetime_original',NULL,'gps',NULL);
 FOREACH field IN ARRAY names LOOP
  IF (p_policy='copyright' AND field IN('author','copyright')) OR (p_policy='approved_exif' AND field=ANY(p_approved)) THEN
   projection:=jsonb_set(projection,ARRAY[field],s->field);
  END IF;
 END LOOP;
 RETURN f->'projection' IS NOT DISTINCT FROM projection;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_metadata_frozen_valid(text,text,jsonb,jsonb,text,text,text[]) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_metadata_frozen_valid(text,text,jsonb,jsonb,text,text,text[]) FROM PUBLIC,photo_api,photo_worker,photo_scheduler,photo_gateway,photo_portal;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
