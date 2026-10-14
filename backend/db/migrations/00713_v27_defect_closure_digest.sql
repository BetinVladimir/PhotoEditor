-- +goose Up
-- Canonical digest for this strict ASCII-only typed envelope, not general JCS.
-- Retained child inventory consistency does not prove physical/catalog completeness.
-- +goose StatementBegin
ALTER FUNCTION photo.semantic_defect_admission_closure_matches(jsonb,jsonb,jsonb) RENAME TO semantic_defect_admission_closure_matches_pre_digest;
REVOKE ALL ON FUNCTION photo.semantic_defect_admission_closure_matches_pre_digest(jsonb,jsonb,jsonb) FROM photo_scheduler;
CREATE FUNCTION photo.semantic_defect_admission_closure_matches(p_files jsonb,p_content jsonb,p_registration jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rows jsonb;item jsonb;file jsonb;parent jsonb;entry_text text;entries_text text:='[';digest_text text;previous uuid;identity uuid;task uuid;first boolean:=true;
BEGIN
 IF photo.semantic_defect_admission_closure_matches_pre_digest(p_files,p_content,p_registration) IS NOT TRUE THEN RETURN false;END IF;
 IF coalesce(p_registration->>'task_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
 OR p_registration->>'task_id'='00000000-0000-0000-0000-000000000000' THEN RETURN false;END IF;
 task:=(p_registration->>'task_id')::uuid;rows:=p_registration->'transport'->'rows';
 FOR item IN SELECT value FROM jsonb_array_elements(rows) LOOP
  identity:=(item->'ref'->>'artifact_id')::uuid;
  IF previous IS NOT NULL AND identity<=previous THEN RETURN false;END IF;
  previous:=identity;
  -- Prior gate restricts contracts to Params/WorkingImage/FaceLandmarks/MaskRef/NpyArray
  -- and refs to lowercase UUID/hex. None require JSON escaping.
  entry_text:='{"contract":"'||(item->>'contract')||'","ref":{"artifact_id":"'||identity::text||'","sha256":"'||(item->'ref'->>'sha256')||'"}}';
  IF NOT first THEN entries_text:=entries_text||',';END IF;
  first:=false;entries_text:=entries_text||entry_text;
 END LOOP;
 entries_text:=entries_text||']';
 IF encode(public.digest(convert_to(entries_text,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_registration->'transport'->>'entries_sha' THEN RETURN false;END IF;
 digest_text:='{"entries":'||entries_text||',"operation":"defects.propose","task":"'||task::text||'","version":"1"}';
 IF encode(public.digest(convert_to(digest_text,'UTF8'),'sha256'),'hex') IS DISTINCT FROM p_registration->>'input_digest' THEN RETURN false;END IF;
 FOR file IN SELECT value FROM jsonb_array_elements(p_files) LOOP
  IF (file->>'parent_id' IS NULL) IS DISTINCT FROM (file->>'parent_sha' IS NULL) THEN RETURN false;END IF;
  IF file->>'parent_id' IS NULL THEN CONTINUE;END IF;
  SELECT value INTO parent FROM jsonb_array_elements(rows) WHERE value->'ref'->'artifact_id'=file->'parent_id';
  IF NOT FOUND OR parent->>'contract' NOT IN('WorkingImage','MaskRef')
  OR file->>'contract' NOT IN('WorkingPixels','WorkingAlpha','MaskPixels','DenseWarp','DenseWarpField') THEN CONTINUE;END IF;
  IF parent->'ref'->'sha256' IS DISTINCT FROM file->'parent_sha'
  OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(rows) child WHERE child->'ref'=jsonb_build_object('artifact_id',file->'artifact_id','sha256',file->'sha256')) THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_defect_admission_closure_matches(jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_admission_closure_matches(jsonb,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_defect_admission_closure_matches(jsonb,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
