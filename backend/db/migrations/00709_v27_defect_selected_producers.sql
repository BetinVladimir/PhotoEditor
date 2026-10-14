-- +goose Up
-- Selected producer/qualification evidence only. Completeness, content/root
-- binding, global policy and atomic queue admission remain separate obligations.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_defect_selected_producers(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_faces jsonb,p_parser jsonb,p_manual jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE entry jsonb; entries jsonb; root jsonb; kind integer; i integer; seen text[]; ref_name text;
BEGIN
 IF photo.lock_semantic_defect_execution_scope(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha) IS NOT TRUE
 OR jsonb_typeof(p_faces) IS DISTINCT FROM 'array' OR jsonb_array_length(p_faces)>64
 OR jsonb_typeof(p_manual) IS DISTINCT FROM 'array' OR jsonb_array_length(p_manual)>64
 OR jsonb_typeof(p_parser) IS DISTINCT FROM 'object' OR p_parser->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_parser-'schema_version'-'roots'-'parsers')<>'{}'
 OR jsonb_typeof(p_parser->'roots') IS DISTINCT FROM 'array' OR jsonb_array_length(p_parser->'roots')>128
 OR jsonb_typeof(p_parser->'parsers') IS DISTINCT FROM 'array' OR jsonb_array_length(p_parser->'parsers')>64
 OR octet_length(p_faces::text)>8388608 OR octet_length(p_parser::text)>8388608 OR octet_length(p_manual::text)>8388608 THEN RETURN false;END IF;
 FOR kind IN 0..1 LOOP
  seen:='{}';entries:=CASE WHEN kind=0 THEN p_faces ELSE p_parser->'parsers' END;
  ref_name:=CASE WHEN kind=0 THEN 'landmarks' ELSE 'labels' END;
  FOR entry IN SELECT value FROM jsonb_array_elements(entries) LOOP
   IF jsonb_typeof(entry) IS DISTINCT FROM 'object'
   OR (entry-ref_name-'pixels'-'detector'-'expected'-'evidence'-'policies'-'qualifications')<>'{}'
   OR (kind=0 AND entry ? 'pixels')
   OR jsonb_typeof(entry->'policies') IS DISTINCT FROM 'array' OR jsonb_array_length(entry->'policies')<>2
   OR jsonb_typeof(entry->'qualifications') IS DISTINCT FROM 'array' OR jsonb_array_length(entry->'qualifications')<>2
   OR entry->'expected' IS DISTINCT FROM entry->'policies'
   OR jsonb_typeof(entry->'evidence') IS DISTINCT FROM 'array'
   OR entry->ref_name->>'artifact_id'=ANY(seen) THEN RETURN false;END IF;
   seen:=array_append(seen,entry->ref_name->>'artifact_id');
   IF kind=0 THEN
    IF photo.lock_semantic_proposal_face_evidence(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,
    (entry->'landmarks'->>'artifact_id')::uuid,entry->'landmarks'->>'sha256',(entry->'detector'->>'artifact_id')::uuid,entry->'detector'->>'sha256',entry->'expected',entry->'evidence') IS NOT TRUE THEN RETURN false;END IF;
   ELSE
    IF photo.lock_semantic_proposal_face_parse_evidence(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,
    (entry->'labels'->>'artifact_id')::uuid,entry->'labels'->>'sha256',(entry->'detector'->>'artifact_id')::uuid,entry->'detector'->>'sha256',
    (entry->'pixels'->>'artifact_id')::uuid,entry->'pixels'->>'sha256',entry->'expected',entry->'evidence') IS NOT TRUE THEN RETURN false;END IF;
   END IF;
   FOR i IN 0..1 LOOP
    IF kind=0 THEN
     IF photo.lock_semantic_proposal_face_qualification(p_tenant,p_project,p_actor,p_session,entry->'policies'->i,entry->'qualifications'->i) IS NOT TRUE THEN RETURN false;END IF;
    ELSE
     IF photo.lock_semantic_proposal_face_parse_qualification(p_tenant,p_project,p_actor,p_session,entry->'policies'->i,entry->'qualifications'->i) IS NOT TRUE THEN RETURN false;END IF;
    END IF;
   END LOOP;
  END LOOP;
 END LOOP;
 seen:='{}';
 FOR entry IN SELECT value FROM jsonb_array_elements(p_manual) LOOP
  IF jsonb_typeof(entry) IS DISTINCT FROM 'object' OR (entry-'root'-'target'-'evidence')<>'{}'
  OR jsonb_typeof(entry->'evidence') IS DISTINCT FROM 'array'
  OR entry->'target'->>'artifact_id'=ANY(seen) THEN RETURN false;END IF;
  seen:=array_append(seen,entry->'target'->>'artifact_id');
  SELECT value INTO root FROM jsonb_array_elements(entry->'evidence') WHERE value->>'role'='rasterized_mask';
  IF root IS NULL OR entry->'root' IS DISTINCT FROM jsonb_build_object('artifact_id',root->'artifact_id','sha256',root->'sha256')
  OR photo.lock_semantic_proposal_manual_evidence(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,
  (entry->'target'->>'artifact_id')::uuid,entry->'target'->>'sha256',entry->'evidence') IS NOT TRUE THEN RETURN false;END IF;
 END LOOP;
 RETURN photo.lock_semantic_defect_execution_scope(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_defect_selected_producers(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_selected_producers(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_selected_producers(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
