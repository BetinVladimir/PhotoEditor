-- +goose Up
-- Called inside the eventual replacement admission transaction. No object IO,
-- no commit and no lease/resource/publication mutation occurs here.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_v29_negative_unchanged_proof(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,
 p_review uuid,p_hash text,p_plan jsonb,p_task uuid,p_image uuid,p_image_sha text,p_source text,p_selection jsonb,p_pins jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE image jsonb;expected jsonb:='[]'::jsonb;item jsonb;ref jsonb;role_name text;i integer;actual jsonb;
BEGIN
 IF p_tenant IS DISTINCT FROM nullif(current_setting('app.tenant_id',true),'')::uuid
 OR p_actor IS NULL OR p_session IS NULL OR p_source IS NULL OR octet_length(p_source) NOT BETWEEN 2 AND 65536
 OR p_selection IS NULL OR jsonb_typeof(p_selection)<>'object' OR pg_column_size(p_selection)>32768
 OR p_pins IS NULL OR jsonb_typeof(p_pins)<>'array' OR jsonb_array_length(p_pins) NOT BETWEEN 1 AND 130
 OR pg_column_size(p_pins)>2097152 THEN RETURN false;END IF;
 IF photo.lock_v29_negative_replacement_scope(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_plan) IS DISTINCT FROM true THEN RETURN false;END IF;
 IF photo.read_v29_negative_frozen_intent_locked(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_task) IS NULL THEN RETURN false;END IF;
 PERFORM a.id FROM photo.artifacts a WHERE a.tenant_id=p_tenant AND a.id IN(
 SELECT p_image UNION SELECT (pin->'object'->>'artifact_id')::uuid FROM jsonb_array_elements(p_pins) pin
 ) ORDER BY a.id FOR SHARE;
 actual:=photo.read_v29_negative_unchanged_source(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_task,p_image,p_image_sha);
 IF actual IS NULL OR actual IS DISTINCT FROM p_selection
 OR encode(sha256(convert_to(p_source,'UTF8')),'hex') IS DISTINCT FROM actual->>'sha256' THEN RETURN false;END IF;
 image:=p_source::jsonb;
 IF jsonb_typeof(image->'transform_chain'->'steps') IS DISTINCT FROM 'array'
 OR jsonb_array_length(image->'transform_chain'->'steps')>128 THEN RETURN false;END IF;
 FOR i IN 0..jsonb_array_length(image->'transform_chain'->'steps')+1 LOOP
  IF i=0 THEN role_name:='WorkingPixels';ref:=image->'pixels';
  ELSIF i=1 THEN role_name:='WorkingAlpha';ref:=image->'alpha';IF ref IS NULL OR ref='null'::jsonb THEN CONTINUE;END IF;
  ELSE
   item:=image->'transform_chain'->'steps'->(i-2);
   IF item->>'kind' IS DISTINCT FROM 'dense_warp' THEN
    IF item->'inverse_field' IS NOT NULL AND item->'inverse_field'<>'null'::jsonb THEN RETURN false;END IF;
    CONTINUE;
   END IF;
   role_name:='InverseField/'||(i-2)::text;ref:=item->'inverse_field';
  END IF;
  actual:=photo.read_v29_negative_unchanged_array(p_tenant,p_project,p_actor,p_session,p_review,p_hash,p_task,p_image,p_image_sha,p_source,
   (ref->>'artifact_id')::uuid,ref->>'sha256',role_name);
  IF actual IS NULL THEN RETURN false;END IF;
  expected:=expected||jsonb_build_array(jsonb_build_object('role',role_name,'object',actual));
 END LOOP;
 RETURN expected=p_pins;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_v29_negative_unchanged_proof(uuid,uuid,uuid,uuid,uuid,text,jsonb,uuid,uuid,text,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_v29_negative_unchanged_proof(uuid,uuid,uuid,uuid,uuid,text,jsonb,uuid,uuid,text,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_v29_negative_unchanged_proof(uuid,uuid,uuid,uuid,uuid,text,jsonb,uuid,uuid,text,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
