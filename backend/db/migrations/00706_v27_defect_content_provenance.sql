-- +goose Up
-- Content/provenance composition only; no graph activation authority.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_defect_content_provenance(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_document uuid,p_params uuid,p_params_sha text,p_origin jsonb,p_params_pin jsonb,p_content jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source_bytes bigint;
BEGIN
 IF photo.semantic_defect_activation_content_matches(p_content) IS NOT TRUE
 OR p_content->'working_image' IS DISTINCT FROM jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha)
 OR p_content->'params' IS DISTINCT FROM jsonb_build_object('artifact_id',p_params,'sha256',p_params_sha)
 OR jsonb_typeof(p_origin) IS DISTINCT FROM 'array' OR jsonb_array_length(p_origin)<>5
 OR p_content->'original_params' IS DISTINCT FROM jsonb_build_object('artifact_id',p_origin->0->'artifact_id','sha256',p_origin->0->'sha256')
 THEN RETURN false;END IF;
 IF photo.lock_semantic_defect_execution_scope(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha) IS NOT TRUE
 OR photo.lock_semantic_defect_origin_evidence(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_origin) IS NOT TRUE
 OR photo.lock_semantic_defect_version_params(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_document,p_params,p_params_sha,p_params_pin) IS NOT TRUE
 THEN RETURN false;END IF;
 SELECT bytes INTO source_bytes FROM photo.artifacts WHERE tenant_id=p_tenant AND id=p_source AND sha256=p_source_sha FOR SHARE;
 IF NOT FOUND THEN RETURN false;END IF;
 RETURN source_bytes=octet_length(p_content->>'working_image_text')
 AND (p_origin->0->>'bytes')::bigint=octet_length(p_content->>'original_params_text')
 AND (p_params_pin->>'bytes')::bigint=octet_length(p_content->>'params_text');
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_defect_content_provenance(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,jsonb,jsonb,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_content_provenance(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,jsonb,jsonb,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_content_provenance(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,jsonb,jsonb,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
