-- +goose Up
-- Content matching only. Original catalog/producer/policy locks and dedicated
-- graph activation are separate; this function grants no admission authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_defect_activation_content_matches(p_content jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE name text; ref jsonb; raw text; image jsonb; original jsonb; native jsonb;
 required jsonb; candidates jsonb; mask jsonb; ids text[]:='{}';
BEGIN
 IF p_content IS NULL OR jsonb_typeof(p_content) IS DISTINCT FROM 'object'
 OR octet_length(p_content::text)>20000000
 OR (p_content-'schema_version'-'working_image'-'original_params'-'params'-'working_image_text'-'original_params_text'-'params_text')<>'{}'
 OR p_content->'schema_version' IS DISTINCT FROM '1'::jsonb THEN RETURN false;END IF;
 FOREACH name IN ARRAY ARRAY['working_image','original_params','params'] LOOP
  ref:=p_content->name;
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-'artifact_id'-'sha256')<>'{}'
  OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string'
  OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR ref->>'artifact_id'='00000000-0000-0000-0000-000000000000'
  OR ref->>'artifact_id'=ANY(ids)
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR jsonb_typeof(p_content->(name||'_text')) IS DISTINCT FROM 'string' THEN RETURN false;END IF;
  ids:=array_append(ids,ref->>'artifact_id');raw:=p_content->>(name||'_text');
  IF octet_length(raw) NOT BETWEEN 2 AND 1048576
  OR encode(public.digest(convert_to(raw,'UTF8'),'sha256'),'hex') IS DISTINCT FROM ref->>'sha256'
  THEN RETURN false;END IF;
 END LOOP;
 image:=(p_content->>'working_image_text')::jsonb;
 original:=(p_content->>'original_params_text')::jsonb;
 native:=(p_content->>'params_text')::jsonb;
 IF jsonb_typeof(image) IS DISTINCT FROM 'object'
 OR image->>'dtype' IS DISTINCT FROM 'float32'
 OR image->'color'->>'profile_id' IS DISTINCT FROM 'work_rec2020_gamma24_v1'
 OR jsonb_typeof(image->'canvas') IS DISTINCT FROM 'object'
 OR jsonb_typeof(original) IS DISTINCT FROM 'object' OR jsonb_typeof(native) IS DISTINCT FROM 'object'
 OR original->'image' IS DISTINCT FROM p_content->'working_image'
 OR native->'image' IS DISTINCT FROM p_content->'working_image'
 OR jsonb_typeof(original->'faces') IS DISTINCT FROM 'array'
 OR jsonb_typeof(native->'faces') IS DISTINCT FROM 'array'
 OR jsonb_array_length(original->'faces')>64
 OR native->'faces' IS DISTINCT FROM original->'faces' THEN RETURN false;END IF;
 IF jsonb_typeof(coalesce(original->'protections','[]')) IS DISTINCT FROM 'array'
 OR jsonb_typeof(coalesce(native->'protections','[]')) IS DISTINCT FROM 'array'
 OR jsonb_array_length(coalesce(original->'protections','[]'))>64
 OR jsonb_array_length(coalesce(native->'protections','[]'))>64 THEN RETURN false;END IF;
 required:=coalesce(original->'protections','[]');
 candidates:=coalesce(native->'protections','[]');
 IF original->'permanent' IS NOT NULL AND original->'permanent'<>'null' THEN required:=required||jsonb_build_array(original->'permanent');END IF;
 IF native->'permanent' IS NOT NULL AND native->'permanent'<>'null' THEN candidates:=candidates||jsonb_build_array(native->'permanent');END IF;
 FOR mask IN SELECT value FROM jsonb_array_elements(required) LOOP
  IF mask->'protected'='true'::jsonb AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(candidates) c WHERE c.value=mask) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range OR untranslatable_character THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_defect_activation_content_matches(jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_activation_content_matches(jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_defect_activation_content_matches(jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
