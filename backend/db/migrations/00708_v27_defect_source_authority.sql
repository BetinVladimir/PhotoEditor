-- +goose Up
-- Source/defect qualification/policy component. Other producer and content
-- proofs and dedicated atomic graph admission are still required.
-- +goose StatementBegin
CREATE FUNCTION photo.lock_semantic_defect_source_authority(p_tenant uuid,p_project uuid,p_actor uuid,p_session uuid,p_version uuid,p_source uuid,p_source_sha text,p_authority jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE binding jsonb;
BEGIN
 IF jsonb_typeof(p_authority) IS DISTINCT FROM 'object' OR octet_length(p_authority::text)>8388608
 OR p_authority->'schema_version' IS DISTINCT FROM '1'::jsonb
 OR (p_authority-'schema_version'-'binding'-'source_pins'-'expected'-'qualification'-'policy')<>'{}'
 THEN RETURN false;END IF;
 binding:=p_authority->'binding';
 IF jsonb_typeof(binding) IS DISTINCT FROM 'object'
 OR (binding-'asset_version_id'-'source'-'canvas_hash')<>'{}'
 OR binding->'asset_version_id' IS DISTINCT FROM to_jsonb(p_version::text)
 OR binding->'source' IS DISTINCT FROM jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha)
 OR jsonb_typeof(binding->'canvas_hash') IS DISTINCT FROM 'string'
 OR coalesce(binding->>'canvas_hash','') !~ '^[0-9a-f]{64}$'
 OR jsonb_typeof(p_authority->'expected') IS DISTINCT FROM 'object'
 THEN RETURN false;END IF;
 IF photo.lock_semantic_defect_execution_scope(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha) IS NOT TRUE
 OR photo.lock_semantic_proposal_source_evidence(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha,p_authority->'source_pins') IS NOT TRUE
 OR photo.lock_semantic_proposal_qualification_policy_snapshot(p_tenant,p_project,p_actor,p_session,p_authority->'policy') IS NOT TRUE
 OR photo.lock_semantic_proposal_defect_qualification(p_tenant,p_project,p_actor,p_session,p_authority->'expected',p_authority->'qualification') IS NOT TRUE
 THEN RETURN false;END IF;
 RETURN photo.lock_semantic_defect_execution_scope(p_tenant,p_project,p_actor,p_session,p_version,p_source,p_source_sha);
EXCEPTION WHEN invalid_text_representation OR invalid_parameter_value OR numeric_value_out_of_range THEN RETURN false;
END $$;
ALTER FUNCTION photo.lock_semantic_defect_source_authority(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.lock_semantic_defect_source_authority(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.lock_semantic_defect_source_authority(uuid,uuid,uuid,uuid,uuid,uuid,text,jsonb) TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
