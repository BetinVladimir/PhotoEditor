-- +goose Up
-- Independent bounded transport/file/Params consistency. Does not establish
-- catalog discovery completeness, physical mask coverage or queue authority.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_defect_admission_closure_matches(p_files jsonb,p_content jsonb,p_registration jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE rows jsonb;direct jsonb;native jsonb;image jsonb;required jsonb;item jsonb;ref jsonb;file jsonb;contract text;ids text[]:='{}';
BEGIN
 IF jsonb_typeof(p_files) IS DISTINCT FROM 'array' OR octet_length(p_files::text)>4194304
 OR jsonb_array_length(p_files) NOT BETWEEN 2 AND 4097
 OR photo.semantic_defect_activation_content_matches(p_content) IS NOT TRUE
 OR p_registration->'params' IS DISTINCT FROM p_content->'params' THEN RETURN false;END IF;
 rows:=p_registration->'transport'->'rows';direct:=p_registration->'transport'->'direct';
 IF jsonb_typeof(rows) IS DISTINCT FROM 'array' OR octet_length(rows::text)>4194304 OR jsonb_array_length(rows) NOT BETWEEN 3 AND 4097
 OR p_registration->'transport'->'count' IS DISTINCT FROM to_jsonb(jsonb_array_length(rows))
 OR jsonb_typeof(direct) IS DISTINCT FROM 'array' OR jsonb_array_length(direct)<>3
 OR (SELECT count(DISTINCT value->>'artifact_id') FROM jsonb_array_elements(p_files))<>jsonb_array_length(p_files) THEN RETURN false;END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(rows) LOOP
  ref:=item->'ref';
  IF jsonb_typeof(item) IS DISTINCT FROM 'object' OR (item-'ref'-'contract')<>'{}'
  OR jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-'artifact_id'-'sha256')<>'{}'
  OR coalesce(ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR ref->>'artifact_id'='00000000-0000-0000-0000-000000000000'
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$'
  OR ref->>'artifact_id'=ANY(ids) THEN RETURN false;END IF;
  ids:=array_append(ids,ref->>'artifact_id');
  IF ref=p_content->'params' THEN
   IF item->>'contract' IS DISTINCT FROM 'Params' THEN RETURN false;END IF;
  ELSE
   SELECT value INTO file FROM jsonb_array_elements(p_files) WHERE value->'artifact_id'=ref->'artifact_id' AND value->'sha256'=ref->'sha256';
   IF NOT FOUND THEN RETURN false;END IF;
   contract:=file->>'contract';
   IF contract IN('WorkingPixels','WorkingAlpha','MaskPixels','DenseWarp','DenseWarpField') THEN contract:='NpyArray';END IF;
   IF item->>'contract' IS DISTINCT FROM contract OR contract NOT IN('WorkingImage','FaceLandmarks','MaskRef','NpyArray') THEN RETURN false;END IF;
  END IF;
 END LOOP;
 native:=(p_content->>'params_text')::jsonb;image:=(p_content->>'working_image_text')::jsonb;
 required:=jsonb_build_array(p_content->'params',p_content->'working_image',image->'pixels');
 IF NOT rows @> jsonb_build_array(jsonb_build_object('ref',p_content->'params','contract','Params'))
 OR NOT direct @> required OR NOT required @> direct THEN RETURN false;END IF;
 required:=jsonb_build_array(jsonb_build_object('ref',p_content->'working_image','contract','WorkingImage'),jsonb_build_object('ref',image->'pixels','contract','NpyArray'));
 IF native->'permanent' IS NOT NULL AND native->'permanent'<>'null' THEN
  required:=required||jsonb_build_array(jsonb_build_object('ref',native->'permanent'->'artifact'));
 END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(coalesce(native->'protections','[]')) LOOP
  required:=required||jsonb_build_array(jsonb_build_object('ref',item->'artifact'));
 END LOOP;
 FOR item IN SELECT value FROM jsonb_array_elements(native->'faces') LOOP
  required:=required||jsonb_build_array(jsonb_build_object('ref',item->'landmarks','contract','FaceLandmarks'));
  IF item->'skin' IS NOT NULL AND item->'skin'<>'null' THEN required:=required||jsonb_build_array(jsonb_build_object('ref',item->'skin'->'artifact'));END IF;
  IF item->'manual' IS NOT NULL AND item->'manual'<>'null' THEN required:=required||jsonb_build_array(jsonb_build_object('ref',item->'manual'->'artifact'));END IF;
 END LOOP;
 RETURN rows @> required;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_defect_admission_closure_matches(jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_admission_closure_matches(jsonb,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_defect_admission_closure_matches(jsonb,jsonb,jsonb) TO photo_scheduler;
ALTER FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) RENAME TO lock_semantic_defect_admission_evidence_pre_closure;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_admission_evidence_pre_closure(uuid,uuid,jsonb) FROM photo_scheduler;
CREATE FUNCTION photo.lock_semantic_defect_admission_evidence(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.semantic_defect_admission_closure_matches(p_evidence->'files',p_evidence->'content',p_evidence->'registration') IS NOT TRUE THEN RETURN false;END IF;
 RETURN photo.lock_semantic_defect_admission_evidence_pre_closure(p_tenant,p_run,p_evidence);
END $$;
ALTER FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
