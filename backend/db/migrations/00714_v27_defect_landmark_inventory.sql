-- +goose Up
-- Exact retained landmark sets only. Object/face content, discovery, model
-- qualifications and pixel coverage remain independent admission obligations.
-- +goose StatementBegin
CREATE FUNCTION photo.semantic_defect_landmark_inventory_matches(p_files jsonb,p_content jsonb,p_faces jsonb)
RETURNS boolean LANGUAGE plpgsql IMMUTABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE native jsonb;required jsonb;catalog jsonb;producers jsonb;ref jsonb;
BEGIN
 IF jsonb_typeof(p_files) IS DISTINCT FROM 'array' OR jsonb_array_length(p_files)>4097 OR octet_length(p_files::text)>4194304
 OR jsonb_typeof(p_faces) IS DISTINCT FROM 'array' OR jsonb_array_length(p_faces)>64 OR octet_length(p_faces::text)>8388608
 OR photo.semantic_defect_activation_content_matches(p_content) IS NOT TRUE THEN RETURN false;END IF;
 native:=(p_content->>'params_text')::jsonb;
 FOR ref IN SELECT value->'landmarks' FROM jsonb_array_elements(native->'faces') LOOP
  IF jsonb_typeof(ref) IS DISTINCT FROM 'object' OR (ref-'artifact_id'-'sha256')<>'{}'
  OR jsonb_typeof(ref->'artifact_id') IS DISTINCT FROM 'string' OR jsonb_typeof(ref->'sha256') IS DISTINCT FROM 'string'
  OR coalesce(ref->>'artifact_id','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR ref->>'artifact_id'='00000000-0000-0000-0000-000000000000'
  OR coalesce(ref->>'sha256','') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 END LOOP;
 SELECT coalesce(jsonb_agg(value ORDER BY value->>'artifact_id'),'[]') INTO required
 FROM (SELECT DISTINCT value->'landmarks' AS value FROM jsonb_array_elements(native->'faces')) unique_refs;
 IF (SELECT count(DISTINCT value->'landmarks'->>'artifact_id') FROM jsonb_array_elements(native->'faces'))<>jsonb_array_length(required) THEN RETURN false;END IF;
 SELECT coalesce(jsonb_agg(jsonb_build_object('artifact_id',value->'artifact_id','sha256',value->'sha256') ORDER BY value->>'artifact_id'),'[]') INTO catalog
 FROM jsonb_array_elements(p_files) WHERE value->>'contract'='FaceLandmarks';
 SELECT coalesce(jsonb_agg(value->'landmarks' ORDER BY value->'landmarks'->>'artifact_id'),'[]') INTO producers FROM jsonb_array_elements(p_faces);
 RETURN required=catalog AND required=producers;
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.semantic_defect_landmark_inventory_matches(jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.semantic_defect_landmark_inventory_matches(jsonb,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.semantic_defect_landmark_inventory_matches(jsonb,jsonb,jsonb) TO photo_scheduler;
ALTER FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) RENAME TO lock_semantic_defect_admission_evidence_pre_landmarks;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_admission_evidence_pre_landmarks(uuid,uuid,jsonb) FROM photo_scheduler;
CREATE FUNCTION photo.lock_semantic_defect_admission_evidence(p_tenant uuid,p_run uuid,p_evidence jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF photo.semantic_defect_landmark_inventory_matches(p_evidence->'files',p_evidence->'content',p_evidence->'faces') IS NOT TRUE THEN RETURN false;END IF;
 RETURN photo.lock_semantic_defect_admission_evidence_pre_landmarks(p_tenant,p_run,p_evidence);
END $$;
ALTER FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_admission_evidence(uuid,uuid,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
