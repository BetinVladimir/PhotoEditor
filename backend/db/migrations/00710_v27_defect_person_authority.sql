-- +goose Up
-- Selected person/protection evidence and protected pixel presence only.
-- Actual coverage content and full atomic graph admission remain separate.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_defect_person_authority(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_person jsonb,p_content jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE native jsonb;masks jsonb;pass integer;
BEGIN
 IF jsonb_typeof(p_person) IS DISTINCT FROM 'object' OR octet_length(p_person::text)>8388608
 OR p_person->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_person-'schema_version'-'root'-'child'-'protection'-'protected_pixels'-'count'-'expected'-'evidence'-'qualification'-'protection_evidence')<>'{}'
 OR jsonb_typeof(p_person->'count') IS DISTINCT FROM 'number' OR coalesce(p_person->>'count','') !~ '^[0-9]+$'
 OR (p_person->>'count')::bigint NOT BETWEEN 0 AND 1000000
 OR photo.semantic_defect_activation_content_matches(p_content) IS NOT TRUE
 OR p_content->'working_image' IS DISTINCT FROM jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha)
 THEN RETURN false;END IF;
 native:=(p_content->>'params_text')::jsonb;
 masks:=coalesce(native->'protections','[]');
 IF native->'permanent' IS NOT NULL AND native->'permanent'<>'null' THEN masks:=masks||jsonb_build_array(native->'permanent');END IF;
 IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(masks) mask WHERE mask->'protected'='true'::jsonb AND mask->'artifact'=p_person->'protected_pixels') THEN RETURN false;END IF;
 IF photo.lock_semantic_defect_execution_scope(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha) IS NOT TRUE THEN RETURN false;END IF;
 FOR pass IN 0..1 LOOP
  IF photo.lock_semantic_proposal_protection_evidence(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,
  (p_person->'protection'->>'artifact_id')::uuid,p_person->'protection'->>'sha256',(p_person->'protected_pixels'->>'artifact_id')::uuid,p_person->'protected_pixels'->>'sha256',p_person->'protection_evidence') IS NOT TRUE
  OR photo.lock_semantic_proposal_person_evidence(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,
  (p_person->'root'->>'artifact_id')::uuid,p_person->'root'->>'sha256',(p_person->'child'->>'artifact_id')::uuid,p_person->'child'->>'sha256',p_person->'expected',p_person->'evidence') IS NOT TRUE
  OR photo.lock_semantic_proposal_person_qualification(p_tenant,p_project,p_actor,p_session,p_person->'expected',p_person->'qualification') IS NOT TRUE THEN RETURN false;END IF;
  IF pass=0 AND photo.lock_semantic_proposal_person_inventory(p_tenant,p_project,p_actor,p_session) IS NOT TRUE THEN RETURN false;END IF;
 END LOOP;
 IF photo.semantic_proposal_person_candidate_matches(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,
 (p_person->'root'->>'artifact_id')::uuid,p_person->'root'->>'sha256',(p_person->'child'->>'artifact_id')::uuid,p_person->'child'->>'sha256',p_person->'expected') IS NOT TRUE THEN RETURN false;END IF;
 RETURN photo.lock_semantic_defect_execution_scope(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_defect_person_authority(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_person_authority(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_person_authority(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
